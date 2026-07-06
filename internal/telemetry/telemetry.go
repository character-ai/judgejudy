// Package telemetry configures the OpenTelemetry SDK for the judgejudy CLI.
package telemetry

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// exportInterval is how often accumulated metrics are pushed to the
// collector. CLI runs are short-lived, so the final flush in Shutdown does
// most of the work; the periodic reader covers long evaluation runs.
const exportInterval = 15 * time.Second

// Setup installs a global OpenTelemetry meter provider that exports metrics
// over OTLP/gRPC. It is enabled by the standard OTEL environment variables:
// if neither OTEL_EXPORTER_OTLP_ENDPOINT nor OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
// is set, Setup does nothing and metrics stay disabled (no-op instruments).
//
// The returned shutdown function flushes pending metrics and must be called
// before the process exits. It is never nil.
func Setup(ctx context.Context, version string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
		return noop, nil
	}

	exporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return noop, err
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName("judgejudy"),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return noop, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(exportInterval))),
	)
	otel.SetMeterProvider(mp)
	return mp.Shutdown, nil
}
