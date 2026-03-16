package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/cache"
	"llm-pricing-api/internal/config"
	"llm-pricing-api/internal/database"
	internalotel "llm-pricing-api/internal/otel"
	"llm-pricing-api/internal/logger"
	"llm-pricing-api/internal/mailer"
	"llm-pricing-api/internal/metrics"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/review"
	"llm-pricing-api/internal/signup"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		// Use plain stderr before the logger is set up.
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	log := logger.New(logger.Config{
		ServiceName: cfg.OTELServiceName,
		Environment: cfg.AppEnv,
		Level:       parseLogLevel(cfg.LogLevel),
	})

	ctx := context.Background()

	// Initialise OpenTelemetry SDK.  When OTELEndpoint is empty the SDK
	// defaults to a no-op provider — safe to call unconditionally.
	otelShutdown, err := internalotel.Init(ctx, internalotel.Config{
		ServiceName:  cfg.OTELServiceName,
		ServiceVersion: "0.1.0",
		Environment:  cfg.AppEnv,
		OTLPEndpoint: cfg.OTELEndpoint,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialise OTel SDK")
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutErr := otelShutdown(shutCtx); shutErr != nil {
			log.Error().Err(shutErr).Msg("OTel SDK shutdown error")
		}
	}()

	db, err := database.ConnectWithRetry(ctx, cfg.DatabaseURL, 5, log)
	if err != nil {
		log.Fatal().Err(err).Msg("could not connect to database")
	}
	defer db.Close()

	// Connect to Redis
	redisClient, err := cache.Connect(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("could not connect to redis")
	}
	defer redisClient.Close()

	// Wire OTel instrumentation onto the Redis client.
	if err := redisotel.InstrumentTracing(redisClient); err != nil {
		log.Fatal().Err(err).Msg("could not instrument redis with OTel tracing")
	}

	reviewStore := review.NewPgxStore(db)
	reviewHandler := review.NewHandler(reviewStore)

	// Read trusted proxies from env var TRUSTED_PROXY_CIDRS (comma-separated).
	// When unset, EnableTrustedProxyCheck is disabled and c.IP() returns the
	// peer address directly. Set TRUSTED_PROXY_CIDRS to enable proxy-aware IP
	// extraction (e.g. "10.0.0.0/8,172.16.0.0/12" for RFC 1918 LB ranges).
	var trustedProxies []string
	if v := os.Getenv("TRUSTED_PROXY_CIDRS"); v != "" {
		for _, cidr := range strings.Split(v, ",") {
			entry := strings.TrimSpace(cidr)
			if entry == "" {
				continue
			}
			// Accept either CIDR notation or plain IP.
			if _, _, err := net.ParseCIDR(entry); err == nil {
				trustedProxies = append(trustedProxies, entry)
			} else if net.ParseIP(entry) != nil {
				trustedProxies = append(trustedProxies, entry)
			} else {
				log.Warn().Str("entry", entry).Msg("TRUSTED_PROXY_CIDRS: skipping invalid entry")
			}
		}
	}

	app := fiber.New(fiber.Config{
		AppName: "llm-pricing-api",
		// ErrorHandler serialises all errors as RFC 7807 Problem Details
		// with Content-Type: application/problem+json.
		ErrorHandler: api.ErrorHandler,
		// Only enable proxy trust when explicit CIDRs are configured.
		// Without real CIDRs, trusting all proxies allows XFF spoofing.
		// ProxyHeader is only set when trusted-proxy checking is active;
		// setting it unconditionally lets clients spoof X-Forwarded-For
		// even when EnableTrustedProxyCheck is false.
		EnableTrustedProxyCheck: len(trustedProxies) > 0,
		TrustedProxies:          trustedProxies,
		ProxyHeader:             func() string {
			if len(trustedProxies) > 0 {
				return fiber.HeaderXForwardedFor
			}
			return ""
		}(),
	})

	// Middleware order: security headers first, then OTel tracing, Prometheus
	// instrumentation, request logger, and panic recovery.
	app.Use(middleware.Security())
	app.Use(otelfiber.Middleware())
	app.Use(metrics.PrometheusMiddleware())
	app.Use(requestLogger(log))
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

	// /v1 routes: require Unkey API key authentication, per-key rate limiting,
	// and Redis response caching for cacheable GET endpoints.
	// /health and discovery endpoints are registered outside this group so they
	// are exempt from auth.
	unkeyVerifier := middleware.NewUnkeyClient(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
	v1 := app.Group("/v1",
		middleware.Auth(unkeyVerifier, redisClient, cfg.UnkeyAPIID),
		middleware.Cache(redisClient),
		middleware.RateLimit(redisClient),
	)

	// Register magic-link signup auth routes (public — no Unkey required).
	// An IP-based rate limiter is applied to the auth group to prevent abuse.
	signupStore := signup.NewStore(db)
	ml := mailer.New(cfg.ResendAPIKey, cfg.EmailFrom)
	authHandler := auth.New(signupStore, ml, auth.Config{
		SigningSecret:           cfg.MagicLinkSigningSecret,
		MagicLinkTTLMinutes:     cfg.MagicLinkTTLMinutes,
		MagicLinkBaseURL:        cfg.MagicLinkBaseURL,
		MagicLinkPath:           cfg.MagicLinkPath,
		SignupSessionCookieName: cfg.SignupSessionCookieName,
		SignupSessionTTLHours:   cfg.SignupSessionTTLHours,
		SignupSessionSecure:     cfg.SignupSessionSecure,
		SignupEnabled:           cfg.SignupEnabled,
	}, log)
	authGroup := app.Group("/auth", middleware.IPRateLimit(redisClient, log))
	auth.Register(authGroup, authHandler)

	// Register public discovery routes outside the auth group.
	handlers.RegisterDiscovery(app, db, redisClient)

	// Register all /v1/ endpoint groups.
	handlers.RegisterFree(v1, db, redisClient)
	if err := handlers.RegisterDev(v1, db, redisClient); err != nil {
		log.Fatal().Err(err).Msg("failed to register dev handlers")
	}
	handlers.RegisterPro(v1, db, redisClient, cfg.WebhookSecretKey, log)
	if err := handlers.RegisterSSE(v1, redisClient); err != nil {
		log.Fatal().Err(err).Msg("failed to register SSE handler")
	}

	// /admin routes are protected by HTTP Basic Auth.
	// Credentials are read from ADMIN_USER / ADMIN_PASSWORD env vars.
	admin := app.Group("/admin", basicauth.New(basicauth.Config{
		Users: map[string]string{cfg.AdminUser: cfg.AdminPassword},
	}))
	admin.Get("/review", reviewHandler.List)
	admin.Post("/review/:id/approve", reviewHandler.Approve)
	admin.Post("/review/:id/reject", reviewHandler.Reject)

	// Start internal Prometheus metrics server on a separate port.
	// This server is intentionally NOT behind the public-facing Fiber instance
	// so that /metrics is never reachable via the public API port.
	if cfg.MetricsPort != "" {
		metricsAddr := fmt.Sprintf(":%s", cfg.MetricsPort)
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.Handler())
		metricsServer := &http.Server{
			Addr:         metricsAddr,
			Handler:      metricsMux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		go func() {
			log.Info().Str("addr", metricsAddr).Msg("starting metrics server")
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("metrics server error")
			}
		}()
		defer func() {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = metricsServer.Shutdown(shutCtx)
		}()
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	addr := fmt.Sprintf(":%s", cfg.AppPort)
	log.Info().Str("addr", addr).Str("env", cfg.AppEnv).Msg("starting api")

	serverErr := make(chan error, 1)
	go func() {
		if err := app.Listen(addr); err != nil {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Error().Err(err).Msg("server error")
		return
	case <-quit:
		log.Info().Msg("shutting down...")
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Error().Err(err).Msg("shutdown error")
		}
	}
}

// parseLogLevel converts a LOG_LEVEL string to a zerolog.Level.
// Unknown or empty strings default to InfoLevel.
func parseLogLevel(s string) zerolog.Level {
	l, err := zerolog.ParseLevel(s)
	if err != nil {
		return zerolog.InfoLevel
	}
	return l
}

// requestLogger returns a Fiber middleware that logs each completed request
// with zerolog, injecting OTel trace_id and span_id when a span is active.
//
// Because Fiber's ErrorHandler runs after all middleware has unwound,
// c.Response().StatusCode() still returns the default 200 when an error is
// being returned. We therefore derive the status from the error itself.
func requestLogger(base zerolog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start)

		// Derive the real status code: when an error is present the
		// ErrorHandler has not yet written the response, so read the
		// intended status directly from the error type.
		status := c.Response().StatusCode()
		if err != nil {
			switch e := err.(type) {
			case *api.ProblemDetail:
				status = e.Status
			case *fiber.Error:
				status = e.Code
			default:
				status = fiber.StatusInternalServerError
			}
		}

		l := logger.FromContext(c.Context(), base)
		event := l.Info()
		if err != nil {
			event = l.Error().Err(err)
		}
		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Dur("latency_ms", duration).
			Msg("request")

		return err
	}
}
