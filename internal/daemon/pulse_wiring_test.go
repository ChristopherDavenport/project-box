package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestAssembleGivesEveryLoopTheDaemonsOneRegistry is issue #211's own
// property at the assemble seam: the daemon owns exactly one [loop.Heartbeat],
// constructed once in New beside d.suspend, and every assembled Loop — on
// every assemble, including a reload — is handed that same registry rather
// than a fresh one. A fresh registry per reload would silently drop a
// mid-run's renewal state the moment a config file changed.
func TestAssembleGivesEveryLoopTheDaemonsOneRegistry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	writeCfg(t, cfgPath, db, root, map[string]string{"work": workRem})

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.pulse == nil {
		t.Fatal("New did not construct a registry on the daemon")
	}

	loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := loops["shop/work"]
	if !ok {
		t.Fatal("shop/work: no loop assembled")
	}
	if lp.Pulse != d.pulse {
		t.Fatalf("assembled loop's Pulse = %p, want the daemon's own registry %p", lp.Pulse, d.pulse)
	}

	// A second assemble — standing in for a reload — must read the same
	// registry off d rather than constructing a fresh one.
	loops2, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if loops2["shop/work"].Pulse != d.pulse {
		t.Fatal("a second assemble handed the loop a different registry than the first")
	}
}

// TestProgressPulseOnAnUnregisteredRunChangesNothing is the planner-run and
// no-such-run case: d.progress must still stamp the live bus and the run log
// exactly as it always has, and the pulse into d.pulse for a run nobody
// Tracked must be a silent no-op rather than an error or a panic.
func TestProgressPulseOnAnUnregisteredRunChangesNothing(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)

	d.progress(ctx, "wi-untracked", acp.Update{Kind: acp.UpdateTool, Call: "c-1", Tool: "Read", Text: "read a file"})

	live := d.live.Item("wi-untracked")
	kept, err := d.store.Runs().Frames(ctx, d.tenant, "wi-untracked")
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || len(kept) != 1 {
		t.Fatalf("live %d, kept %d; want one frame on each even though the run was never registered with the pulse registry", len(live), len(kept))
	}
}

// TestProgressPulseRenewsARegisteredRunsClaimLease is issue #211's own
// property at the frame seam: once a run's claim lease is registered with
// d.pulse — which [loop.Loop.RunHolding] does the moment it claims an item —
// a frame arriving through d.progress renews that lease, observable as a
// later expiry on the very same store row, well before the build itself
// finishes.
func TestProgressPulseRenewsARegisteredRunsClaimLease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")

	// The build blocks on a file this test controls, so the claim stays
	// live — and the run stays registered with d.pulse — for exactly as
	// long as the test needs it to.
	cont := filepath.Join(dir, "continue")
	buildCmd := fmt.Sprintf(`while [ ! -f %s ]; do sleep 0.02; done; echo done > out.txt && git add -A && git -c user.name=b -c user.email=b@i commit -m work`, cont)
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
build_timeout: 10s
boxes:
  - name: shop
    repos:
      - name: work
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
`, db, root, workRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := loops["shop/work"]
	if !ok {
		t.Fatal("shop/work: no loop assembled")
	}

	item, err := d.queue.Submit(ctx, queue.WorkItem{ID: "wi-pulse", Tenant: d.tenant, Box: "shop", Ingress: "ing-pulse", Task: "t"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		lp.RunHolding(ctx, item, forge.Lease{}, nil)
	}()
	// The background run must be released — and let finish, still against
	// an open store — before the store itself closes underneath it.
	t.Cleanup(func() {
		os.WriteFile(cont, []byte("go"), 0o644)
		<-done
		d.Close()
	})

	leaser := d.store.Leaser(d.cfgRunner())
	var before forge.Lease
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, ok, err := leaser.Inspect(ctx, d.tenant, queue.ClaimRepo, queue.ClaimRef(item.ID))
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			before = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim lease never appeared — the run never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Let real wall time move so a renewed expiry is observably later than
	// the one Claim itself granted.
	time.Sleep(50 * time.Millisecond)
	d.progress(ctx, item.ID, acp.Update{Kind: acp.UpdateTool, Call: "c-1", Tool: "Bash", Text: "working"})

	after, ok, err := leaser.Inspect(ctx, d.tenant, queue.ClaimRepo, queue.ClaimRef(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("claim lease gone after the pulse")
	}
	if !after.Expires.After(before.Expires) {
		t.Fatalf("pulse did not renew the claim lease: before %v, after %v", before.Expires, after.Expires)
	}
	if after.Token != before.Token {
		t.Fatalf("pulse changed the claim's fencing token: before %v, after %v — want a renewal, not a new grant", before.Token, after.Token)
	}
}
