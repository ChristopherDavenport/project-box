package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// writeReviewCfg writes a box.yaml with one repo whose `review` (and `lane`,
// when non-empty) are exactly what the test passes — the two knobs
// [Daemon.assemble] reads to decide whether the gate set gets a reviewer
// appended after the command gate.
func writeReviewCfg(t *testing.T, path, db, root, remote, lane, review string) {
	t.Helper()
	var extra string
	if lane != "" {
		extra += "\n        lane: " + lane
	}
	if review != "" {
		extra += "\n        review: " + review
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    agent: ['true']
    repos:
      - name: app
        remote: %s
        check: 'true'%s
`, db, root, remote, extra)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAssembleAppendsTheReviewerWhenDeclared: a repo declaring
// `review: judgement` gets a gate set whose second gate is the reviewer
// after the command gate, and a repo that says nothing about review gets
// exactly today's single command gate.
func TestAssembleAppendsTheReviewerWhenDeclared(t *testing.T) {
	ctx := context.Background()

	t.Run("review: judgement", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeReviewCfg(t, cfgPath, db, root, bare(t, "app"), "gated", "judgement")

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		gates := lp.Gates(forge.Workspace{Dir: dir})
		if len(gates) != 2 {
			t.Fatalf("gate set = %d gates, want 2 (command, then reviewer): %+v", len(gates), gates)
		}
		if gates[0].Name() != "check" {
			t.Fatalf("first gate = %q, want the command gate first", gates[0].Name())
		}
		if gates[1].Name() != acp.ReviewerName {
			t.Fatalf("second gate = %q, want %q", gates[1].Name(), acp.ReviewerName)
		}
	})

	t.Run("unconfigured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeReviewCfg(t, cfgPath, db, root, bare(t, "app"), "", "")

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		gates := lp.Gates(forge.Workspace{Dir: dir})
		if len(gates) != 1 {
			t.Fatalf("gate set = %d gates, want 1 (today's command gate alone): %+v", len(gates), gates)
		}
		if gates[0].Name() != "check" {
			t.Fatalf("gate = %q, want the command gate", gates[0].Name())
		}
	})
}
