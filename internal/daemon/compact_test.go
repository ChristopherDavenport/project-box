package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// writeRetentionCfg is writeCfg with a retention block.
func writeRetentionCfg(t *testing.T, path, db, root, retention, remote string) {
	t.Helper()
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {max_concurrent: 2}
%s
boxes:
  - name: shop
    repos:
      - name: api
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
`, db, root, retention, remote, buildCmd)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTheCompactionChoreRunsAndTheChainSurvivesIt. The schema half of
// compaction has been in the store since Phase 0 — the reserved kind, the
// summary at the pruned head's seat, Verify across the seam, both backends,
// the conformance suite. Nothing called it, so the hot store grew forever
// and the safe prune existed only in tests.
//
// This is the chore, and the property it has to hold is the one the issue
// opens with: Gas Town could flatten its chain because it was honest by
// norm, and ours is tamper-evident by construction, so a prune is
// indistinguishable from the attack Verify exists to catch unless the seam
// proves out afterwards.
func TestTheCompactionChoreRunsAndTheChainSurvivesIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")

	writeRetentionCfg(t, cfgPath, db, root,
		"retention: {keep: 20, keep_for: 1ns, min_prune: 10}", bare(t, "api"))
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	// Fill a standing scope past its retention. control/capacity is the one
	// a real factory fills fastest — a charge and a spend per run.
	const n = 200
	for i := range n {
		if _, err := d.store.Log().Append(ctx, evidence.Record{
			Tenant: d.tenant, Scope: "control/capacity", Kind: "charge",
			Subject: fmt.Sprintf("run-%d", i), Actor: "boxd",
			Body: []byte(`{"tokens":1000,"metered":true}`),
		}); err != nil {
			t.Fatal(err)
		}
	}

	sums, err := d.compactor.Sweep(ctx, d.tenant, StandingScopes...)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if len(sums) != 1 {
		t.Fatalf("compacted %d scopes, want 1 (only control/capacity is over)", len(sums))
	}

	// The hot side verifies across the seam, and is the size the policy asked
	// for: the newest 20 plus the summary standing for the other 180.
	if err := d.store.Log().Verify(ctx, tenant.ID("acme"), "control/capacity"); err != nil {
		t.Fatalf("the compacted scope does not verify: %v", err)
	}
	var hot int
	for _, err := range d.store.Log().Since(ctx, d.tenant, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		hot++
	}
	if hot != 21 {
		t.Fatalf("hot store holds %d records, want 21", hot)
	}

	// The pruned side is durable and provable, offline, from the object under
	// the daemon's own root — which is the half that makes the prune a prune
	// rather than a deletion.
	cmp, err := evidence.ParseCompaction(sums[0])
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Records != 180 {
		t.Fatalf("the summary stands for %d records, want 180", cmp.Records)
	}
	sink := &evidence.DirSink{Dir: filepath.Join(root, "worm"), Log: d.store.Log()}
	exported, err := sink.Load(cmp.Export)
	if err != nil {
		t.Fatalf("the exported object is not where the receipt says: %v", err)
	}
	if err := evidence.VerifySeam(sums[0], exported); err != nil {
		t.Fatalf("the seam does not prove against its export: %v", err)
	}

	// A second sweep with nothing new to do is a no-op and not an error: the
	// chore runs on a timer, so "already compacted" must not read as a
	// failure every twenty ticks.
	if again, err := d.compactor.Sweep(ctx, d.tenant, StandingScopes...); err != nil || len(again) != 0 {
		t.Fatalf("an idle sweep: %d summaries, err %v", len(again), err)
	}
}

// TestAnUnconfiguredDaemonCompactsNothing. The destructive act must not be
// the one a silent default takes. A daemon whose box.yaml says nothing about
// retention keeps every record, and the sink directory is never created —
// which is also how an operator can tell at a glance whether it is on.
func TestAnUnconfiguredDaemonCompactsNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")

	writeRetentionCfg(t, cfgPath, db, root, "", bare(t, "api"))
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	for i := range 200 {
		if _, err := d.store.Log().Append(ctx, evidence.Record{
			Tenant: d.tenant, Scope: "control/capacity", Kind: "charge",
			Subject: fmt.Sprintf("run-%d", i), Actor: "boxd", Body: []byte(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	sums, err := d.compactor.Sweep(ctx, d.tenant, StandingScopes...)
	if err != nil || len(sums) != 0 {
		t.Fatalf("an unconfigured daemon pruned: %d summaries, err %v", len(sums), err)
	}
	if _, err := os.Stat(filepath.Join(root, "worm")); !os.IsNotExist(err) {
		t.Fatal("an unconfigured daemon wrote to the sink")
	}
}

// TestHalfARetentionPolicyIsRefusedAtLoad. A count with no age, or an age
// with no count, compacts nothing at all — so an operator who configured a
// retention policy would get none and have no way to know. The config
// already refuses a repo with neither an agent nor a build command for the
// same reason: silence about a thing that will never happen is the failure.
func TestHalfARetentionPolicyIsRefusedAtLoad(t *testing.T) {
	for _, half := range []string{
		"retention: {keep: 20}",
		"retention: {keep_for: 72h}",
	} {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		writeRetentionCfg(t, cfgPath, filepath.Join(dir, "box.db"),
			filepath.Join(dir, "forge"), half, bare(t, "api"))
		_, err := New(cfgPath)
		if err == nil || !strings.Contains(err.Error(), "BOTH keep and keep_for") {
			t.Fatalf("%q was accepted: %v", half, err)
		}
	}
	// And an unparseable age is refused rather than silently disabling it.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	writeRetentionCfg(t, cfgPath, filepath.Join(dir, "box.db"),
		filepath.Join(dir, "forge"), "retention: {keep: 20, keep_for: 72 hours}", bare(t, "api"))
	if _, err := New(cfgPath); err == nil || !strings.Contains(err.Error(), "keep_for") {
		t.Fatalf("an unparseable keep_for was accepted: %v", err)
	}
	_ = time.Second
}
