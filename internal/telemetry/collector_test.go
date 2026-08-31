package telemetry

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const collectorImage = "otel/opentelemetry-collector-contrib:0.159.0"

// TestCollectorRedactsAndKeepsFailures runs the REAL collector with the
// committed gateway config and asserts the two properties the architecture
// makes audited controls: a planted credential does not reach the exporter,
// and a failed span survives sampling that drops the routine.
//
// Docker-gated the same way Spanner conformance is emulator-gated: skip
// locally without docker, FAIL in CI — a skipped control test reading as a
// pass is the exact failure mode the plan forbids.
func TestCollectorRedactsAndKeepsFailures(t *testing.T) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("docker required in CI for the collector control test: %v", err)
		}
		t.Skip("docker unavailable; the collector redaction test needs it")
	}
	ctx := context.Background()

	cfg, err := filepath.Abs("../../deploy/collector/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	os.Chmod(out, 0o777) // the collector runs unprivileged in its image

	const port = "24317"
	run := exec.Command("docker", "run", "-d", "--rm",
		"-p", port+":4317",
		"-v", cfg+":/etc/otelcol/config.yaml:ro",
		"-v", out+":/out",
		collectorImage, "--config=/etc/otelcol/config.yaml")
	id, err := run.Output()
	if err != nil {
		t.Fatalf("start collector: %v", err)
	}
	cid := strings.TrimSpace(string(id))
	t.Cleanup(func() { exec.Command("docker", "stop", cid).Run() })

	// Wait for the OTLP port.
	for i := 0; ; i++ {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
			c.Close()
			break
		}
		if i > 30 {
			t.Fatal("collector never opened its OTLP port")
		}
		time.Sleep(time.Second)
	}

	shutdown, err := Setup(ctx, "127.0.0.1:"+port, "collector-test")
	if err != nil {
		t.Fatal(err)
	}
	tr := otel.Tracer("collector-test")

	// A failed span deliberately carrying a planted credential — simulating
	// exactly the leak the second line of defence exists for.
	const planted = "AKIA1234567890ABCDEF"
	_, span := tr.Start(ctx, "stage.exploded")
	span.SetAttributes(attribute.String("oops.debug", "connecting with "+planted+" failed"))
	span.SetStatus(codes.Error, "boom")
	span.End()

	// A crowd of routine spans for the sampler to thin.
	const routine = 40
	for i := range routine {
		_, sp := tr.Start(ctx, fmt.Sprintf("stage.routine-%02d", i))
		sp.SetStatus(codes.Ok, "")
		sp.End()
	}
	if err := shutdown(ctx); err != nil { // flushes the batcher
		t.Fatalf("flush: %v", err)
	}

	// The tail sampler decides after decision_wait; poll the exporter file.
	var content string
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(out, "spans.json"))
		content = string(b)
		if strings.Contains(content, "stage.exploded") {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Property 1: the failure reached the exporter — a sampler must never be
	// the reason an incident cannot be reconstructed.
	if !strings.Contains(content, "stage.exploded") {
		t.Fatal("the failed span did not survive the pipeline")
	}
	// Property 2: the planted credential did not.
	if strings.Contains(content, planted) {
		t.Fatal("the planted credential reached the exporter unredacted")
	}
	// And the sampler is actually thinning: strictly fewer routine spans out
	// than in (at 10%, all 40 surviving has probability ~1e-40).
	kept := strings.Count(content, "stage.routine-")
	if kept >= routine {
		t.Fatalf("sampler kept all %d routine spans", kept)
	}
	t.Logf("kept %d/%d routine spans; failure kept; credential masked", kept, routine)
}
