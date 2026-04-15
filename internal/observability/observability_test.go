package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewRecorder_NilProviders(t *testing.T) {
	t.Parallel()

	r := NewRecorder(nil, nil)
	require.NotNil(t, r)
	require.NotNil(t, r.tracer)
	require.NotNil(t, r.operationDuration)
	require.NotNil(t, r.tokenUsage)
	require.NotNil(t, r.toolCallsTotal)
	require.NotNil(t, r.toolCallDuration)
	require.NotNil(t, r.cliProcessRestarts)
	require.NotNil(t, r.cliMessageParseErrors)
}

func TestNopRecorder(t *testing.T) {
	t.Parallel()

	r := NopRecorder()
	require.NotNil(t, r)

	// Noop recorder should not panic on any recording call.
	ctx := context.Background()

	queryCtx, span := r.StartQuerySpan(ctx, "query", "test-model")
	require.NotNil(t, span)
	r.EndQuerySpan(queryCtx, span, "test-model", time.Second, nil)

	r.RecordTokenUsage(ctx, "test-model", 100, 50)

	toolCtx, toolSpan := r.StartToolCallSpan(ctx, "test-tool")
	require.NotNil(t, toolSpan)
	r.EndToolCallSpan(toolCtx, toolSpan, "test-tool", "success", time.Second)

	r.RecordCLIProcessRestart(ctx)
	r.RecordMessageParseError(ctx)
}

func TestRecorder_QuerySpan(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := NewRecorder(nil, tp)
	ctx := context.Background()

	spanCtx, span := r.StartQuerySpan(ctx, "query", "codex-model")
	require.NotNil(t, span)

	r.EndQuerySpan(spanCtx, span, "codex-model", 500*time.Millisecond, nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "codex.query", spans[0].Name)
}

func TestRecorder_QuerySpanWithError(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := NewRecorder(nil, tp)
	ctx := context.Background()

	spanCtx, span := r.StartQuerySpan(ctx, "query", "codex-model")
	testErr := errors.New("test error")

	r.EndQuerySpan(spanCtx, span, "codex-model", time.Second, testErr)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "should have recorded error event")
}

func TestRecorder_ToolCallSpan(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := NewRecorder(nil, tp)
	ctx := context.Background()

	toolCtx, span := r.StartToolCallSpan(ctx, "bash")
	r.EndToolCallSpan(toolCtx, span, "bash", "success", 200*time.Millisecond)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "codex.tool_call", spans[0].Name)
}

func TestRecorder_OperationDurationMetric(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	spanCtx, span := r.StartQuerySpan(ctx, "query", "test-model")
	r.EndQuerySpan(spanCtx, span, "test-model", time.Second, nil)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricOperationDuration {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric to be recorded", metricOperationDuration)
}

func TestRecorder_TokenUsageMetric(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	r.RecordTokenUsage(ctx, "test-model", 100, 50)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricTokenUsage {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric to be recorded", metricTokenUsage)
}

func TestRecorder_ToolCallMetrics(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	toolCtx, span := r.StartToolCallSpan(ctx, "bash")
	r.EndToolCallSpan(toolCtx, span, "bash", "success", 200*time.Millisecond)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)

	foundCounter := false
	foundHistogram := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case metricToolCallsTotal:
				foundCounter = true
			case metricToolCallDuration:
				foundHistogram = true
			}
		}
	}

	require.True(t, foundCounter, "expected %s metric", metricToolCallsTotal)
	require.True(t, foundHistogram, "expected %s metric", metricToolCallDuration)
}

func TestRecorder_CLIProcessRestartMetric(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	r.RecordCLIProcessRestart(ctx)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricCLIProcessRestarts {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric", metricCLIProcessRestarts)
}

func TestRecorder_MessageParseErrorMetric(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	r.RecordMessageParseError(ctx)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricCLIMessageParseErrors {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric", metricCLIMessageParseErrors)
}

func TestRecorder_AddSpanEvent(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := NewRecorder(nil, tp)
	ctx := context.Background()

	spanCtx, span := r.StartQuerySpan(ctx, "query", "test-model")
	r.AddSpanEvent(span, "test.event")
	r.EndQuerySpan(spanCtx, span, "test-model", time.Second, nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	require.Equal(t, "test.event", spans[0].Events[0].Name)
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	require.Empty(t, classifyError(nil))
	require.Equal(t, "error", classifyError(errors.New("test")))
}
