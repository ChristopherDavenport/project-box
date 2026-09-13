package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
)

func writeAgentCfg(t *testing.T, dir, runs string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "box.yaml")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
%s
boxes:
  - name: shop
    agent: ['true']
    kinds:
      - name: issue
        decompose: {decomposer: planner}
        completion: {graph: true}
    repos:
      - name: app
        remote: %s
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), runs, bare(t, "app"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestARunIsSeenLiveAndKeptAfter: one hook, for builders and the planner
// alike, lands each frame on the live bus the room watches and in the run log
// the room reads back — stamped with one moment, so the frame seen live and
// the frame read later are the same frame. Before this the planner had no
// hook at all and a builder's frames lived only in a ring that died with the
// daemon.
func TestARunIsSeenLiveAndKeptAfter(t *testing.T) {
	d, err := New(writeAgentCfg(t, t.TempDir(), "runs: {keep_for: 24h}"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, ok := d.planners.For("shop")
	if !ok || p.Progress == nil {
		t.Fatal("the planner runs with no progress hook; a judgement would be invisible while it ran")
	}
	for name, lp := range d.pump.Loops {
		if b, ok := lp.Builder.(*acp.Builder); !ok || b.Progress == nil {
			t.Fatalf("loop %s: the builder runs with no progress hook", name)
		}
	}
	ctx := context.Background()
	d.progress(ctx, "wi-x", acp.Update{Kind: acp.UpdateTool, Call: "t-1", Tool: "Read", Text: "Read main.go", Session: "agentref-1"})
	live := d.live.Item("wi-x")
	kept, err := d.store.Runs().Frames(ctx, d.tenant, "wi-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || len(kept) != 1 {
		t.Fatalf("live %d, kept %d; want one frame on each", len(live), len(kept))
	}
	if live[0].Call != "t-1" || kept[0].Call != "t-1" || kept[0].Text != "Read main.go" || kept[0].Kind != "tool" {
		t.Errorf("frames = live %+v, kept %+v; want the call id, tool, title and kind on both", live[0], kept[0])
	}
	if live[0].Session != "agentref-1" || kept[0].Session != "agentref-1" {
		t.Errorf("frames = live %+v, kept %+v; want the session on both", live[0], kept[0])
	}
	if !live[0].At.Equal(kept[0].At) {
		t.Errorf("the same frame carries two moments: live %v, kept %v", live[0].At, kept[0].At)
	}
	if got := d.runsKeepFor(); got != 24*time.Hour {
		t.Errorf("runs.keep_for in force = %v, want 24h", got)
	}
}

// TestTheRunLogKeepsEverythingUnlessTold: no policy is no pruning, on the
// same reasoning as evidence retention, and a policy that cannot be read is
// refused at load rather than read as "never" or as "now".
func TestTheRunLogKeepsEverythingUnlessTold(t *testing.T) {
	d, err := New(writeAgentCfg(t, t.TempDir(), ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.runsKeepFor(); got != 0 {
		t.Errorf("with no runs block, keep_for = %v, want 0 (keep everything)", got)
	}
	d.Close()
	for _, bad := range []string{"runs: {keep_for: soon}", "runs: {keep_for: -1h}"} {
		_, err := New(writeAgentCfg(t, t.TempDir(), bad))
		if err == nil || !strings.Contains(err.Error(), "runs.keep_for") {
			t.Errorf("%s: not refused at load: %v", bad, err)
		}
	}
}
