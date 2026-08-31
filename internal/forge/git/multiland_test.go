package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// twoRepoFix stands up two remotes (app depends on lib, so lib lands first)
// and returns everything a two-member ChangeSet needs.
type twoRepoFix struct {
	f      *fixture // reuses the single-repo fixture for store + app
	lib    forge.Repo
	libRem string
	libTip forge.Commit
}

func fix2(t *testing.T) *twoRepoFix {
	t.Helper()
	f := fix(t)
	rem := filepath.Join(t.TempDir(), "lib.git")
	mustGit(t, "", "init", "--bare", rem)
	mustGit(t, rem, "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), "libseed")
	mustGit(t, "", "clone", rem, seed)
	os.WriteFile(filepath.Join(seed, "lib.go"), []byte("package lib\n"), 0o644)
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "seed lib")
	mustGit(t, seed, "push", "origin", "HEAD:refs/heads/main")
	tip := forge.Commit(strings.TrimSpace(mustGit(t, seed, "rev-parse", "HEAD")))
	return &twoRepoFix{
		f: f, libRem: rem, libTip: tip,
		lib: forge.Repo{Name: "lib", Remote: rem, Role: forge.RoleLibrary, DefaultBranch: "main"},
	}
}

// build acquires, checks out, and commits one change in the named repo,
// returning the member ready for a ChangeSet.
func (x *twoRepoFix) build(t *testing.T, m *Mirror, leaser forge.Leaser, repo forge.Repo, base forge.Commit, run string) forge.Member {
	t.Helper()
	ctx := context.Background()
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: repo.Name, Ref: "refs/heads/main", Run: run, TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire %s: %v", repo.Name, err)
	}
	ws, err := m.Checkout(ctx, lease, base)
	if err != nil {
		t.Fatalf("checkout %s: %v", repo.Name, err)
	}
	head := buildOn(t, ws, "change "+repo.Name)
	return forge.Member{Repo: repo.Name, Branch: "main", Base: base, Head: head, Lease: lease}
}

func TestOrderedMultiMemberLanding(t *testing.T) {
	x := fix2(t)
	ctx := context.Background()
	m := x.f.mirror(t, "runner-a")
	if err := m.Fetch(ctx, x.lib); err != nil {
		t.Fatal(err)
	}
	leaser := x.f.store.Leaser("runner-a")

	libM := x.build(t, m, leaser, x.lib, x.libTip, "run-1")
	appM := x.build(t, m, leaser, x.f.repo, x.f.base, "run-1")
	cs := forge.ChangeSet{ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "contract bump",
		Members: []forge.Member{libM, appM}} // dependency order: lib first

	landing, err := NewLander(m, x.f.store.Log()).Land(ctx, cs)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !landing.Complete() || len(landing.Landed) != 2 {
		t.Fatalf("landing: %+v", landing)
	}
	if landing.Landed[0].Repo != "lib" {
		t.Fatal("members landed out of dependency order")
	}
	for rem, want := range map[string]forge.Commit{x.libRem: libM.Head, x.f.remote: appM.Head} {
		got := strings.TrimSpace(mustGit(t, rem, "rev-parse", "refs/heads/main"))
		if forge.Commit(got) != want {
			t.Fatalf("%s at %s, want %s", rem, got, want)
		}
	}
}

// TestMidLandingFailureRevertsForward is the Phase 4 partial-landing
// criterion: member two fails, member one is reverted FORWARD — the change
// and its undoing both on the branch, history intact, nothing rewritten.
func TestMidLandingFailureRevertsForward(t *testing.T) {
	x := fix2(t)
	ctx := context.Background()
	m := x.f.mirror(t, "runner-a")
	if err := m.Fetch(ctx, x.lib); err != nil {
		t.Fatal(err)
	}
	leaser := x.f.store.Leaser("runner-a")

	libM := x.build(t, m, leaser, x.lib, x.libTip, "run-1")
	appM := x.build(t, m, leaser, x.f.repo, x.f.base, "run-1")

	// Sabotage member two: app's main moves out-of-band after the build, so
	// its push will fail the expected-old-value check mid-landing.
	rogue := filepath.Join(t.TempDir(), "rogue")
	mustGit(t, "", "clone", x.f.remote, rogue)
	os.WriteFile(filepath.Join(rogue, "oob.txt"), []byte("moved\n"), 0o644)
	mustGit(t, rogue, "add", ".")
	mustGit(t, rogue, "commit", "-m", "out of band")
	mustGit(t, rogue, "push", "origin", "HEAD:refs/heads/main")

	cs := forge.ChangeSet{ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "contract bump",
		Members: []forge.Member{libM, appM}}
	landing, err := NewLander(m, x.f.store.Log()).Land(ctx, cs)
	if err == nil {
		t.Fatal("landing should have failed at the second member")
	}
	if landing.Failed != "app" || len(landing.Landed) != 1 || len(landing.Compensated) != 1 {
		t.Fatalf("landing shape wrong: %+v", landing)
	}

	// lib's branch: seed -> change -> revert. History intact, tree restored.
	count := strings.TrimSpace(mustGit(t, x.libRem, "rev-list", "--count", "refs/heads/main"))
	if count != "3" {
		t.Fatalf("lib history has %s commits, want 3 (seed, change, revert-forward)", count)
	}
	headTree := strings.TrimSpace(mustGit(t, x.libRem, "rev-parse", "refs/heads/main^{tree}"))
	baseTree := strings.TrimSpace(mustGit(t, x.libRem, "rev-parse", string(x.libTip)+"^{tree}"))
	if headTree != baseTree {
		t.Fatal("revert-forward did not restore the pre-changeset tree")
	}
	// The landed change is still IN history — nothing was rewritten.
	if !strings.Contains(mustGit(t, x.libRem, "log", "--format=%s", "refs/heads/main"), "change lib") {
		t.Fatal("the landed change vanished from history — that is a rewrite, not a revert")
	}
	// And the whole story is evidence: land, conflict, compensation.
	kinds := map[string]int{}
	for r, err := range x.f.store.Log().Since(ctx, tn, "changeset/cs-1", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind]++
	}
	if kinds["land"] != 1 || kinds["land-conflict"] != 1 || kinds["land-compensated"] != 1 {
		t.Fatalf("evidence kinds: %v", kinds)
	}
}
