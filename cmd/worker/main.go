package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/config"
	"llm-pricing-api/internal/database"
	"llm-pricing-api/internal/logger"
	"llm-pricing-api/internal/metrics"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/worker"
)

// Freshness telemetry runs on its own ticker, independently of the scrape
// pipeline, so the gauges keep advancing even when a scraper stops running.
// freshnessSampleTimeout bounds a single query so slow samples cannot pile up
// on the ticker.
const (
	freshnessSampleInterval = 60 * time.Second
	freshnessSampleTimeout  = 10 * time.Second
)

// parseLogLevel converts a LOG_LEVEL string to a zerolog.Level.
// Unknown or empty strings default to InfoLevel.
func parseLogLevel(s string) zerolog.Level {
	l, err := zerolog.ParseLevel(s)
	if err != nil {
		return zerolog.InfoLevel
	}
	return l
}

// asynqOptFromURL converts a Redis URL (redis://[user:pass@]host:port/db)
// or a bare host:port string into a fully-populated asynq.RedisClientOpt.
// It preserves the username, password, and database number so that asynq
// can authenticate to Redis in production environments.
func asynqOptFromURL(rawURL string) asynq.RedisClientOpt {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		// Treat as bare host:port — no credentials to pass.
		return asynq.RedisClientOpt{Addr: rawURL}
	}
	return asynq.RedisClientOpt{
		Addr:     opts.Addr,
		Username: opts.Username,
		Password: opts.Password,
		DB:       opts.DB,
	}
}

// sampleFreshness runs one freshness sample under a bounded deadline. A failure
// is logged, never returned: freshness telemetry must not take the data pipeline
// down with it.
func sampleFreshness(ctx context.Context, log zerolog.Logger, s *worker.FreshnessSampler) {
	sampleCtx, cancel := context.WithTimeout(ctx, freshnessSampleTimeout)
	defer cancel()
	if err := s.Sample(sampleCtx); err != nil {
		log.Warn().Err(err).Msg("freshness sample failed")
	}
}

// asynqLogger adapts zerolog.Logger to asynq's Logger interface so that
// task errors and retries appear in the structured log output.
type asynqLogger struct{ l zerolog.Logger }

func (a *asynqLogger) Debug(args ...any) { a.l.Debug().Msgf("%v", args) }
func (a *asynqLogger) Info(args ...any)  { a.l.Info().Msgf("%v", args) }
func (a *asynqLogger) Warn(args ...any)  { a.l.Warn().Msgf("%v", args) }
func (a *asynqLogger) Error(args ...any) { a.l.Error().Msgf("%v", args) }

// Fatal logs at FatalLevel (so downstream log processors and alerting rules
// still see a fatal-severity event) without calling zerolog.Logger.Fatal,
// which invokes os.Exit and would bypass all deferred cleanup in run().
func (a *asynqLogger) Fatal(args ...any) { a.l.WithLevel(zerolog.FatalLevel).Msgf("%v", args) }

// main is the entry point. All logic lives in run() so that deferred
// cleanup executes before os.Exit is called on a non-zero exit.
func main() {
	if err := run(); err != nil {
		// run() logs most errors at their origin, but print to stderr here
		// so that any error that bubbles up silently (e.g. an early-return
		// before the logger is initialised) is still visible in platform logs.
		fmt.Fprintf(os.Stderr, "worker: fatal: %v\n", err)
		os.Exit(1)
	}
}

// run contains the full worker lifecycle. It returns a non-nil error
// whenever the process should exit with a non-zero status, allowing main
// to call os.Exit(1) after all deferred cleanup has run.
func run() error {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		// Logger not yet available — write directly to stderr.
		l := zerolog.New(os.Stderr)
		l.Error().Err(err).Msg("config error")
		return err
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	log := logger.New(logger.Config{
		ServiceName: cfg.OTELServiceName,
		Environment: cfg.AppEnv,
		Level:       parseLogLevel(cfg.LogLevel),
	})

	ctx := context.Background()

	db, err := database.ConnectWithRetry(ctx, cfg.DatabaseURL, 5, log)
	if err != nil {
		log.Error().Err(err).Msg("could not connect to database")
		return err
	}
	defer db.Close()

	redisOpt := asynqOptFromURL(cfg.RedisURL)
	const workerConcurrency = 10
	srv := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: workerConcurrency,
			Logger:      &asynqLogger{log},
		},
	)

	mux := asynq.NewServeMux()

	// Build a *redis.Client for Pub/Sub event publishing.  This client is
	// separate from the asynq-managed connection pool so that Pub/Sub writes
	// go through a dedicated connection and do not interfere with task queuing.
	redisClient := func() *redis.Client {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			// cfg.RedisURL may be a bare host:port; fall back to a default Options struct.
			opts = &redis.Options{Addr: cfg.RedisURL}
		}
		return redis.NewClient(opts)
	}()
	defer redisClient.Close()

	store := worker.NewPgxStore(db)
	rec := reconciler.New(db)
	rec.SetLogger(log)
	rec.SetRedisClient(redisClient)
	h := worker.NewHandlers(store, rec, db)
	h.SetLogger(log)

	mux.HandleFunc(worker.TaskOpenRouterScrape, h.HandleOpenRouterScrape)
	mux.HandleFunc(worker.TaskLiteLLMScrape, h.HandleLiteLLMScrape)
	mux.HandleFunc(worker.TaskHuggingFaceScrape, h.HandleHuggingFaceScrape)
	mux.HandleFunc(worker.TaskOpenAIScrape, h.HandleOpenAIScrape)
	mux.HandleFunc(worker.TaskAnthropicScrape, h.HandleAnthropicScrape)
	mux.HandleFunc(worker.TaskGeminiScrape, h.HandleGeminiScrape)

	// Benchmark scraper handlers — daily cron jobs that fetch scores from
	// external leaderboards and upsert into model_benchmark_scores.
	mux.HandleFunc(worker.TaskSWEBenchScrape, h.HandleSWEBenchScrape)
	mux.HandleFunc(worker.TaskLiveCodeBenchScrape, h.HandleLiveCodeBenchScrape)
	mux.HandleFunc(worker.TaskChatbotArenaScrape, h.HandleChatbotArenaScrape)

	// Drain tasks enqueued under the pre-rename type names. Nothing enqueues
	// these any more; the registrations exist so in-flight tasks from before the
	// deploy are not archived as "handler not found". Remove after one cron cycle.
	mux.HandleFunc(worker.TaskBFCLScrapeDeprecated, h.HandleSWEBenchScrape)
	mux.HandleFunc(worker.TaskHuggingFaceLLMScrapeDeprecated, h.HandleLiveCodeBenchScrape)

	// Intelligence recomputation handlers.
	mux.HandleFunc(worker.TaskRecomputeCapabilityScores, h.HandleRecomputeCapabilityScores)
	mux.HandleFunc(worker.TaskStalenessCheck, h.HandleStalenessCheck)

	// WebhookDeliveryHandler holds the AES key so it can decrypt secrets at
	// task execution time — secrets are stored encrypted at rest.
	webhookHandler := worker.NewWebhookDeliveryHandler(cfg.WebhookSecretKey)
	mux.HandleFunc(worker.TypeWebhookDeliver, webhookHandler.Handle)

	// Start cron scheduler using the same Redis options as the server.
	scheduler := asynq.NewScheduler(redisOpt, nil)

	if _, err := scheduler.Register("@every 6h", asynq.NewTask(worker.TaskOpenRouterScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register openrouter")
		return err
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register litellm")
		return err
	}
	// HuggingFace tasks get an explicit 90s asynq-level timeout and capped retries.
	// The HuggingFace API has been observed stalling mid-body for minutes; without
	// a task timeout asynq never cancels the context and the slot hangs indefinitely.
	// The handler also applies an inner 80s deadline for belt-and-suspenders protection.
	// MaxRetry=3 prevents a retry storm when the API is degraded.
	if _, err := scheduler.Register("@every 24h",
		asynq.NewTask(worker.TaskHuggingFaceScrape, nil),
		asynq.Timeout(90*time.Second),
		asynq.MaxRetry(3),
	); err != nil {
		log.Error().Err(err).Msg("scheduler: register huggingface")
		return err
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskOpenAIScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register openai")
		return err
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskAnthropicScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register anthropic")
		return err
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskGeminiScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register gemini")
		return err
	}

	// Benchmark scraper cron schedules — daily.
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskSWEBenchScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register swebench")
		return err
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiveCodeBenchScrape, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register livecodebench")
		return err
	}
	// Daily recomputation is a safety net and refreshes per-dimension freshness.
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskRecomputeCapabilityScores, nil)); err != nil {
		log.Error().Err(err).Msg("scheduler: register recompute_capability_scores")
		return err
	}

	if err := scheduler.Start(); err != nil {
		log.Error().Err(err).Msg("scheduler: start")
		return err
	}
	defer scheduler.Shutdown()

	// Enqueue one-shot scrapes so the database is populated immediately after
	// a fresh deploy. The @every cron schedules only fire after the full
	// interval elapses, which would leave the DB empty for hours on first boot.
	//
	// asynq.Unique(24h) prevents duplicate entries: if a previous deploy
	// crashed mid-scrape and left a task pending in Redis, re-enqueueing
	// the same task type without Unique would pile up additional copies.
	// ErrDuplicateTask is returned (not a fatal error) when deduplication fires.
	client := asynq.NewClient(redisOpt)
	defer client.Close()

	type startupTask struct {
		taskType string
		label    string
		opts     []asynq.Option
	}
	initialTasks := []startupTask{
		{worker.TaskOpenRouterScrape, "openrouter", nil},
		{worker.TaskLiteLLMScrape, "litellm", nil},
		// HuggingFace startup task: 90s task-level timeout + MaxRetry=3 to prevent
		// a hung slot from blocking all 10 worker goroutines on fresh deploys.
		// The handler wraps the actual scrape in an 80s inner deadline as well.
		{worker.TaskHuggingFaceScrape, "huggingface", []asynq.Option{
			asynq.Timeout(90 * time.Second),
			asynq.MaxRetry(3),
		}},
		{worker.TaskOpenAIScrape, "openai", nil},
		{worker.TaskAnthropicScrape, "anthropic", nil},
		{worker.TaskGeminiScrape, "gemini", nil},
		// Benchmark scrapers — initial run on startup.
		{worker.TaskSWEBenchScrape, "swebench", nil},
		{worker.TaskLiveCodeBenchScrape, "livecodebench", nil},
		// Intelligence tasks — initial run on startup.
		{worker.TaskRecomputeCapabilityScores, "recompute_capability_scores", nil},
	}
	for _, t := range initialTasks {
		opts := append([]asynq.Option{asynq.Unique(24 * time.Hour)}, t.opts...)
		_, err := client.Enqueue(asynq.NewTask(t.taskType, nil), opts...)
		if err != nil && err != asynq.ErrDuplicateTask {
			log.Warn().Err(err).Str("task", t.label).Msg("initial scrape enqueue failed")
		}
	}
	log.Info().Msg("enqueued initial scrape tasks")

	// Start the asynq server. srv.Start is non-blocking in asynq v0.26 — it
	// spawns background goroutines and returns nil immediately. A non-nil
	// return means startup itself failed (e.g. Redis unreachable), which is
	// fatal. Signal handling and graceful shutdown are managed below via
	// srv.Shutdown(), which blocks until in-flight tasks finish.
	log.Info().Str("env", cfg.AppEnv).Int("concurrency", workerConcurrency).Msg("worker starting")
	if err := srv.Start(mux); err != nil {
		log.Error().Err(err).Msg("worker: asynq server failed to start")
		return err
	}
	log.Info().Msg("worker started")

	// Start a minimal HTTP server for Railway health checks. The worker is a
	// pure asynq consumer with no Fiber router, but Railway expects every
	// service to respond on healthcheckPath ("/health"). We bind to APP_PORT
	// (same env var the API uses) so the shared railway.json config works for
	// both services without modification.
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		if err := db.Ping(ctx); err != nil {
			dbStatus = "error"
		}
		redisStatus := "ok"
		if err := redisClient.Ping(ctx).Err(); err != nil {
			redisStatus = "error"
		}
		status := "ok"
		code := http.StatusOK
		if dbStatus != "ok" || redisStatus != "ok" {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"status":"` + status + `","db":"` + dbStatus + `","redis":"` + redisStatus + `"}`))
	})
	// healthSrvErrCh receives non-ErrServerClosed errors from the health
	// server goroutine. A send means ListenAndServe failed unexpectedly
	// (e.g. port already in use, or an accept/serve failure after the server
	// was already listening), so Railway health checks are broken — treat it
	// as fatal and exit non-zero.
	healthSrvErrCh := make(chan error, 1)
	healthSrv := &http.Server{Addr: ":" + cfg.AppPort, Handler: healthMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info().Str("port", cfg.AppPort).Msg("health server listening")
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("health server error")
			healthSrvErrCh <- err
		}
	}()

	// Start the internal Prometheus metrics server on its own port. The worker
	// increments the pipeline counters inside this process, so this endpoint is
	// the only path those samples have out of it — without it the data-pipeline
	// dashboard stays empty and the scraper-failure alert can never fire.
	//
	// An empty METRICS_PORT disables the server, matching cmd/api. A bind
	// failure is logged rather than fatal: losing telemetry must not take the
	// data pipeline down with it, and the failure is loud in the logs.
	metricsSrv := metrics.NewServer(cfg.MetricsPort)
	if metricsSrv == nil {
		// Metrics were explicitly disabled with an empty METRICS_PORT. Say so
		// loudly: silently losing the pipeline counters is precisely the
		// failure this endpoint exists to prevent.
		log.Warn().Msg("metrics endpoint disabled (METRICS_PORT is empty)")
	} else {
		go func() {
			log.Info().Str("addr", metricsSrv.Addr).Msg("starting metrics server")
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Str("addr", metricsSrv.Addr).Msg(
					"metrics server error — this process is now unscrapeable")
			}
		}()
	}

	// Publish data-freshness gauges on a ticker, independently of the scrape
	// pipeline. Updating them only when a scrape succeeded would let a scraper
	// that stopped running freeze the gauges at their last good values, so
	// time() - llm_source_last_success_timestamp_seconds would stay small and the
	// staleness alert could never fire — the exact outage it exists to catch.
	//
	// The sampler is seeded immediately (so the gauges are populated before the
	// first tick) and stopped on shutdown, before the metrics listener, so its
	// final published sample is still scrapeable during the drain.
	sampler := worker.NewFreshnessSampler(db, worker.DefaultStaleAfter)
	samplerCtx, stopSampler := context.WithCancel(context.Background())
	defer stopSampler()
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		ticker := time.NewTicker(freshnessSampleInterval)
		defer ticker.Stop()
		sampleFreshness(samplerCtx, log, sampler)
		for {
			select {
			case <-samplerCtx.Done():
				return
			case <-ticker.C:
				sampleFreshness(samplerCtx, log, sampler)
			}
		}
	}()

	// Block until SIGINT/SIGTERM or a health server bind failure.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case <-quit:
		log.Info().Msg("worker shutting down...")
	case healthErr := <-healthSrvErrCh:
		// Health server failed unexpectedly — Railway health checks will never
		// pass. Initiate a graceful asynq shutdown and exit non-zero so the
		// platform restarts the container.
		log.Error().Err(healthErr).Msg("health server error — initiating shutdown")
		// This path returns a non-nil error, so the process exits non-zero and
		// the OS reclaims the metrics port; there is nothing to drain here.
		shutdownDone := make(chan struct{})
		go func() {
			srv.Shutdown()
			close(shutdownDone)
		}()
		select {
		case <-shutdownDone:
		case <-time.After(10 * time.Second):
			log.Warn().Msg("worker shutdown timed out during health-triggered exit")
		}
		return healthErr
	}

	// One budget covers the whole graceful shutdown — health, the asynq drain,
	// the freshness sampler, and the metrics listener — so a stuck component
	// cannot push the process past Railway's SIGKILL window. Each phase returns
	// as soon as it is done; the deadline only bites when something is genuinely
	// hung.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()

	// Stop the health listener first so Railway sees the service draining.
	_ = healthSrv.Shutdown(shutdownCtx)

	// srv.Shutdown blocks until all in-flight tasks complete.
	shutdownDone := make(chan struct{})
	go func() {
		srv.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-shutdownCtx.Done():
		log.Warn().Msg("worker shutdown timed out — forcing exit")
		return fmt.Errorf("worker shutdown timed out")
	}

	// Stop the freshness sampler inside the same budget, before the metrics
	// listener: samples already published stay scrapeable while the endpoint
	// drains, and stopping here keeps the sampler from issuing a fresh query
	// against a pool that is about to be closed.
	stopSampler()
	select {
	case <-samplerDone:
	case <-shutdownCtx.Done():
		log.Warn().Msg("freshness sampler did not stop before shutdown deadline")
	}

	// Stop the metrics listener last, inside the same budget: a scrape during
	// the drain can still observe the final counter increments produced by
	// in-flight tasks.
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(shutdownCtx)
	}

	return nil
}
