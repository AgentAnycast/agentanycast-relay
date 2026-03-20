// Package telemetry provides OpenTelemetry setup for the AgentAnycast relay.
package telemetry

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds telemetry configuration.
type Config struct {
	Enabled      bool
	OTLPEndpoint string
	SampleRate   float64
	Version      string
	Logger       *slog.Logger
}

// ShutdownFunc flushes and shuts down the tracer provider.
type ShutdownFunc func(ctx context.Context) error

// Setup initializes the OpenTelemetry tracer provider.
func Setup(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	if !cfg.Enabled || cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.SampleRate > 1.0 {
		cfg.SampleRate = 1.0
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("agentanycast-relay"),
			semconv.ServiceVersion(cfg.Version),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	cfg.Logger.Info("OpenTelemetry tracing enabled",
		"endpoint", cfg.OTLPEndpoint,
		"sample_rate", cfg.SampleRate,
	)

	shutdown := func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(shutdownCtx)
	}

	return shutdown, nil
}

// Tracer returns a named tracer from the global tracer provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
