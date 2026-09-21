package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"

	"llm-pricing-api/internal/metrics"
)

// Defaults for the OTLP log pipeline. The queue is deliberately generous —
// log records are small and the SDK batches them — but bounded, because
// unbounded buffering trades a blocked request for unbounded memory.
const (
	defaultLogQueueSize      = 2048
	defaultLogExportInterval = 5 * time.Second
	defaultLogExportTimeout  = 10 * time.Second
)

// OTLPConfig configures shipping zerolog JSON lines to the OTLP collector.
type OTLPConfig struct {
	// Endpoint is the collector's OTLP gRPC endpoint (e.g. "collector:4317"),
	// sharing the trace exporter's endpoint. Empty disables shipping: the
	// writer is nil and the shutdown function is a no-op, so local development
	// needs no special casing.
	Endpoint string
	// ServiceName is the logger name within the provider.
	ServiceName string
	// Resource identifies this process. Pass otel.NewResource's result so Loki
	// labels and Tempo resource attributes describe the same service.
	Resource *resource.Resource
	// MinLevel is the lowest zerolog level that is shipped. Records below it
	// never leave the process, which is the primary control on Loki ingest
	// cost. Defaults to InfoLevel — debug stays on stdout only.
	MinLevel zerolog.Level
	// QueueSize bounds the in-process line buffer before drops begin.
	QueueSize int
	// ExportInterval and ExportTimeout configure the SDK batch processor.
	ExportInterval time.Duration
	ExportTimeout  time.Duration
}

// NewOTLPWriter returns a writer that parses zerolog JSON lines and emits them
// as OpenTelemetry log records, together with a shutdown function that flushes
// and closes the provider.
//
// It returns a nil writer when no endpoint is configured. The writer never
// blocks: when its queue is full the record is dropped and
// llm_logs_dropped_total is incremented, because a request must never wait on
// the logging backend.
func NewOTLPWriter(ctx context.Context, cfg OTLPConfig) (io.Writer, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, noop, nil
	}

	exporter, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(cfg.Endpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		return nil, noop, fmt.Errorf("logger: create OTLP log exporter: %w", err)
	}

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultLogQueueSize
	}
	exportInterval := cfg.ExportInterval
	if exportInterval <= 0 {
		exportInterval = defaultLogExportInterval
	}
	exportTimeout := cfg.ExportTimeout
	if exportTimeout <= 0 {
		exportTimeout = defaultLogExportTimeout
	}
	minLevel := cfg.MinLevel
	if minLevel == zerolog.NoLevel {
		minLevel = zerolog.InfoLevel
	}

	providerOpts := []sdklog.LoggerProviderOption{
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter,
			sdklog.WithMaxQueueSize(queueSize),
			sdklog.WithExportInterval(exportInterval),
			sdklog.WithExportTimeout(exportTimeout),
		)),
	}
	if cfg.Resource != nil {
		providerOpts = append(providerOpts, sdklog.WithResource(cfg.Resource))
	}
	provider := sdklog.NewLoggerProvider(providerOpts...)

	w := &otlpWriter{
		logger:   provider.Logger(cfg.ServiceName),
		minLevel: minLevel,
		lines:    make(chan string, queueSize),
		done:     make(chan struct{}),
	}
	go w.run()

	// Shutdown is idempotent: callers flush on the graceful path and again from
	// a defer, and a second provider.Shutdown would otherwise surface as an
	// error at exactly the moment logs matter most.
	var (
		shutdownOnce sync.Once
		shutdownErr  error
	)
	shutdown := func(shutdownCtx context.Context) error {
		shutdownOnce.Do(func() {
			w.stop()
			shutdownErr = provider.Shutdown(shutdownCtx)
		})
		return shutdownErr
	}
	return w, shutdown, nil
}

// otlpWriter implements io.Writer over zerolog's JSON output.
//
// zerolog writes one JSON object per line, so lines are reassembled from
// whatever chunk sizes it happens to pass and handed to a single consumer
// goroutine. Everything a caller does is a non-blocking channel send.
type otlpWriter struct {
	logger   log.Logger
	minLevel zerolog.Level

	mu     sync.Mutex
	buf    []byte
	closed bool
	lines  chan string

	closeOnce sync.Once
	done      chan struct{}
}

// Write buffers p, forwarding every complete line. It always reports success:
// a logging writer that returns an error makes zerolog print to stderr and can
// recursively log.
func (w *otlpWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		// Copy the remainder to the front rather than reslicing, so the
		// consumed prefix does not pin the whole buffer.
		w.buf = append(w.buf[:0], w.buf[idx+1:]...)
		w.enqueue(line)
	}
	return len(p), nil
}

// enqueue hands a line to the consumer, dropping it if the queue is full.
// Callers hold w.mu.
func (w *otlpWriter) enqueue(line string) {
	if w.closed || strings.TrimSpace(line) == "" {
		return
	}
	select {
	case w.lines <- line:
	default:
		metrics.LogsDroppedTotal.Inc()
	}
}

func (w *otlpWriter) run() {
	defer close(w.done)
	for line := range w.lines {
		w.emit(line)
	}
}

// stop flushes any buffered partial line, closes the queue and waits for the
// consumer to drain it. Safe to call more than once.
func (w *otlpWriter) stop() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		if len(w.buf) > 0 {
			w.enqueue(string(w.buf))
			w.buf = nil
		}
		w.closed = true
		close(w.lines)
		w.mu.Unlock()
		<-w.done
	})
}

// emit converts one zerolog JSON line into an OTel log record.
func (w *otlpWriter) emit(line string) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		// Not zerolog JSON (e.g. the development console writer, or a panic
		// trace). stdout already has it; there is nothing to ship.
		return
	}

	levelText, _ := fields["level"].(string)
	level := parseZerologLevel(levelText)
	if level < w.minLevel {
		return
	}

	var rec log.Record
	if ts, ok := parseLogTime(fields["time"]); ok {
		rec.SetTimestamp(ts)
	}
	rec.SetObservedTimestamp(time.Now())
	rec.SetSeverity(otelSeverity(level))
	rec.SetSeverityText(levelText)
	if msg, ok := fields["message"].(string); ok {
		rec.SetBody(log.StringValue(msg))
	}

	for key, value := range fields {
		switch key {
		case "level", "time", "message", "trace_id", "span_id", "service":
			// service is a resource attribute already; the rest carry their own
			// semantics on the record.
			continue
		}
		rec.AddAttributes(log.KeyValue{Key: key, Value: logAttribute(key, value)})
	}

	// Log records pick up the trace context from the context passed to Emit, so
	// reconstruct one from the ids zerolog embedded. Without this a Loki line
	// cannot be linked to its Tempo trace.
	ctx := context.Background()
	if sc, ok := spanContextFromFields(fields); ok {
		ctx = trace.ContextWithSpanContext(ctx, sc)
	}
	w.logger.Emit(ctx, rec)
}

// spanContextFromFields rebuilds a sampled span context from trace_id and
// span_id fields, if both are present and well formed.
func spanContextFromFields(fields map[string]any) (trace.SpanContext, bool) {
	traceHex, _ := fields["trace_id"].(string)
	spanHex, _ := fields["span_id"].(string)
	if traceHex == "" || spanHex == "" {
		return trace.SpanContext{}, false
	}
	traceID, err := trace.TraceIDFromHex(traceHex)
	if err != nil {
		return trace.SpanContext{}, false
	}
	spanID, err := trace.SpanIDFromHex(spanHex)
	if err != nil {
		return trace.SpanContext{}, false
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	return sc, sc.IsValid()
}

// parseZerologLevel converts zerolog's level string to a level, defaulting to
// InfoLevel for anything unrecognised. ParseLevel returns (NoLevel, nil) for an
// empty string, which would otherwise compare below every threshold and ship
// nothing, so NoLevel is treated as unrecognised too.
func parseZerologLevel(s string) zerolog.Level {
	level, err := zerolog.ParseLevel(s)
	if err != nil || level == zerolog.NoLevel {
		return zerolog.InfoLevel
	}
	return level
}

// otelSeverity maps a zerolog level onto the OTel severity scale.
func otelSeverity(level zerolog.Level) log.Severity {
	switch {
	case level <= zerolog.DebugLevel:
		return log.SeverityDebug
	case level == zerolog.InfoLevel:
		return log.SeverityInfo
	case level == zerolog.WarnLevel:
		return log.SeverityWarn
	case level == zerolog.ErrorLevel:
		return log.SeverityError
	default:
		return log.SeverityFatal
	}
}

// parseLogTime accepts the formats zerolog can emit: RFC3339(Nano) strings and
// Unix seconds as JSON numbers.
func parseLogTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return ts, true
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts, true
		}
	case float64:
		sec, frac := math.Modf(t)
		return time.Unix(int64(sec), int64(frac*float64(time.Second))).UTC(), true
	}
	return time.Time{}, false
}

// sensitiveKeys are attribute names whose values must never leave the process,
// matching the "never log API keys, tokens, or credentials" rule in CLAUDE.md.
// Bare "key" is deliberately absent: it would redact innocuous fields like a
// count, and the real secret-bearing names all contain something more specific.
var sensitiveKeys = []string{
	"authorization",
	"api_key", "apikey", "api-key",
	"token",
	"secret",
	"password", "passwd",
	"cookie",
	"credential",
	"private_key", "signing_key",
}

// logAttribute converts a decoded JSON value to an OTel attribute, redacting
// anything that looks like a credential. This is defence in depth, not the
// primary control: code must not log secrets in the first place.
func logAttribute(key string, v any) log.Value {
	if isSensitiveKey(key) {
		return log.StringValue("[redacted]")
	}
	switch t := v.(type) {
	case nil:
		return log.StringValue("")
	case string:
		return log.StringValue(t)
	case bool:
		return log.BoolValue(t)
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return log.Int64Value(int64(t))
		}
		return log.Float64Value(t)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return log.StringValue(fmt.Sprintf("%v", v))
		}
		return log.StringValue(string(encoded))
	}
}

// isSensitiveKey reports whether an attribute name looks credential-bearing.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}
