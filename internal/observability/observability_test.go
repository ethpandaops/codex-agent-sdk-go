package observability

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdkerrors "github.com/ethpandaops/codex-agent-sdk-go/internal/errors"
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
	require.NotNil(t, r.cliProcessFailures)
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
	r.EndQuerySpan(queryCtx, span, "query", "test-model", time.Second, nil)

	r.RecordTokenUsage(ctx, "query", "test-model", 100, 50)

	toolCtx, toolSpan := r.StartToolCallSpan(ctx, "test-tool")
	require.NotNil(t, toolSpan)
	r.EndToolCallSpan(toolCtx, toolSpan, "test-tool", "ok", time.Second)

	r.RecordCLIProcessFailure(ctx)
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

	r.EndQuerySpan(spanCtx, span, "query", "codex-model", 500*time.Millisecond, nil)

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

	r.EndQuerySpan(spanCtx, span, "query", "codex-model", time.Second, testErr)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "should have recorded error event")
}

func TestRecorder_QueryStreamSpan(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	spanCtx, span := r.StartQuerySpan(ctx, "query_stream", "test-model")
	r.EndQuerySpan(spanCtx, span, "query_stream", "test-model", time.Second, nil)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))
	require.NotEmpty(t, rm.ScopeMetrics)

	// Verify the operation.duration metric was recorded with query_stream attribute.
	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricOperationDuration {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric for query_stream", metricOperationDuration)
}

func TestRecorder_ToolCallSpan(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := NewRecorder(nil, tp)
	ctx := context.Background()

	toolCtx, span := r.StartToolCallSpan(ctx, "bash")
	r.EndToolCallSpan(toolCtx, span, "bash", "ok", 200*time.Millisecond)

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
	r.EndQuerySpan(spanCtx, span, "query", "test-model", time.Second, nil)

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

	r.RecordTokenUsage(ctx, "query", "test-model", 100, 50)

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
	r.EndToolCallSpan(toolCtx, span, "bash", "ok", 200*time.Millisecond)

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

func TestRecorder_ToolCallDenied(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	r.RecordToolCallDenied(ctx, "bash")

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricToolCallsTotal {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric with denied outcome", metricToolCallsTotal)
}

func TestRecorder_CLIProcessFailureMetric(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	defer func() { _ = mp.Shutdown(context.Background()) }()

	r := NewRecorder(mp, nil)
	ctx := context.Background()

	r.RecordCLIProcessFailure(ctx)

	var rm metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(ctx, &rm))

	found := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricCLIProcessFailures {
				found = true
			}
		}
	}

	require.True(t, found, "expected %s metric", metricCLIProcessFailures)
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
	r.EndQuerySpan(spanCtx, span, "query", "test-model", time.Second, nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	require.Equal(t, "test.event", spans[0].Events[0].Name)
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "nil", err: nil, expected: ""},
		{name: "context_cancelled", err: context.Canceled, expected: "cancelled"},
		{name: "context_deadline", err: context.DeadlineExceeded, expected: "timeout"},
		{name: "request_timeout", err: sdkerrors.ErrRequestTimeout, expected: "timeout"},
		{name: "session_not_found", err: sdkerrors.ErrSessionNotFound, expected: "session_not_found"},
		{
			name:     "cli_not_found",
			err:      &sdkerrors.CLINotFoundError{SearchedPaths: []string{"/usr/bin"}},
			expected: "cli_not_found",
		},
		{
			name:     "connection_error",
			err:      &sdkerrors.CLIConnectionError{Err: errors.New("conn failed")},
			expected: "connection_error",
		},
		{
			name:     "process_error",
			err:      &sdkerrors.ProcessError{ExitCode: 1, Err: errors.New("exit 1")},
			expected: "process_error",
		},
		{
			name:     "message_parse_error",
			err:      &sdkerrors.MessageParseError{Err: errors.New("bad json")},
			expected: "message_parse_error",
		},
		{
			name:     "json_decode_error",
			err:      &sdkerrors.CLIJSONDecodeError{Err: errors.New("invalid json")},
			expected: "json_decode_error",
		},
		{
			name:     "wrapped_timeout",
			err:      fmt.Errorf("wrapped: %w", sdkerrors.ErrRequestTimeout),
			expected: "timeout",
		},
		{name: "generic_error", err: errors.New("unknown"), expected: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, classifyError(tt.err))
		})
	}
}

func TestResolveMeterProvider(t *testing.T) {
	t.Parallel()

	t.Run("returns_mp_when_set", func(t *testing.T) {
		t.Parallel()

		reader := metric.NewManualReader()
		mp := metric.NewMeterProvider(metric.WithReader(reader))

		defer func() { _ = mp.Shutdown(context.Background()) }()

		result := ResolveMeterProvider(mp, nil)
		require.Equal(t, mp, result)
	})

	t.Run("returns_nil_when_both_nil", func(t *testing.T) {
		t.Parallel()

		result := ResolveMeterProvider(nil, nil)
		require.Nil(t, result)
	})

	t.Run("creates_provider_from_registerer", func(t *testing.T) {
		t.Parallel()

		reg := prometheus.NewRegistry()
		result := ResolveMeterProvider(nil, reg)
		require.NotNil(t, result, "should create MeterProvider from prometheus registerer")
	})

	t.Run("mp_takes_precedence_over_registerer", func(t *testing.T) {
		t.Parallel()

		reader := metric.NewManualReader()
		mp := metric.NewMeterProvider(metric.WithReader(reader))

		defer func() { _ = mp.Shutdown(context.Background()) }()

		reg := prometheus.NewRegistry()
		result := ResolveMeterProvider(mp, reg)
		require.Equal(t, mp, result, "explicit MeterProvider should take precedence")
	})
}
