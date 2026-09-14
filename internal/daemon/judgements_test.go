package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeJudgementCfg writes a minimal box.yaml with the given raw budget body,
// so a test can drive what assemble threads into the meter's budget without
// touching writeCfg's fixed `{max_concurrent: 2}`, which other tests share.
func writeJudgementCfg(t *testing.T, path, db, root, remote, budget string) {
	t.Helper()
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {%s}
boxes:
  - name: shop
    repos:
      - name: app
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
`, db, root, budget, remote, buildCmd)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAssembleThreadsMaxJudgementsIntoTheMeter: the configured judgement seat
// pool reaches the meter's budget, and an absent key floors at one rather
// than reading as unbounded.
func TestAssembleThreadsMaxJudgementsIntoTheMeter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	rem := bare(t, "app")

	writeJudgementCfg(t, cfgPath, db, root, rem, "max_concurrent: 2, max_judgements: 3")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if got := d.meter.Report().MaxJudgements; got != 3 {
		t.Fatalf("max judgements = %d, want 3", got)
	}

	// A reconfigured budget over the same daemon: reload swaps the meter's
	// budget in place, not just at construction.
	writeJudgementCfg(t, cfgPath, db, root, rem, "max_concurrent: 2, max_judgements: 5")
	if _, err := d.Reload(ctx, "chris"); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := d.meter.Report().MaxJudgements; got != 5 {
		t.Fatalf("max judgements after reload = %d, want 5", got)
	}

	// Unset means one — the floor issue #202 restores, not an unbounded
	// pool.
	dir2 := t.TempDir()
	cfgPath2 := filepath.Join(dir2, "box.yaml")
	db2, root2 := filepath.Join(dir2, "box.db"), filepath.Join(dir2, "forge")
	rem2 := bare(t, "app2")
	writeJudgementCfg(t, cfgPath2, db2, root2, rem2, "max_concurrent: 2")
	d2, err := New(cfgPath2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d2.Close() })
	if got := d2.meter.Report().MaxJudgements; got != 1 {
		t.Fatalf("max judgements with the key unset = %d, want 1", got)
	}
}
