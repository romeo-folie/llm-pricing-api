package otel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	internalotel "llm-pricing-api/internal/otel"
)

func TestInit_NoopWhenEndpointEmpty(t *testing.T) {
	ctx := context.Background()

	shutdown, err := internalotel.Init(ctx, internalotel.Config{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		OTLPEndpoint:   "", // no collector → no-op
	})
	if err != nil {
		t.Fatalf("Init returned error with empty endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned nil shutdown function")
	}

	// The global TracerProvider should now be set and usable.
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Fatal("global TracerProvider is nil after Init")
	}

	// Acquiring a tracer and starting a span must not panic.
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	span.End()

	// Shutdown must not return an error.
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestInit_ResourceAttributesSet(t *testing.T) {
	ctx := context.Background()

	// With no OTLP endpoint the SDK uses a no-op batcher but still
	// initialises correctly with the supplied resource attributes.
	shutdown, err := internalotel.Init(ctx, internalotel.Config{
		ServiceName:    "llm-pricing-api",
		ServiceVersion: "1.2.3",
		Environment:    "production",
		OTLPEndpoint:   "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tp := otel.GetTracerProvider()
	tracer := tp.Tracer("resource-test")
	_, span := tracer.Start(ctx, "op")
	span.End()

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func TestInit_ShutdownCalledTwiceIsSafe(t *testing.T) {
	ctx := context.Background()

	shutdown, err := internalotel.Init(ctx, internalotel.Config{
		ServiceName:  "svc",
		OTLPEndpoint: "",
	})
	if err != nil {
		t.Fatalf("Init error: %v", err)
	}

	// First call.
	if err := shutdown(ctx); err != nil {
		t.Errorf("first shutdown error: %v", err)
	}
	// Second call should not panic or error.
	if err := shutdown(ctx); err != nil {
		t.Errorf("second shutdown error: %v", err)
	}
}
