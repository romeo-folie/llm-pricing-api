package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"llm-pricing-api/internal/cache"
	"llm-pricing-api/internal/config"
	"llm-pricing-api/internal/database"
	"llm-pricing-api/internal/review"
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

	// Connect to Redis
	redisClient, err := cache.Connect(ctx, cfg.RedisURL)
	if err != nil {
		slog.Error("could not connect to redis", "err", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	reviewStore := review.NewPgxStore(db)
	reviewHandler := review.NewHandler(reviewStore)

	app := fiber.New(fiber.Config{
		AppName: "llm-pricing-api",
	})
	app.Use(logger.New())
	app.Use(recover.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		dbStatus := "ok"
		if err := db.Ping(c.Context()); err != nil {
			dbStatus = "error"
		}

		redisStatus := "ok"
		if err := redisClient.Ping(c.Context()).Err(); err != nil {
			redisStatus = "error"
		}

		overall := "ok"
		code := fiber.StatusOK
		if dbStatus != "ok" || redisStatus != "ok" {
			overall = "degraded"
			code = fiber.StatusServiceUnavailable
		}

		return c.Status(code).JSON(fiber.Map{
			"status": overall,
			"db":     dbStatus,
			"redis":  redisStatus,
		})
	})

	admin := app.Group("/admin")
	admin.Get("/review", reviewHandler.List)
	admin.Post("/review/:id/approve", reviewHandler.Approve)
	admin.Post("/review/:id/reject", reviewHandler.Reject)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	addr := fmt.Sprintf(":%s", cfg.AppPort)
	slog.Info("starting api", "addr", addr, "env", cfg.AppEnv)

	go func() {
		if err := app.Listen(addr); err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down...")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
