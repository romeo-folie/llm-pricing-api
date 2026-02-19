package main

import (
	"context"
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

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		// Logger not yet available — write directly to stderr.
		l := zerolog.New(os.Stderr)
		l.Fatal().Err(err).Msg("config error")
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
		log.Fatal().Err(err).Msg("could not connect to database")
	}
	defer db.Close()

	redisOpt := asynqOptFromURL(cfg.RedisURL)
	srv := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: 10,
		},
	)

	mux := asynq.NewServeMux()

	store := worker.NewPgxStore(db)
	rec := reconciler.New(db)
	rec.SetLogger(log)
	h := worker.NewHandlers(store, rec)
	h.SetLogger(log)

	mux.HandleFunc(worker.TaskOpenRouterScrape, h.HandleOpenRouterScrape)
	mux.HandleFunc(worker.TaskLiteLLMScrape, h.HandleLiteLLMScrape)

	// WebhookDeliveryHandler holds the AES key so it can decrypt secrets at
	// task execution time — secrets are stored encrypted at rest.
	webhookHandler := worker.NewWebhookDeliveryHandler(cfg.WebhookSecretKey)
	mux.HandleFunc(worker.TypeWebhookDeliver, webhookHandler.Handle)

	// Start cron scheduler using the same Redis options as the server.
	scheduler := asynq.NewScheduler(redisOpt, nil)

	if _, err := scheduler.Register("@every 6h", asynq.NewTask(worker.TaskOpenRouterScrape, nil)); err != nil {
		log.Fatal().Err(err).Msg("scheduler: register openrouter")
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil)); err != nil {
		log.Fatal().Err(err).Msg("scheduler: register litellm")
	}

	if err := scheduler.Start(); err != nil {
		log.Fatal().Err(err).Msg("scheduler: start")
	}
	defer scheduler.Shutdown()

	log.Info().Str("env", cfg.AppEnv).Int("concurrency", 10).Msg("worker started")
	if err := srv.Start(mux); err != nil {
		log.Fatal().Err(err).Msg("worker error")
	}

	// Enqueue one-shot scrapes so the database is populated immediately after
	// a fresh deploy. The @every cron schedules only fire after the full
	// interval elapses, which would leave the DB empty for hours on first boot.
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskOpenRouterScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial openrouter scrape enqueue failed")
	}
	if _, err := client.Enqueue(asynq.NewTask(worker.TaskLiteLLMScrape, nil)); err != nil {
		log.Warn().Err(err).Msg("initial litellm scrape enqueue failed")
	}
	log.Info().Msg("enqueued initial scrape tasks")

	// Block until SIGINT or SIGTERM, then shut down gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit
	log.Info().Msg("worker shutting down...")
	srv.Shutdown()
}
