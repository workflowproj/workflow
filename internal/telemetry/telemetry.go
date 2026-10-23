// Package telemetry sets up JSON logs, Prometheus metrics and OpenTelemetry tracing.
package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var Version = "dev"

func Logger(level, format string) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(strings.ToUpper(level)))
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if format == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// Tracing enables OTLP export when OTEL_EXPORTER_OTLP_ENDPOINT (or the traces-specific variable)
// is set. It returns a shutdown function that flushes spans.
func Tracing(ctx context.Context, mode string, log *slog.Logger) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName("workflow-platform"), semconv.ServiceVersion(Version), semconv.DeploymentEnvironment(os.Getenv("WFM_ENV")),
		semconv.ServiceInstanceID(mode+"-"+hostname()),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	log.Info("tracing enabled", "exporter", "otlp/http")
	return tp.Shutdown, nil
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// MetricsServer serves /metrics and health probes on a separate port (also used by workers).
func MetricsServer(addr string, ready func(context.Context) error) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready"))
	})
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}
