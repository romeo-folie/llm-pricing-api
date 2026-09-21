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
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/cache"
	"llm-pricing-api/internal/config"
	"llm-pricing-api/internal/database"
	"llm-pricing-api/internal/health"
	"llm-pricing-api/internal/logger"
	"llm-pricing-api/internal/mailer"
	"llm-pricing-api/internal/metrics"
	"llm-pricing-api/internal/middleware"
	internalotel "llm-pricing-api/internal/otel"
	"llm-pricing-api/internal/review"
	"llm-pricing-api/internal/signup"
)

const (
	// requestTimeout bounds a single non-streaming /v1 request. It sits well
	// above the p99 latency target (<200ms) so it only fires on a genuine
	// stall, while still releasing a request whose dependency never answers.
	requestTimeout = 15 * time.Second

	// watchdogInterval is how often the liveness watchdog probes dependencies.
	watchdogInterval = 30 * time.Second

	// watchdogFailureThreshold is the number of consecutive failed probes that
	// trigger a restart. At one probe per 30s that is ~90s of sustained
	// failure: long enough to ride out a blip, short enough to catch a wedge.
	watchdogFailureThreshold = 3
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		// Use plain stderr before the logger is set up.
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// API-only validation: RESEND_API_KEY and MAGIC_LINK_SIGNING_SECRET are not
	// needed by the worker binary, so they are validated here rather than in
	// config.Load() (which is shared). This prevents the worker from failing to
	// start when these vars are absent from its environment.
	// cfg.AppEnv already has the "development" default applied by config.Load();
	// no need to re-read os.Getenv here.
	if cfg.AppEnv != "development" {
		if cfg.ResendAPIKey == "" {
			fmt.Fprintf(os.Stderr, "config error: RESEND_API_KEY is required in non-development environments\n")
			os.Exit(1)
		}
		if cfg.MagicLinkSigningSecret == "" {
			fmt.Fprintf(os.Stderr, "config error: MAGIC_LINK_SIGNING_SECRET is required in non-development environments\n")
			os.Exit(1)
		}
	}
	if len(cfg.MagicLinkSigningSecret) > 0 && len(cfg.MagicLinkSigningSecret) < 32 {
		fmt.Fprintf(os.Stderr, "config error: MAGIC_LINK_SIGNING_SECRET must be at least 32 bytes (got %d)\n", len(cfg.MagicLinkSigningSecret))
		os.Exit(1)
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	// Built twice: this first logger has no OTLP tee, so it can report failures
	// while the tee is still being constructed.
	log := logger.New(logger.Config{
		ServiceName: cfg.OTELServiceName,
		Environment: cfg.AppEnv,
		Level:       parseLogLevel(cfg.LogLevel),
	})

	ctx := context.Background()

	otelCfg := internalotel.Config{
		ServiceName:    cfg.OTELServiceName,
		ServiceVersion: "0.1.0",
		Environment:    cfg.AppEnv,
		OTLPEndpoint:   cfg.OTELEndpoint,
	}

	logResource, resErr := internalotel.NewResource(ctx, otelCfg)
	if resErr != nil {
		log.Fatal().Err(resErr).Msg("failed to build OTel resource")
	}

	// Ship structured logs through the same OTLP endpoint as traces. The writer
	// is nil when no endpoint is configured, so local runs are unaffected.
	logWriter, logShutdown, logErr := logger.NewOTLPWriter(ctx, logger.OTLPConfig{
		Endpoint:    cfg.OTELEndpoint,
		ServiceName: cfg.OTELServiceName,
		Resource:    logResource,
		MinLevel:    zerolog.InfoLevel, // debug stays on stdout only
	})
	if logErr != nil {
		log.Fatal().Err(logErr).Msg("failed to initialise OTLP log exporter")
	}

	log = logger.New(logger.Config{
		ServiceName: cfg.OTELServiceName,
		Environment: cfg.AppEnv,
		Level:       parseLogLevel(cfg.LogLevel),
		OTLPWriter:  logWriter,
	})

	// Initialise OpenTelemetry SDK.  When OTELEndpoint is empty the SDK
	// defaults to a no-op provider — safe to call unconditionally.
	otelShutdown, err := internalotel.Init(ctx, otelCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialise OTel SDK")
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutErr := otelShutdown(shutCtx); shutErr != nil {
			log.Error().Err(shutErr).Msg("OTel SDK shutdown error")
		}
		if shutErr := logShutdown(shutCtx); shutErr != nil {
			log.Error().Err(shutErr).Msg("OTLP log shutdown error")
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

	// Shared dependency checker for /health and the liveness watchdog: both need
	// the same bounded probe, and neither may hang on an unreachable dependency.
	healthChecker := health.NewChecker(
		db.Ping,
		func(ctx context.Context) error { return redisClient.Ping(ctx).Err() },
		health.DefaultCheckTimeout,
	)

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
		ProxyHeader: func() string {
			if len(trustedProxies) > 0 {
				return fiber.HeaderXForwardedFor
			}
			return ""
		}(),
	})

	// CORS: allow docs UI (llmrates.live) and local frontend to call the API
	// directly (e.g. API reference "Try it out" requests).
	allowedOrigins := os.Getenv("CORS_ALLOW_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "https://llmrates.live,https://www.llmrates.live,http://localhost:3000"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: allowedOrigins,
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
		MaxAge:       86400,
	}))

	// Middleware order: CORS + security headers first, then OTel tracing,
	// Prometheus instrumentation, request logger, and panic recovery.
	app.Use(middleware.Security())
	app.Use(otelfiber.Middleware())
	app.Use(metrics.PrometheusMiddleware())
	app.Use(requestLogger(log))
	app.Use(recover.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		// healthChecker bounds both pings with one shared deadline, so an
		// unreachable dependency yields a fast 503 instead of hanging the
		// endpoint — a hanging /health is what let the process wedge for four
		// days without Railway ever reporting a failure.
		st := healthChecker.Check(c.Context())

		dbStatus := statusLabel(st.DBOK)
		redisStatus := statusLabel(st.RedisOK)

		overall := "ok"
		code := fiber.StatusOK
		if !st.OK() {
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
	// Every /v1 request is bounded end to end: RequestTimeout runs first so the
	// auth, cache, rate-limit and handler stages all inherit the deadline that
	// eventually releases a connection blocked on pool acquire. Streaming
	// routes are excluded inside the middleware.
	unkeyVerifier := middleware.NewUnkeyClient(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
	v1 := app.Group("/v1",
		middleware.RequestTimeout(requestTimeout),
		middleware.Auth(unkeyVerifier, redisClient, cfg.UnkeyAPIID),
		middleware.Cache(redisClient),
		middleware.RateLimit(redisClient),
	)

	// Register magic-link signup auth routes (public — no Unkey required).
	// An IP-based rate limiter is applied to the auth group to prevent abuse.
	signupStore := signup.NewStore(db)
	ml := mailer.New(cfg.ResendAPIKey, cfg.EmailFrom)
	unkeyIssuer := signup.NewUnkeyIssuer(cfg.UnkeyRootKey, cfg.UnkeyAPIID)
	// Abuse controls for the signup flow. request-link emails an arbitrary
	// address, so the per-email cooldown is what prevents it being used to
	// mail-bomb a third party; the per-IP limit and disposable-domain block
	// bound free-key farming. Every control fails open on a Redis error.
	abuseGuard := signup.NewAbuseGuard(
		redisClient,
		signup.DefaultAbuseConfig(cfg.MagicLinkSigningSecret),
		log,
	)
	authHandler := auth.New(signupStore, ml, unkeyIssuer, abuseGuard, auth.Config{
		SigningSecret:           cfg.MagicLinkSigningSecret,
		MagicLinkTTLMinutes:     cfg.MagicLinkTTLMinutes,
		MagicLinkBaseURL:        cfg.MagicLinkBaseURL,
		MagicLinkPath:           cfg.MagicLinkPath,
		SignupSessionCookieName: cfg.SignupSessionCookieName,
		SignupSessionTTLHours:   cfg.SignupSessionTTLHours,
		SignupSessionSecure:     cfg.SignupSessionSecure,
		SignupEnabled:           cfg.SignupEnabled,
	}, log)
	// Rate-limit all auth routes first (DDoS protection even when signup is
	// disabled). Handler-level checks in auth.Handler manage the 503 response
	// when SIGNUP_ENABLED=false.
	// Bounded like /v1: the signup routes are public and hit the same database
	// pool, so an unbounded request there could exhaust it exactly as /v1 could.
	// Work that must outlive the request opts out explicitly with
	// context.WithoutCancel (see internal/auth).
	authGroup := app.Group("/auth",
		middleware.RequestTimeout(requestTimeout),
		middleware.IPRateLimit(redisClient, log),
	)
	auth.Register(authGroup, authHandler)

	// Register public discovery routes outside the auth group.
	handlers.RegisterDiscovery(app, db, redisClient)

	// Register all /v1/ endpoint groups. Every /v1 route is authenticated —
	// /v1/compare and /v1/recommend are free-tier but still require an API key,
	// and the frontend reaches them via server-side Next.js route handlers that
	// attach the key. Registering them on a separate app.Group("/v1") would not
	// bypass auth in any case: the USE /v1 middleware above matches by prefix.
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
	admin := app.Group("/admin",
		middleware.RequestTimeout(requestTimeout),
		basicauth.New(basicauth.Config{
			Users: map[string]string{cfg.AdminUser: cfg.AdminPassword},
		}),
	)
	admin.Get("/review", reviewHandler.List)
	admin.Post("/review/:id/approve", reviewHandler.Approve)
	admin.Post("/review/:id/reject", reviewHandler.Reject)

	// Start the internal Prometheus metrics server on a separate port. It is
	// intentionally NOT behind the public-facing Fiber instance so that
	// /metrics is never reachable via the public API port. An empty
	// METRICS_PORT disables the listener.
	if metricsServer := metrics.NewServer(cfg.MetricsPort); metricsServer != nil {
		go func() {
			log.Info().Str("addr", metricsServer.Addr).Msg("starting metrics server")
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("metrics server error")
			}
		}()
		defer func() {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = metricsServer.Shutdown(shutCtx)
		}()
	} else {
		log.Warn().Msg("metrics endpoint disabled (METRICS_PORT is empty)")
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	// Liveness watchdog. Railway's healthcheck is a one-time deploy gate, and a
	// wedged process never exits, so restartPolicyType: ON_FAILURE never fired
	// during the four-day outage. Probe the dependencies on an interval and exit
	// non-zero after enough consecutive failures, handing the container to the
	// platform's restart policy.
	watchdogTrips := make(chan struct{}, 1)
	go runWatchdog(ctx, healthChecker, health.NewWatchdog(watchdogFailureThreshold), log, watchdogTrips)

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
	case <-watchdogTrips:
		// The database has been unreachable across several consecutive probes,
		// which is the signature of the pool exhaustion that wedged the process
		// for four days. Shut down and exit non-zero so Railway's ON_FAILURE
		// policy restarts the container.
		log.Error().Msg("watchdog: database unreachable repeatedly — shutting down for restart")
		if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
			log.Error().Err(err).Msg("watchdog shutdown error")
		}
		// Flush traces before exiting: os.Exit skips the deferred shutdown, and
		// the traces describing this failure are exactly what must survive it.
		flushCtx, cancelFlush := context.WithTimeout(context.Background(), 2*time.Second)
		if err := otelShutdown(flushCtx); err != nil {
			log.Error().Err(err).Msg("OTel flush during watchdog shutdown failed")
		}
		// Same reasoning for logs: the lines describing a watchdog trip are the
		// ones worth keeping.
		if err := logShutdown(flushCtx); err != nil {
			log.Error().Err(err).Msg("OTLP log flush during watchdog shutdown failed")
		}
		cancelFlush()
		os.Exit(1)
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

// statusLabel renders a dependency's health using the strings the /health
// payload has always reported.
func statusLabel(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

// runWatchdog probes the dependencies on a fixed interval and signals when the
// database has been unreachable for enough consecutive checks.
//
// It watches the database specifically, not every dependency. Issue #183 was
// connection-pool exhaustion, which shows up as a failing pool ping (Ping has
// to acquire a connection), whereas a Redis outage is survivable — cache, rate
// limiting and key verification all fall back. Restarting on a Redis blip would
// convert a degraded-but-serving API into a crash loop.
//
// It signals rather than exiting so that main can shut down and flush traces on
// the normal path: a goroutine calling os.Exit would skip that, and could also
// fire in the middle of a graceful shutdown.
func runWatchdog(ctx context.Context, checker *health.Checker, wd *health.Watchdog, log zerolog.Logger, trips chan<- struct{}) {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st := checker.Check(ctx)
			switch {
			case !st.DBOK:
				log.Error().Bool("redis_ok", st.RedisOK).Msg("watchdog: database check failed")
			case !st.RedisOK:
				log.Warn().Msg("watchdog: redis check failed (not fatal — cache, rate limit and auth fail open)")
			}

			if wd.Observe(st.DBOK) {
				select {
				case trips <- struct{}{}:
				default:
					// A trip is already queued; main has not acted yet.
				}
				return
			}
		}
	}
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

		// UserContext, not Context: otelfiber installs the request span there,
		// so this is what puts trace_id/span_id on request log lines.
		l := logger.FromContext(c.UserContext(), base)
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
