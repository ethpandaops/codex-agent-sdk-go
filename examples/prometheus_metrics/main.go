// Package main demonstrates using WithMeterProvider and WithTracerProvider
// for OpenTelemetry observability with the Codex Agent SDK.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	codexsdk "github.com/ethpandaops/codex-agent-sdk-go"
)

func main() {
	fmt.Println("=== Prometheus Metrics Example ===")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Create an OTel MeterProvider with a manual reader.
	// In production, replace with a Prometheus exporter or OTLP exporter:
	//   import "go.opentelemetry.io/otel/exporters/prometheus"
	//   exporter, _ := prometheus.New()
	//   mp := metric.NewMeterProvider(metric.WithReader(exporter))
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			logger.Error("failed to shutdown meter provider", "error", err)
		}
	}()

	// Create an OTel TracerProvider for distributed tracing.
	tp := sdktrace.NewTracerProvider()

	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Error("failed to shutdown tracer provider", "error", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pass both providers to the SDK. When set, the SDK records:
	//   - gen_ai.client.operation.duration (histogram)
	//   - gen_ai.client.token.usage (counter)
	//   - codex.tool_calls_total (counter)
	//   - codex.tool_call_duration_seconds (histogram)
	//   - codex.cli_process_restarts_total (counter)
	//   - codex.cli_message_parse_errors_total (counter)
	//   - One span per Query/QueryStream call
	//   - Child spans per tool invocation
	for msg, err := range codexsdk.Query(ctx, codexsdk.Text("What is 2 + 2?"),
		codexsdk.WithLogger(logger),
		codexsdk.WithMeterProvider(mp),
		codexsdk.WithTracerProvider(tp),
		codexsdk.WithPermissionMode("bypassPermissions"),
	) {
		if err != nil {
			fmt.Printf("Error: %v\n", err)

			return
		}

		switch m := msg.(type) {
		case *codexsdk.AssistantMessage:
			for _, block := range m.Content {
				if textBlock, ok := block.(*codexsdk.TextBlock); ok {
					fmt.Printf("Codex: %s\n", textBlock.Text)
				}
			}

		case *codexsdk.ResultMessage:
			fmt.Println("Query complete")

			if m.Usage != nil {
				fmt.Printf("Tokens: %d in / %d out\n",
					m.Usage.InputTokens, m.Usage.OutputTokens)
			}
		}
	}
}
