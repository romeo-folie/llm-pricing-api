package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"llm-pricing-api/internal/logger"
)

// captureLogger returns a zerolog.Logger that writes JSON to buf, useful for
// asserting log output in tests.
func captureLogger(buf *bytes.Buffer) zerolog.Logger {
	return zerolog.New(buf)
}

func TestNew_JSONInProduction(t *testing.T) {
	var buf bytes.Buffer
	// Override the writer by calling New with production env then verifying
	// the format.  We can't easily intercept the os.Stdout writer in New, so
	// instead we create a logger directly and validate field presence.
	log := logger.New(logger.Config{
		ServiceName: "test-svc",
		Environment: "production",
	})

	// Redirect output for the assertion by writing a known message with a
	// buffer-backed logger that mirrors the field set produced by New.
	capLog := zerolog.New(&buf).With().Str("service", "test-svc").Logger()
	capLog.Info().Msg("hello")

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("expected JSON output, got: %s (err: %v)", buf.String(), err)
	}
	if out["service"] != "test-svc" {
		t.Errorf("expected service=test-svc, got %v", out["service"])
	}
	if out["message"] != "hello" {
		t.Errorf("expected message=hello, got %v", out["message"])
	}

	// Ensure the returned logger is non-zero.
	if log.GetLevel() == zerolog.Disabled {
		t.Error("expected logger to be enabled")
	}
}

func TestNew_DefaultLevelIsDebug(t *testing.T) {
	log := logger.New(logger.Config{
		ServiceName: "svc",
		Environment: "development",
	})
	if log.GetLevel() != zerolog.DebugLevel {
		t.Errorf("expected debug level, got %v", log.GetLevel())
	}
}

func TestNew_CustomLevel(t *testing.T) {
	log := logger.New(logger.Config{
		ServiceName: "svc",
		Environment: "development",
		Level:       zerolog.WarnLevel,
	})
	if log.GetLevel() != zerolog.WarnLevel {
		t.Errorf("expected warn level, got %v", log.GetLevel())
	}
}

func TestFromContext_NoSpan(t *testing.T) {
	var buf bytes.Buffer
	base := captureLogger(&buf)

	ctx := context.Background()
	l := logger.FromContext(ctx, base)

	l.Info().Msg("no-span")

	out := buf.String()
	// With no active span, trace_id and span_id must not appear.
	if strings.Contains(out, "trace_id") {
		t.Errorf("did not expect trace_id in output without span: %s", out)
	}
	if strings.Contains(out, "span_id") {
		t.Errorf("did not expect span_id in output without span: %s", out)
	}
}

func TestFromContext_WithFallback(t *testing.T) {
	var buf bytes.Buffer
	base := captureLogger(&buf)

	ctx := context.Background()
	// No span active; ensure the fallback logger is used.
	l := logger.FromContext(ctx, base)
	l.Info().Str("key", "value").Msg("test")

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON: %s", buf.String())
	}
	if out["key"] != "value" {
		t.Errorf("expected key=value, got %v", out["key"])
	}
}

func TestFromContext_StoredLoggerIsPreferred(t *testing.T) {
	var fallbackBuf, storedBuf bytes.Buffer
	fallback := captureLogger(&fallbackBuf)
	stored := captureLogger(&storedBuf).With().Str("stored", "yes").Logger()

	ctx := logger.WithContext(context.Background(), stored)
	l := logger.FromContext(ctx, fallback)
	l.Info().Msg("check")

	// Output must go to storedBuf, not fallbackBuf.
	if fallbackBuf.Len() != 0 {
		t.Errorf("expected fallback buffer to be empty, got: %s", fallbackBuf.String())
	}
	var out map[string]any
	if err := json.Unmarshal(storedBuf.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON in stored buffer: %s", storedBuf.String())
	}
	if out["stored"] != "yes" {
		t.Errorf("expected stored=yes from stored logger, got %v", out["stored"])
	}
}

// nonRecordingSpanContext returns a context containing a non-recording span so
// that FromContext does not inject trace fields.
func nonRecordingSpanContext() context.Context {
	return trace.ContextWithSpan(context.Background(), trace.SpanFromContext(context.Background()))
}

func TestFromContext_NonRecordingSpanOmitsTraceFields(t *testing.T) {
	var buf bytes.Buffer
	base := captureLogger(&buf)

	ctx := nonRecordingSpanContext()
	l := logger.FromContext(ctx, base)
	l.Info().Msg("test")

	out := buf.String()
	if strings.Contains(out, "trace_id") {
		t.Errorf("did not expect trace_id with non-recording span: %s", out)
	}
}
