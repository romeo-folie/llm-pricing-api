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
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/worker"
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

// asynqLogger adapts zerolog.Logger to asynq's Logger interface so that
// task errors and retries appear in the structured log output.
type asynqLogger struct{ l zerolog.Logger }

func (a *asynqLogger) Debug(args ...any) { a.l.Debug().Msgf("%v", args) }
func (a *asynqLogger) Info(args ...any)  { a.l.Info().Msgf("%v", args) }
func (a *asynqLogger) Warn(args ...any)  { a.l.Warn().Msgf("%v", args) }
func (a *asynqLogger) Error(args ...any) { a.l.Error().Msgf("%v", args) }
// Fatal logs at Error level rather than calling zerolog.Logger.Fatal (which
// invokes os.Exit and would bypass all deferred cleanup in run()).
func (a *asynqLogger) Fatal(args ...any) { a.l.Error().Msgf("%v", args) }

// main is the entry point. All logic lives in run() so that deferred
// cleanup executes before os.Exit is called on a non-zero exit.
func main() {
	if err := run(); err != nil {
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
	h := worker.NewHandlers(store, rec)
	h.SetLogger(log)

	mux.HandleFunc(worker.TaskOpenRouterScrape, h.HandleOpenRouterScrape)
	mux.HandleFunc(worker.TaskLiteLLMScrape, h.HandleLiteLLMScrape)
	mux.HandleFunc(worker.TaskHuggingFaceScrape, h.HandleHuggingFaceScrape)
	mux.HandleFunc(worker.TaskOpenAIScrape, h.HandleOpenAIScrape)
	mux.HandleFunc(worker.TaskAnthropicScrape, h.HandleAnthropicScrape)
	mux.HandleFunc(worker.TaskGeminiScrape, h.HandleGeminiScrape)

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
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskHuggingFaceScrape, nil)); err != nil {
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

	if err := scheduler.Start(); err != nil {
		log.Error().Err(err).Msg("scheduler: start")
		return err
	}
	defer scheduler.Shutdown()

	// Enqueue one-shot scrapes so the database is populated immediately after
	// a fresh deploy. The @every cron schedules only fire after the full
	// interval elapses, which would leave the DB empty for hours on first boot.
	// NOTE: this block must run before srv.Start(mux) because Start blocks.
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskOpenRouterScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial openrouter scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskLiteLLMScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial litellm scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskHuggingFaceScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial huggingface scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskOpenAIScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial openai scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskAnthropicScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial anthropic scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskGeminiScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial gemini scrape enqueue failed")
	}
	log.Info().Msg("enqueued initial scrape tasks")

	// Run the asynq server in a goroutine — srv.Start blocks until Shutdown
	// is called. Running it in the foreground prevented the health server
	// and signal handler from ever executing.
	//
	// srvErrCh receives the return value of srv.Start so that failures route
	// to the main goroutine for a controlled shutdown rather than calling
	// log.Fatal (which calls os.Exit and skips all deferred cleanup).
	log.Info().Str("env", cfg.AppEnv).Int("concurrency", workerConcurrency).Msg("worker started")
	srvErrCh := make(chan error, 1)
	go func() {
		srvErrCh <- srv.Start(mux)
	}()

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
	healthSrv := &http.Server{Addr: ":" + cfg.AppPort, Handler: healthMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info().Str("port", cfg.AppPort).Msg("health server listening")
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("health server error")
		}
	}()

	// Block until SIGINT/SIGTERM or an unexpected asynq server exit.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case <-quit:
		log.Info().Msg("worker shutting down...")
	case srvErr := <-srvErrCh:
		// srv.Start returned before a shutdown signal — treat as a fatal startup/runtime
		// error. Returning a non-nil error from run() causes main() to call os.Exit(1)
		// so the platform/supervisor registers this as a crash. All deferred cleanup
		// (db.Close, scheduler.Shutdown, redisClient.Close, etc.) still runs because
		// we return via run() rather than calling os.Exit directly.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
		if srvErr != nil {
			log.Error().Err(srvErr).Msg("worker exited unexpectedly")
			return srvErr
		}
		log.Error().Msg("worker stopped without shutdown signal")
		return fmt.Errorf("worker stopped without shutdown signal")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = healthSrv.Shutdown(shutdownCtx)

	// Bound the entire asynq shutdown path — srv.Shutdown() blocks while
	// in-flight tasks complete, and the Start goroutine needs to fully unwind
	// afterwards. A single 10 s deadline covers both phases so that a stuck
	// handler cannot push the total wait past Railway's SIGKILL window.
	// (Using a separate timer only for the srvErrCh drain would leave
	// srv.Shutdown() itself unbounded.)
	shutdownDone := make(chan error, 1)
	go func() {
		srv.Shutdown()           // blocks until in-flight tasks finish
		shutdownDone <- <-srvErrCh // then drain the Start goroutine
	}()
	select {
	case err := <-shutdownDone:
		if err != nil {
			log.Error().Err(err).Msg("worker error on exit")
		}
	case <-time.After(10 * time.Second):
		log.Warn().Msg("worker shutdown timed out — forcing exit")
	}

	return nil
}
