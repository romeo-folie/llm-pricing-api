package logger

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/log"
)

// counterValue reads the current value of a single-sample counter family from
// the default registry, returning 0 when the family has no samples yet.
func counterValue(t *testing.T, name string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		var total float64
		for _, m := range family.GetMetric() {
			total += m.GetCounter().GetValue()
		}
		return total
	}
	return 0
}

func TestParseZerologLevel(t *testing.T) {
	cases := []struct {
		in   string
		want zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		// An unrecognised level must not silently become "most severe": info is
		// the safe default because it never suppresses a real record.
		{"nonsense", zerolog.InfoLevel},
		{"", zerolog.InfoLevel},
	}
	for _, tc := range cases {
		if got := parseZerologLevel(tc.in); got != tc.want {
			t.Errorf("parseZerologLevel(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestOtelSeverityMapping(t *testing.T) {
	cases := []struct {
		in   zerolog.Level
		want log.Severity
	}{
		{zerolog.TraceLevel, log.SeverityDebug},
		{zerolog.DebugLevel, log.SeverityDebug},
		{zerolog.InfoLevel, log.SeverityInfo},
		{zerolog.WarnLevel, log.SeverityWarn},
		{zerolog.ErrorLevel, log.SeverityError},
		{zerolog.FatalLevel, log.SeverityFatal},
		{zerolog.PanicLevel, log.SeverityFatal},
	}
	for _, tc := range cases {
		if got := otelSeverity(tc.in); got != tc.want {
			t.Errorf("otelSeverity(%v) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseLogTime(t *testing.T) {
	want := time.Date(2026, 9, 21, 16, 54, 3, 123456789, time.UTC)

	if got, ok := parseLogTime(want.Format(time.RFC3339Nano)); !ok || !got.Equal(want) {
		t.Errorf("RFC3339Nano: got %v (ok=%v); want %v", got, ok, want)
	}
	seconds := want.Format(time.RFC3339)
	if got, ok := parseLogTime(seconds); !ok || got.Unix() != want.Unix() {
		t.Errorf("RFC3339: got %v (ok=%v); want unix %v", got, ok, want.Unix())
	}
	if got, ok := parseLogTime(float64(1_700_000_000)); !ok || got.Unix() != 1_700_000_000 {
		t.Errorf("unix seconds: got %v (ok=%v); want 1700000000", got, ok)
	}
	if _, ok := parseLogTime("not a time"); ok {
		t.Error("garbage timestamp should not parse")
	}
	if _, ok := parseLogTime(nil); ok {
		t.Error("nil timestamp should not parse")
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"authorization", "Authorization", "api_key", "apiKey", "api-key",
		"token", "refresh_token", "secret", "webhook_secret",
		"password", "db_password", "cookie", "set-cookie",
		"credential", "aws_credential", "private_key", "signing_key",
	}
	for _, key := range sensitive {
		if !isSensitiveKey(key) {
			t.Errorf("%q should be treated as sensitive", key)
		}
	}
	// Bare "key" is deliberately not sensitive: it would redact innocuous fields
	// like a count, and every real secret-bearing name carries a qualifier.
	benign := []string{"key_count", "path", "status", "model", "source", "error_type"}
	for _, key := range benign {
		if isSensitiveKey(key) {
			t.Errorf("%q should not be treated as sensitive", key)
		}
	}
}

func TestLogAttribute_RedactsAndMaps(t *testing.T) {
	if got := logAttribute("authorization", "Bearer sk-live-abc").AsString(); got != "[redacted]" {
		t.Errorf("authorization attribute = %q; want [redacted]", got)
	}
	if got := logAttribute("api_key", "sk-live-abc").AsString(); got != "[redacted]" {
		t.Errorf("api_key attribute = %q; want [redacted]", got)
	}
	if got := logAttribute("status", "ok").AsString(); got != "ok" {
		t.Errorf("benign string attribute = %q; want ok", got)
	}
	if got := logAttribute("count", float64(7)); got.AsInt64() != 7 {
		t.Errorf("integer attribute = %v; want 7", got.AsInt64())
	}
	if got := logAttribute("ratio", 1.5); got.AsFloat64() != 1.5 {
		t.Errorf("float attribute = %v; want 1.5", got.AsFloat64())
	}
	if got := logAttribute("ok", true); !got.AsBool() {
		t.Error("bool attribute should be true")
	}
}

// TestOTLPWriter_WriteSplitsLines verifies the writer reassembles whole JSON
// objects across arbitrary Write boundaries, which is what zerolog guarantees.
func TestOTLPWriter_WriteSplitsLines(t *testing.T) {
	w := &otlpWriter{lines: make(chan string, 10)}

	if _, err := w.Write([]byte(`{"a":1}` + "\n" + `{"b":`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("2}\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	close(w.lines)

	var got []string
	for line := range w.lines {
		got = append(got, line)
	}
	want := []string{`{"a":1}`, `{"b":2}`}
	if len(got) != len(want) {
		t.Fatalf("got %d lines (%v); want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q; want %q", i, got[i], want[i])
		}
	}
}

// TestOTLPWriter_DropsWhenQueueFull pins the non-blocking contract: logging must
// never wait on the backend, and a drop must be counted rather than silent.
func TestOTLPWriter_DropsWhenQueueFull(t *testing.T) {
	before := counterValue(t, "llm_logs_dropped_total")

	// No consumer goroutine, so the single-slot queue fills immediately.
	w := &otlpWriter{lines: make(chan string, 1)}
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte(`{"n":1}` + "\n")); err != nil {
			t.Fatalf("Write must never error: %v", err)
		}
	}

	after := counterValue(t, "llm_logs_dropped_total")
	if after <= before {
		t.Errorf("expected drops to be counted: before=%v after=%v", before, after)
	}
}

// TestOTLPWriter_WriteAfterStopIsSafe verifies a late write cannot panic on a
// closed channel — shutdown races are exactly when a stray log line arrives.
func TestOTLPWriter_WriteAfterStopIsSafe(t *testing.T) {
	w := &otlpWriter{lines: make(chan string, 4), done: make(chan struct{})}
	go w.run()
	w.stop()

	if _, err := w.Write([]byte(`{"late":true}` + "\n")); err != nil {
		t.Fatalf("Write after stop must be a no-op, got: %v", err)
	}
}
