package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"llm-pricing-api/internal/config"
	"llm-pricing-api/internal/database"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/worker"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Connect to PostgreSQL with retry
	var db *pgxpool.Pool
	for i := range 5 {
		p, err := database.Connect(ctx, cfg.DatabaseURL)
		if err == nil {
			db = p
			break
		}
		slog.Warn("database connect failed, retrying", "attempt", i+1, "err", err)
		time.Sleep(2 * time.Second)
	}
	if db == nil {
		slog.Error("could not connect to database after 5 attempts")
		os.Exit(1)
	}
	defer db.Close()

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisURL},
		asynq.Config{
			Concurrency: 10,
		},
	)

	mux := asynq.NewServeMux()

	store := worker.NewPgxStore(db)
	rec := reconciler.New(db)
	h := worker.NewHandlers(store, rec)

	mux.HandleFunc(worker.TaskOpenRouterScrape, h.HandleOpenRouterScrape)
	mux.HandleFunc(worker.TaskLiteLLMScrape, h.HandleLiteLLMScrape)
	mux.HandleFunc(worker.TaskOpenAIScrape, h.HandleOpenAIScrape)
	mux.HandleFunc(worker.TaskAnthropicScrape, h.HandleAnthropicScrape)
	mux.HandleFunc(worker.TaskGoogleScrape, h.HandleGoogleScrape)
	mux.HandleFunc(worker.TaskMistralScrape, h.HandleMistralScrape)
	mux.HandleFunc(worker.TaskAmazonScrape, h.HandleAmazonScrape)

	// Start cron scheduler
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisURL}
	scheduler := asynq.NewScheduler(redisOpt, nil)

	if _, err := scheduler.Register("@every 6h", asynq.NewTask(worker.TaskOpenRouterScrape, nil)); err != nil {
		slog.Error("scheduler: register openrouter", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskLiteLLMScrape, nil)); err != nil {
		slog.Error("scheduler: register litellm", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskOpenAIScrape, nil)); err != nil {
		slog.Error("scheduler: register openai", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskAnthropicScrape, nil)); err != nil {
		slog.Error("scheduler: register anthropic", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskGoogleScrape, nil)); err != nil {
		slog.Error("scheduler: register google", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskMistralScrape, nil)); err != nil {
		slog.Error("scheduler: register mistral", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(worker.TaskAmazonScrape, nil)); err != nil {
		slog.Error("scheduler: register amazon", "err", err)
		os.Exit(1)
	}

	if err := scheduler.Start(); err != nil {
		slog.Error("scheduler: start", "err", err)
		os.Exit(1)
	}
	defer scheduler.Shutdown()

	// Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		slog.Info("worker shutting down...")
		srv.Shutdown()
	}()

	slog.Info("worker started", "env", cfg.AppEnv, "concurrency", 10)
	if err := srv.Run(mux); err != nil {
		slog.Error("worker error", "err", err)
		os.Exit(1)
	}
}
