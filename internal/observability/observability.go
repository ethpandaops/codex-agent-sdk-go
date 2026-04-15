// Package observability provides internal OTel metric and trace recording
// for the Codex Agent SDK. All instruments are created eagerly during
// construction; when providers are nil the OTel API returns noop
// implementations, making recording zero-cost.
package observability

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	sdkerrors "github.com/ethpandaops/codex-agent-sdk-go/internal/errors"
	"github.com/ethpandaops/codex-agent-sdk-go/internal/version"
)

const (
	// instrumentationName is the OTel instrumentation scope name.
	instrumentationName = "github.com/ethpandaops/codex-agent-sdk-go"
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
	metricCLIProcessFailures    = "codex.cli_process_failures_total"
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
	cliProcessFailures    metric.Int64Counter
	cliMessageParseErrors metric.Int64Counter
}

// ResolveMeterProvider returns mp if non-nil. Otherwise, if reg is non-nil,
// it creates a MeterProvider backed by the Prometheus registerer via the
// OTel→Prometheus bridge. Returns nil when both are nil, which causes
// NewRecorder to fall back to noop instruments.
func ResolveMeterProvider(mp metric.MeterProvider, reg prometheus.Registerer) metric.MeterProvider {
	if mp != nil {
		return mp
	}

	if reg == nil {
		return nil
	}

	exporter, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		slog.Warn("failed to create prometheus exporter, falling back to noop metrics", "error", err)

		return nil
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
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
		metric.WithInstrumentationVersion(version.Version),
	)
	tracer := tp.Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion(version.Version),
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

	cliProcessFailures, _ := meter.Int64Counter(
		metricCLIProcessFailures,
		metric.WithDescription("Total number of CLI process failures"),
		metric.WithUnit("{failure}"),
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
		cliProcessFailures:    cliProcessFailures,
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
// The operationName must match the value passed to StartQuerySpan
// (e.g. "query" or "query_stream") so that metrics are correctly attributed.
func (r *Recorder) EndQuerySpan(
	ctx context.Context,
	span trace.Span,
	operationName string,
	model string,
	duration time.Duration,
	err error,
) {
	attrs := []attribute.KeyValue{
		attrOperationName.String(operationName),
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
// The operationName identifies the operation that produced the tokens
// (e.g. "query" or "query_stream").
func (r *Recorder) RecordTokenUsage(
	ctx context.Context,
	operationName string,
	model string,
	inputTokens int64,
	outputTokens int64,
) {
	baseAttrs := []attribute.KeyValue{
		attrOperationName.String(operationName),
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

// RecordToolCallDenied increments the tool calls counter with outcome "denied".
// This is recorded when the permission callback denies a tool invocation.
func (r *Recorder) RecordToolCallDenied(ctx context.Context, toolName string) {
	attrs := []attribute.KeyValue{
		attrToolName.String(toolName),
		attrToolOutcome.String("denied"),
	}

	r.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordCLIProcessFailure increments the CLI process failure counter.
// This is recorded when the CLI process exits with a non-zero exit code,
// regardless of whether a restart occurs.
func (r *Recorder) RecordCLIProcessFailure(ctx context.Context) {
	r.cliProcessFailures.Add(ctx, 1)
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

// classifyError returns a short, cardinality-safe error classification
// string suitable for use as the error.type metric attribute.
func classifyError(err error) string {
	if err == nil {
		return ""
	}

	// Check context errors first (most common).
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	// Check SDK sentinel errors.
	if errors.Is(err, sdkerrors.ErrRequestTimeout) {
		return "timeout"
	}

	if errors.Is(err, sdkerrors.ErrSessionNotFound) {
		return "session_not_found"
	}

	// Check SDK typed errors.
	if _, ok := errors.AsType[*sdkerrors.CLINotFoundError](err); ok {
		return "cli_not_found"
	}

	if _, ok := errors.AsType[*sdkerrors.CLIConnectionError](err); ok {
		return "connection_error"
	}

	if _, ok := errors.AsType[*sdkerrors.ProcessError](err); ok {
		return "process_error"
	}

	if _, ok := errors.AsType[*sdkerrors.MessageParseError](err); ok {
		return "message_parse_error"
	}

	if _, ok := errors.AsType[*sdkerrors.CLIJSONDecodeError](err); ok {
		return "json_decode_error"
	}

	return "error"
}
