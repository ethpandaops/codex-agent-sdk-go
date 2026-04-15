// Package observability provides internal OTel metric and trace recording
// for the Codex Agent SDK. All instruments are created eagerly during
// construction; when providers are nil the OTel API returns noop
// implementations, making recording zero-cost.
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

const (
	// instrumentationName is the OTel instrumentation scope name.
	instrumentationName = "github.com/ethpandaops/codex-agent-sdk-go"
	// instrumentationVersion is the OTel instrumentation scope version.
	instrumentationVersion = "0.1.0"
)

// GenAI semantic convention metric names.
const (
	metricOperationDuration = "gen_ai.client.operation.duration"
	metricTokenUsage        = "gen_ai.client.token.usage" //nolint:gosec // G101: not a credential, OTel metric name
)

// Codex-specific metric names.
const (
	metricToolCallsTotal        = "codex.tool_calls_total"
	metricToolCallDuration      = "codex.tool_call_duration_seconds"
	metricCLIProcessRestarts    = "codex.cli_process_restarts_total"
	metricCLIMessageParseErrors = "codex.cli_message_parse_errors_total"
)

// GenAI semantic convention attribute keys.
var (
	attrOperationName = attribute.Key("gen_ai.operation.name")
	attrRequestModel  = attribute.Key("gen_ai.request.model")
	attrTokenType     = attribute.Key("gen_ai.token.type")
	attrErrorType     = attribute.Key("error.type")
	attrToolName      = attribute.Key("codex.tool.name")
	attrToolOutcome   = attribute.Key("codex.tool.outcome")
)

// Recorder holds pre-created OTel instruments for SDK-wide recording.
// All fields are safe for concurrent use. When created with noop providers,
// all recording methods are zero-cost.
type Recorder struct {
	tracer trace.Tracer

	// GenAI semconv metrics.
	operationDuration metric.Float64Histogram
	tokenUsage        metric.Int64Counter

	// Codex-specific metrics.
	toolCallsTotal        metric.Int64Counter
	toolCallDuration      metric.Float64Histogram
	cliProcessRestarts    metric.Int64Counter
	cliMessageParseErrors metric.Int64Counter
}

// NewRecorder creates instruments from the provided providers.
// If providers are nil, OTel API returns noop instruments automatically.
func NewRecorder(mp metric.MeterProvider, tp trace.TracerProvider) *Recorder {
	if mp == nil {
		mp = noopmetric.NewMeterProvider()
	}

	if tp == nil {
		tp = nooptrace.NewTracerProvider()
	}

	meter := mp.Meter(
		instrumentationName,
		metric.WithInstrumentationVersion(instrumentationVersion),
	)
	tracer := tp.Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion(instrumentationVersion),
	)

	operationDuration, _ := meter.Float64Histogram(
		metricOperationDuration,
		metric.WithDescription("Duration of GenAI client operations"),
		metric.WithUnit("s"),
	)

	tokenUsage, _ := meter.Int64Counter(
		metricTokenUsage,
		metric.WithDescription("Number of tokens used by GenAI operations"),
		metric.WithUnit("{token}"),
	)

	toolCallsTotal, _ := meter.Int64Counter(
		metricToolCallsTotal,
		metric.WithDescription("Total number of tool calls"),
		metric.WithUnit("{call}"),
	)

	toolCallDuration, _ := meter.Float64Histogram(
		metricToolCallDuration,
		metric.WithDescription("Duration of tool call executions"),
		metric.WithUnit("s"),
	)

	cliProcessRestarts, _ := meter.Int64Counter(
		metricCLIProcessRestarts,
		metric.WithDescription("Total number of CLI process restarts"),
		metric.WithUnit("{restart}"),
	)

	cliMessageParseErrors, _ := meter.Int64Counter(
		metricCLIMessageParseErrors,
		metric.WithDescription("Total number of CLI message parse errors"),
		metric.WithUnit("{error}"),
	)

	return &Recorder{
		tracer:                tracer,
		operationDuration:     operationDuration,
		tokenUsage:            tokenUsage,
		toolCallsTotal:        toolCallsTotal,
		toolCallDuration:      toolCallDuration,
		cliProcessRestarts:    cliProcessRestarts,
		cliMessageParseErrors: cliMessageParseErrors,
	}
}

// NopRecorder returns a recorder backed by noop providers.
func NopRecorder() *Recorder {
	return NewRecorder(nil, nil)
}

// StartQuerySpan starts a new span for a query operation and returns the
// enriched context and span. The caller must call span.End() when done.
func (r *Recorder) StartQuerySpan(
	ctx context.Context,
	operationName string,
	model string,
) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attrOperationName.String(operationName),
	}

	if model != "" {
		attrs = append(attrs, attrRequestModel.String(model))
	}

	return r.tracer.Start(ctx, "codex.query",
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

// EndQuerySpan records the query duration metric and ends the span.
func (r *Recorder) EndQuerySpan(
	ctx context.Context,
	span trace.Span,
	model string,
	duration time.Duration,
	err error,
) {
	attrs := []attribute.KeyValue{
		attrOperationName.String("query"),
	}

	if model != "" {
		attrs = append(attrs, attrRequestModel.String(model))
	}

	if err != nil {
		attrs = append(attrs, attrErrorType.String(classifyError(err)))
		span.RecordError(err)
	}

	r.operationDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attrs...),
	)

	span.End()
}

// RecordTokenUsage records input and output token counts.
func (r *Recorder) RecordTokenUsage(
	ctx context.Context,
	model string,
	inputTokens int64,
	outputTokens int64,
) {
	baseAttrs := []attribute.KeyValue{
		attrOperationName.String("query"),
	}

	if model != "" {
		baseAttrs = append(baseAttrs, attrRequestModel.String(model))
	}

	if inputTokens > 0 {
		inputAttrs := append(
			append([]attribute.KeyValue{}, baseAttrs...),
			attrTokenType.String("input"),
		)
		r.tokenUsage.Add(ctx, inputTokens,
			metric.WithAttributes(inputAttrs...),
		)
	}

	if outputTokens > 0 {
		outputAttrs := append(
			append([]attribute.KeyValue{}, baseAttrs...),
			attrTokenType.String("output"),
		)
		r.tokenUsage.Add(ctx, outputTokens,
			metric.WithAttributes(outputAttrs...),
		)
	}
}

// StartToolCallSpan starts a child span for a tool call.
func (r *Recorder) StartToolCallSpan(
	ctx context.Context,
	toolName string,
) (context.Context, trace.Span) {
	return r.tracer.Start(ctx, "codex.tool_call",
		trace.WithAttributes(attrToolName.String(toolName)),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}

// EndToolCallSpan records tool call metrics and ends the span.
func (r *Recorder) EndToolCallSpan(
	ctx context.Context,
	span trace.Span,
	toolName string,
	outcome string,
	duration time.Duration,
) {
	attrs := []attribute.KeyValue{
		attrToolName.String(toolName),
		attrToolOutcome.String(outcome),
	}

	r.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	r.toolCallDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attrs...),
	)

	span.SetAttributes(attrToolOutcome.String(outcome))
	span.End()
}

// RecordCLIProcessRestart increments the CLI process restart counter.
func (r *Recorder) RecordCLIProcessRestart(ctx context.Context) {
	r.cliProcessRestarts.Add(ctx, 1)
}

// RecordMessageParseError increments the message parse error counter.
func (r *Recorder) RecordMessageParseError(ctx context.Context) {
	r.cliMessageParseErrors.Add(ctx, 1)
}

// AddSpanEvent adds an event to an active span.
func (r *Recorder) AddSpanEvent(
	span trace.Span,
	name string,
	attrs ...attribute.KeyValue,
) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// classifyError returns a short, cardinality-safe error classification.
func classifyError(err error) string {
	if err == nil {
		return ""
	}

	return "error"
}
