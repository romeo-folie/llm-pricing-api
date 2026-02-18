// Package otel initialises the OpenTelemetry SDK for the llm-pricing-api
// service.  When OTEL_EXPORTER_OTLP_ENDPOINT is set, traces are exported via
// OTLP over gRPC; when the variable is unset or empty the SDK is configured
// with a no-op TracerProvider so the application starts without an external
// collector.
package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds the options required to initialise the OTel SDK.
type Config struct {
	// ServiceName is the logical name of the service (e.g. "llm-pricing-api").
	ServiceName string
	// ServiceVersion is the version string embedded in resource attributes.
	ServiceVersion string
	// Environment is the deployment environment (e.g. "development", "production").
	Environment string
	// OTLPEndpoint is the gRPC endpoint of the OTLP collector
	// (e.g. "localhost:4317").  When empty the SDK falls back to a no-op
	// TracerProvider — zero startup risk without a collector.
	OTLPEndpoint string
}

// Init configures and registers a global OTel TracerProvider and
// TextMapPropagator.  The returned shutdown function must be called before
// process exit to flush buffered spans.
//
// When cfg.OTLPEndpoint is empty, a no-op provider is used and the returned
// shutdown function is a harmless no-op.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	res, err := resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("otel: build resource: %w", err)
	}

	var tp *sdktrace.TracerProvider

	if cfg.OTLPEndpoint == "" {
		// No collector configured — use a no-op provider.
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
	} else {
		exp, expErr := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if expErr != nil {
			return noopShutdown, fmt.Errorf("otel: create OTLP exporter: %w", expErr)
		}

		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
		)
	}

	// Register as global provider so instrumentation libraries pick it up.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return tp.Shutdown, nil
}

func noopShutdown(_ context.Context) error { return nil }
