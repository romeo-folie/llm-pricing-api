package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"

	"llm-pricing-api/internal/config"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisURL},
		asynq.Config{
			Concurrency: 10,
		},
	)

	mux := asynq.NewServeMux()
	// Task handlers will be registered here in Phase 1

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
