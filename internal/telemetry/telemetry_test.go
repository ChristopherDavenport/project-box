package telemetry

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

func recorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

func item() queue.WorkItem {
	return queue.WorkItem{
		ID: "wi-1", Tenant: tenant.ID("acme"), Box: "shop", Ingress: "ing-1",
		Task: "rotate the credentials in config.yaml using s3cr3t-value", // must never appear on a span
	}
}

func TestStagesAreSeparateTracesWithClosedAttributes(t *testing.T) {
	sr := recorder(t)
	ctx := context.Background()
	for _, stage := range []string{"build", "gate", "land"} {
		if err := Stage(ctx, stage, item(), func(ctx context.Context) error {
			Outcome(ctx, queue.StateDone, "abc123")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	spans := sr.Ended()
	if len(spans) != 3 {
		t.Fatalf("want 3 spans, got %d", len(spans))
	}
	// Each stage is its own trace — hours-long traces and tail sampling are
	// incompatible, so no two stages may share a trace id.
	seen := map[string]bool{}
	for _, sp := range spans {
		id := sp.SpanContext().TraceID().String()
		if seen[id] {
			t.Fatal("two stages share a trace id")
		}
		seen[id] = true
		attrs := map[string]string{}
		for _, a := range sp.Attributes() {
			attrs[string(a.Key)] = a.Value.Emit()
		}
		if attrs["workitem.id"] != "wi-1" || attrs["box"] != "shop" {
			t.Fatalf("correlation attributes missing: %v", attrs)
		}
		// The closed set: the task text (and the credential inside it) must
		// not appear in ANY attribute value.
		for k, v := range attrs {
			if len(v) > 0 && (contains(v, "s3cr3t") || contains(v, "rotate the credentials")) {
				t.Fatalf("task text leaked into span attribute %s=%q", k, v)
			}
		}
	}
}

func TestFailedStageIsMarkedNotDescribed(t *testing.T) {
	sr := recorder(t)
	sentinel := errors.New("command output: password=hunter2 leaked to stderr")
	err := Stage(context.Background(), "build", item(), func(context.Context) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal("stage must return the underlying error to the caller")
	}
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans: %d", len(spans))
	}
	sp := spans[0]
	if sp.Status().Description == sentinel.Error() {
		t.Fatal("the error text was copied onto the span — failure is marked, never described")
	}
	failed := false
	for _, a := range sp.Attributes() {
		if string(a.Key) == "failed" && a.Value.AsBool() {
			failed = true
		}
		if contains(a.Value.Emit(), "hunter2") {
			t.Fatalf("error content leaked into attribute %s", a.Key)
		}
	}
	if !failed || sp.Status().Code.String() != "Error" {
		t.Fatalf("failure not marked: failed=%v status=%v", failed, sp.Status())
	}
}

func TestToolCallsAreEventsWithoutArguments(t *testing.T) {
	sr := recorder(t)
	Stage(context.Background(), "build", item(), func(ctx context.Context) error {
		ToolCall(ctx, "Bash", true)
		ToolCall(ctx, "DeleteProd", false)
		return nil
	})
	sp := sr.Ended()[0]
	if len(sp.Events()) != 2 {
		t.Fatalf("events: %d", len(sp.Events()))
	}
	for _, ev := range sp.Events() {
		if ev.Name != "tool_call" {
			t.Fatalf("event: %s", ev.Name)
		}
		for _, a := range ev.Attributes {
			if string(a.Key) != "tool" && string(a.Key) != "permitted" {
				t.Fatalf("tool_call carries more than name+permitted: %s", a.Key)
			}
		}
	}
}

func TestTraceIDEmptyWhenOff(t *testing.T) {
	if id := TraceID(context.Background()); id != "" {
		t.Fatalf("no telemetry, but TraceID=%q — evidence must tolerate empty", id)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
