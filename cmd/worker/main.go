package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"

	"llm-pricing-api/internal/cache"
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

	db, err := database.ConnectWithRetry(ctx, cfg.DatabaseURL, 5)
	if err != nil {
		slog.Error("could not connect to database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// RedisAddr normalises full Redis URLs to the bare host:port that
	// asynq's RedisClientOpt.Addr expects.
	redisAddr := cache.RedisAddr(cfg.RedisURL)
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
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
	redisOpt := asynq.RedisClientOpt{Addr: redisAddr}
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

	slog.Info("worker started", "env", cfg.AppEnv, "concurrency", 10)
	if err := srv.Start(mux); err != nil {
		slog.Error("worker error", "err", err)
		os.Exit(1)
	}

	// Block until SIGINT or SIGTERM, then shut down gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit
	slog.Info("worker shutting down...")
	srv.Shutdown()
}
