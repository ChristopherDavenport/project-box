package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

type fixture struct {
	store   *sqlite.Store
	advance func(time.Duration)
	remote  string
	repo    forge.Repo
	root    string
	base    forge.Commit
}

// fix builds a real remote (a bare repo with one commit), a store with a
// controllable clock, and a root for mirrors and workspaces.
func fix(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	var mu sync.Mutex
	offset := time.Duration(0)
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return time.Now().Add(offset)
	}))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	remote := filepath.Join(t.TempDir(), "remote.git")
	seed := t.TempDir()
	mustGit(t, "", "init", "--bare", remote)
	// A bare init points HEAD at master; our branch is main. Without this,
	// a fresh clone of the remote checks out nothing and builds an unrelated
	// history — a test artifact, not a landing behavior.
	mustGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	mustGit(t, "", "clone", remote, filepath.Join(seed, "clone"))
	clone := filepath.Join(seed, "clone")
	os.WriteFile(filepath.Join(clone, "README.md"), []byte("seed\n"), 0o644)
	mustGit(t, clone, "add", ".")
	mustGit(t, clone, "commit", "-m", "seed")
	mustGit(t, clone, "push", "origin", "HEAD:refs/heads/main")
	base := forge.Commit(strings.TrimSpace(mustGit(t, clone, "rev-parse", "HEAD")))

	f := &fixture{
		store: s,
		advance: func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			offset += d
		},
		remote: remote,
		repo:   forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"},
		root:   t.TempDir(),
		base:   base,
	}
	_ = ctx
	return f
}

func (f *fixture) mirror(t *testing.T, runner string) *Mirror {
	t.Helper()
	m, err := NewMirror(f.root, tn, f.store.Leaser(runner))
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if err := m.Fetch(context.Background(), f.repo); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return m
}

// buildOn makes one commit in the workspace and returns its sha.
func buildOn(t *testing.T, ws forge.Workspace, msg string) forge.Commit {
	t.Helper()
	os.WriteFile(filepath.Join(ws.Dir, "change.txt"), []byte(msg+"\n"), 0o644)
	mustGit(t, ws.Dir, "add", ".")
	mustGit(t, ws.Dir, "commit", "-m", msg)
	return forge.Commit(strings.TrimSpace(mustGit(t, ws.Dir, "rev-parse", "HEAD")))
}

func changeset(f *fixture, id, run string, head forge.Commit, l forge.Lease) forge.ChangeSet {
	return forge.ChangeSet{
		ID: id, Tenant: tn, Run: run, Intent: "test",
		Members: []forge.Member{{
			Repo: "app", Branch: "main", Base: f.base, Head: head, Lease: l,
		}},
	}
}

func TestLandMovesRemoteAndLeavesEvidence(t *testing.T) {
	f := fix(t)
	ctx := context.Background()
	m := f.mirror(t, "runner-a")
	leaser := f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := m.Checkout(ctx, lease, f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head := buildOn(t, ws, "feature")

	landing, err := NewLander(m, f.store.Log()).Land(ctx, changeset(f, "cs-1", "run-1", head, lease))
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !landing.Complete() || landing.Landed[0].Now != head {
		t.Fatalf("landing wrong: %+v", landing)
	}
	// The remote actually moved.
	got := strings.TrimSpace(mustGit(t, f.remote, "rev-parse", "refs/heads/main"))
	if forge.Commit(got) != head {
		t.Fatalf("remote main = %s, want %s", got, head)
	}
	// And the audit query works: the head sha leads to the land record.
	found := false
	for r, err := range f.store.Log().BySubject(ctx, tn, string(head)) {
		if err != nil {
			t.Fatalf("by subject: %v", err)
		}
		if r.Kind == "land" {
			found = true
		}
	}
	if !found {
		t.Fatal("no land evidence keyed by the landed commit")
	}
}

// TestStalledRunnerIsFencedAtLand is the Phase 1 exit criterion: a runner
// that stalls past its lease loses at land time, by its superseded token —
// not by luck of the push race.
func TestStalledRunnerIsFencedAtLand(t *testing.T) {
	f := fix(t)
	ctx := context.Background()
	m := f.mirror(t, "runner-a")
	stalled := f.store.Leaser("runner-a")

	lease, err := stalled.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := m.Checkout(ctx, lease, f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head := buildOn(t, ws, "built before the stall")

	// The runner stalls past its deadline; another claims the ref.
	f.advance(2 * time.Hour)
	if _, err := f.store.Leaser("runner-b").Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-2", TTL: time.Hour}); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	// The stalled runner wakes and tries to land. The fence, not the push,
	// must stop it.
	_, err = NewLander(m, f.store.Log()).Land(ctx, changeset(f, "cs-1", "run-1", head, lease))
	if !errors.Is(err, forge.ErrStaleToken) {
		t.Fatalf("want ErrStaleToken at land, got %v", err)
	}
	// The remote never saw the stalled runner's commit.
	got := strings.TrimSpace(mustGit(t, f.remote, "rev-parse", "refs/heads/main"))
	if forge.Commit(got) != f.base {
		t.Fatalf("remote moved despite fencing: %s", got)
	}
	// The refusal is itself evidence.
	refused := false
	for r, err := range f.store.Log().BySubject(ctx, tn, string(head)) {
		if err == nil && r.Kind == "land-refused" {
			refused = true
		}
	}
	if !refused {
		t.Fatal("fencing refusal left no evidence")
	}
}

// TestConcurrentLandersOneWins races two full build-and-land attempts for the
// same branch under -race. The lease serializes them: the loser fails at
// Acquire, never by corrupting the branch. Each runner gets its own mirror
// root, as each runner machine does in reality.
func TestConcurrentLandersOneWins(t *testing.T) {
	f := fix(t)
	ctx := context.Background()
	roots := []string{t.TempDir(), t.TempDir()}

	// Self-contained: no t.* calls from inside the goroutines.
	land := func(root, runner, run string) error {
		leaser := f.store.Leaser(runner)
		m, err := NewMirror(root, tn, leaser)
		if err != nil {
			return err
		}
		if err := m.Fetch(ctx, f.repo); err != nil {
			return err
		}
		lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: run, TTL: time.Hour})
		if err != nil {
			return err
		}
		ws, err := m.Checkout(ctx, lease, f.base)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(ws.Dir, "change.txt"), []byte(runner+"\n"), 0o644); err != nil {
			return err
		}
		if _, err := run2(ctx, ws.Dir, "add", "."); err != nil {
			return err
		}
		if _, err := run2(ctx, ws.Dir, "commit", "-m", "change by "+runner); err != nil {
			return err
		}
		out, err := run2(ctx, ws.Dir, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		head := forge.Commit(strings.TrimSpace(out))
		if _, err := NewLander(m, f.store.Log()).Land(ctx, changeset(f, "cs-"+run, run, head, lease)); err != nil {
			return err
		}
		return leaser.Release(ctx, lease)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, runner := range []string{"runner-a", "runner-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = land(roots[i], runner, "run-"+runner)
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, forge.ErrHeld):
		default:
			// The lease was released by the winner, so the loser may instead
			// have acquired afterwards and been refused by the push's
			// expected-old-value check — also a correct outcome.
			if !strings.Contains(err.Error(), "push refused") {
				t.Errorf("loser failed some other way: %v", err)
			}
		}
	}
	if wins != 1 {
		t.Fatalf("want exactly one landing, got %d (errs: %v)", wins, errs)
	}
	// main is exactly one commit ahead of base — no interleaving.
	n := strings.TrimSpace(mustGit(t, f.remote, "rev-list", "--count", string(f.base)+"..refs/heads/main"))
	if n != "1" {
		t.Fatalf("main advanced by %s commits, want 1", n)
	}
}

// TestPushPresentsExpectedOldValue: if the branch moved underneath (a write
// that did not come through the lease), the push's expected-old-value check
// refuses rather than overwrites.
func TestPushPresentsExpectedOldValue(t *testing.T) {
	f := fix(t)
	ctx := context.Background()
	m := f.mirror(t, "runner-a")
	leaser := f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := m.Checkout(ctx, lease, f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head := buildOn(t, ws, "ours")

	// Someone pushes to the remote outside the control plane entirely.
	rogue := filepath.Join(t.TempDir(), "rogue")
	mustGit(t, "", "clone", f.remote, rogue)
	os.WriteFile(filepath.Join(rogue, "rogue.txt"), []byte("out of band\n"), 0o644)
	mustGit(t, rogue, "add", ".")
	mustGit(t, rogue, "commit", "-m", "rogue")
	mustGit(t, rogue, "push", "origin", "HEAD:refs/heads/main")

	_, err = NewLander(m, f.store.Log()).Land(ctx, changeset(f, "cs-1", "run-1", head, lease))
	if err == nil {
		t.Fatal("push should have been refused: branch moved from expected base")
	}
	// The out-of-band commit survives; ours did not overwrite it.
	log := mustGit(t, f.remote, "log", "--format=%s", "refs/heads/main")
	if !strings.Contains(log, "rogue") || strings.Contains(log, "ours") {
		t.Fatalf("branch state wrong after refused push:\n%s", log)
	}
}

func TestPruneReapsDeadWorkspaces(t *testing.T) {
	f := fix(t)
	ctx := context.Background()
	m := f.mirror(t, "runner-a")
	leaser := f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := m.Checkout(ctx, lease, f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	// Live lease: prune must not touch it.
	if err := m.Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(ws.Dir); err != nil {
		t.Fatal("prune removed a workspace with a live lease")
	}
	// Dead lease: reaped.
	f.advance(2 * time.Hour)
	if err := m.Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(ws.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prune left the workspace of an expired lease")
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out
}

// run2 is the package-internal git runner, aliased for goroutine-safe use in
// tests (no t.Fatalf off the test goroutine).
func run2(ctx context.Context, dir string, args ...string) (string, error) {
	return run(ctx, dir, args...)
}

var _ = evidence.Record{} // keep the import honest if assertions change
