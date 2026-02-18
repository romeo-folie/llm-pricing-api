// Package logger provides zerolog-based structured logging helpers for the
// llm-pricing-api service.  Every log line emitted within a request handler
// automatically includes the active OTel trace_id and span_id so that logs
// can be correlated with distributed traces.
package logger

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// contextKey is an unexported type used to store the logger inside a context.
type contextKey struct{}

// Config controls how the logger is constructed.
type Config struct {
	// ServiceName is embedded in every log line as the "service" field.
	ServiceName string
	// Environment controls the output format: "production" emits compact JSON;
	// any other value emits a human-readable pretty-print suitable for local
	// development.
	Environment string
	// Level is the minimum zerolog level to emit.  Defaults to Debug when the
	// zero value is used.
	Level zerolog.Level
}

// New creates and returns a zerolog.Logger configured according to cfg.
// The logger writes to os.Stdout with the service name embedded as a
// permanent field.  Pretty-print mode is active in all non-production
// environments.
func New(cfg Config) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var w io.Writer
	if cfg.Environment == "production" {
		w = os.Stdout
	} else {
		w = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	level := cfg.Level
	if level == zerolog.NoLevel {
		level = zerolog.DebugLevel
	}

	return zerolog.New(w).
		Level(level).
		With().
		Timestamp().
		Str("service", cfg.ServiceName).
		Logger()
}

// WithContext stores log in ctx and returns the updated context, following
// the zerolog convention (mirrors zerolog.Logger.WithContext).
func WithContext(ctx context.Context, log zerolog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext returns a zerolog.Logger enriched with the OTel trace_id and
// span_id extracted from ctx.  If no span is active the fields are omitted.
// If a logger was previously stored in ctx via WithContext it is used as the
// base; otherwise the provided fallback is used.
func FromContext(ctx context.Context, fallback zerolog.Logger) zerolog.Logger {
	base := fallback
	if l, ok := ctx.Value(contextKey{}).(zerolog.Logger); ok {
		base = l
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return base
	}

	sc := span.SpanContext()
	return base.With().
		Str("trace_id", sc.TraceID().String()).
		Str("span_id", sc.SpanID().String()).
		Logger()
}
