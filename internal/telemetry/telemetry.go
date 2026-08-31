// Package telemetry emits the spans that make the factory operable, and
// nothing that would make it leak.
//
// Two architecture rules are enforced here rather than remembered:
//
// Telemetry is not evidence. Spans are sampled and expire; evidence is
// complete and retained. The two share correlation ids and nothing else — a
// span carries the workitem id and the evidence record carries the trace id,
// and the join is an id, never a shared pipeline.
//
// Each stage is its own trace. A WorkItem can idle for hours between stages,
// and a tail sampler decides a trace's fate after it stops seeing spans —
// hours-long traces and tail sampling are incompatible. So every stage starts
// a fresh trace and reassembly happens at query time on workitem.id.
//
// Attributes are a closed set. Task text, prompts, tool arguments, and agent
// output NEVER become attributes: under a Confidentiality-scoped report the
// redaction pipeline is an audited control, and the strongest redaction is
// the value that was never emitted. The collector's redaction processor is
// the second line, not the first.
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Setup installs a tracer provider exporting OTLP/gRPC to endpoint
// ("host:port", the collector gateway) and returns its shutdown. An empty
// endpoint installs nothing and every span becomes a no-op — telemetry may
// be off; evidence may not, and nothing in this package is load-bearing for
// correctness.
func Setup(ctx context.Context, endpoint, service string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // the collector gateway is local/cluster-internal; TLS is its far side
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: exporter: %w", err)
	}
	res, err := sdkresource.New(ctx, sdkresource.WithAttributes(
		semconv.ServiceName(service),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns the process tracer. With no Setup it is a no-op.
func Tracer() trace.Tracer { return otel.Tracer("project-box") }

// Stage runs one stage of a work item under its own root span — its own
// trace, per the rule above. The runner emits the span, so a stage cannot be
// added without telemetry; it is part of how work executes.
//
// The attribute set is closed on purpose: identifiers and outcomes only.
// Adding an attribute here is a reviewable act; interpolating one at a call
// site is impossible, which is the point.
func Stage(ctx context.Context, stage string, item queue.WorkItem, fn func(context.Context) error) error {
	ctx, span := Tracer().Start(ctx, "stage."+stage,
		trace.WithNewRoot(), // each stage is its own trace
		trace.WithAttributes(
			attribute.String("workitem.id", item.ID),
			attribute.String("tenant", string(item.Tenant)),
			attribute.String("box", item.Box),
			attribute.String("ingress.id", item.Ingress),
			attribute.String("stage", stage),
		),
	)
	defer span.End()
	err := fn(ctx)
	if err != nil {
		// The failure is marked, never described: error text can carry
		// anything the failing command printed, and that is for evidence
		// and the exception queue, not for a third-party telemetry sink.
		span.SetStatus(codes.Error, "stage failed")
		span.SetAttributes(attribute.Bool("failed", true))
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// Outcome annotates the current span with a work item's settled state and,
// when the stage produced one, the commit — the identifier the evidence
// chain is keyed by, and therefore the correlation the operator pivots on.
func Outcome(ctx context.Context, state queue.State, head string) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("outcome", string(state)))
	if head != "" {
		span.SetAttributes(attribute.String("commit", head))
	}
}

// ToolCall records an agent tool call as an event on the current span: the
// tool's name and whether policy permitted it, never its arguments.
func ToolCall(ctx context.Context, title string, permitted bool) {
	trace.SpanFromContext(ctx).AddEvent("tool_call", trace.WithAttributes(
		attribute.String("tool", title),
		attribute.Bool("permitted", permitted),
	))
}

// TraceID returns the current trace id for evidence correlation, empty when
// telemetry is off — and an empty value is normal, because telemetry may be
// disabled and evidence must not be.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
