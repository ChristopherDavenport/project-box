package knowledge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	queuepkg "github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func base(t *testing.T) *Base {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Base{Store: s, Tenant: tn}
}

func TestRetrievalStaysInsideTheGrant(t *testing.T) {
	b := base(t)
	ctx := context.Background()
	put := func(scope, claim string) {
		if _, err := b.Put(ctx, Record{Scope: scope, Claim: claim, Producer: "curator"}); err != nil {
			t.Fatal(err)
		}
	}
	put("global", "the org pins Go via go.mod")
	put("box/shop", "shop's API is contract-first")
	put("box/warehouse", "warehouse batch jobs run at 02:00")

	// Granted global+shop: warehouse contributes nothing, even asked for by
	// name — an empty answer, not an error, because which scopes exist is
	// itself information.
	got, err := b.Retrieve(ctx, Query{Granted: []string{"global", "box/shop"}, Scopes: []string{"box/warehouse", "box/shop"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Scope != "box/shop" {
		t.Fatalf("retrieval crossed the grant: %+v", got)
	}
	// Keyword narrowing within the grant.
	got, _ = b.Retrieve(ctx, Query{Granted: []string{"global", "box/shop"}, Contains: []string{"go.mod"}})
	if len(got) != 1 || !strings.Contains(got[0].Claim, "go.mod") {
		t.Fatalf("keyword: %+v", got)
	}
	// Ungranted principal: nothing at all.
	if got, _ := b.Retrieve(ctx, Query{Granted: nil}); len(got) != 0 {
		t.Fatalf("empty grant returned records: %+v", got)
	}
	// A record without a producer is refused.
	if _, err := b.Put(ctx, Record{Scope: "global", Claim: "anonymous"}); err == nil {
		t.Fatal("producerless knowledge accepted")
	}
}

func TestSupersedeReplacesWithoutErasing(t *testing.T) {
	b := base(t)
	ctx := context.Background()
	old, err := b.Put(ctx, Record{Scope: "global", Claim: "deploys run at 17:00", Producer: "curator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Put(ctx, Record{Scope: "global", Claim: "deploys run at 09:00", Producer: "curator", Supersedes: old.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Retrieve(ctx, Query{Granted: []string{"global"}})
	if len(got) != 1 || !strings.Contains(got[0].Claim, "09:00") {
		t.Fatalf("supersede: %+v", got)
	}
	// The old record still exists in the chain — corrected, not erased.
	if err := b.Store.Log().Verify(ctx, tn, "knowledge/global"); err != nil {
		t.Fatal(err)
	}
}

// TestSweepFlagsStaleWithoutAnyoneMarking is the Phase 6 exit criterion: a
// record bound to a file that has since changed is flagged by the sweep, and
// retrieval shows it stale rather than hiding it.
func TestSweepFlagsStaleWithoutAnyoneMarking(t *testing.T) {
	b := base(t)
	ctx := context.Background()

	remote := filepath.Join(t.TempDir(), "r.git")
	mustRun(t, "", "git", "init", "--bare", remote)
	mustRun(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	work := filepath.Join(t.TempDir(), "w")
	mustRun(t, "", "git", "clone", remote, work)
	os.WriteFile(filepath.Join(work, "handlers.go"), []byte("package x // v1\n"), 0o644)
	os.WriteFile(filepath.Join(work, "README.md"), []byte("docs\n"), 0o644)
	mustRun(t, work, "git", "add", ".")
	mustRun(t, work, "git", "commit", "-m", "v1")
	mustRun(t, work, "git", "push", "origin", "HEAD:refs/heads/main")
	v1 := rev(t, work)

	// Two bound claims: one to the file that will change, one to the file
	// that will not.
	aboutHandlers, err := b.Put(ctx, Record{Scope: "repo/app", Claim: "handlers.go validates before dispatch",
		Producer: "curator", Repo: remote, Path: "handlers.go", Commit: v1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Put(ctx, Record{Scope: "repo/app", Claim: "README covers setup",
		Producer: "curator", Repo: remote, Path: "README.md", Commit: v1}); err != nil {
		t.Fatal(err)
	}
	// A bound record without its commit is refused.
	if _, err := b.Put(ctx, Record{Scope: "repo/app", Claim: "vague", Producer: "c", Repo: remote}); err == nil {
		t.Fatal("unpinned binding accepted")
	}

	// The code moves.
	os.WriteFile(filepath.Join(work, "handlers.go"), []byte("package x // v2: dispatch first\n"), 0o644)
	mustRun(t, work, "git", "add", ".")
	mustRun(t, work, "git", "commit", "-m", "v2")
	mustRun(t, work, "git", "push", "origin", "HEAD:refs/heads/main")
	v2 := rev(t, work)

	flagged, err := b.Sweep(ctx, "repo/app", map[string]string{remote: v2})
	if err != nil {
		t.Fatal(err)
	}
	if len(flagged) != 1 || flagged[0].ID != aboutHandlers.ID {
		t.Fatalf("sweep flagged %+v, want exactly the handlers claim", flagged)
	}
	got, _ := b.Retrieve(ctx, Query{Granted: []string{"repo/app"}})
	byClaim := map[string]bool{}
	for _, r := range got {
		byClaim[r.Claim] = r.Stale
	}
	if !byClaim["handlers.go validates before dispatch"] {
		t.Fatal("the changed binding is not marked stale")
	}
	if byClaim["README covers setup"] {
		t.Fatal("an unchanged binding was marked stale")
	}
	// Idempotent: a second sweep flags nothing new.
	if again, _ := b.Sweep(ctx, "repo/app", map[string]string{remote: v2}); len(again) != 0 {
		t.Fatalf("second sweep re-flagged: %+v", again)
	}
}

func rev(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

func TestCurateBindsPromotedClaimsToTheLandedRun(t *testing.T) {
	b := base(t)
	ctx := context.Background()
	item := queuepkg.WorkItem{ID: "wi-9", Tenant: tn, Box: "shop", Ingress: "ing-1",
		State: queuepkg.StateDone, Head: "abc123def"}
	got, err := Curate(ctx, b, item, "http://remote/env.git", "curator-agent", []Draft{
		{Claim: "the shop API validates before dispatch", Path: "handlers.go"},
		{Claim: "   "}, // blank drafts are dropped, not stored
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("curated: %+v", got)
	}
	r := got[0]
	if r.Commit != "abc123def" || r.Run != "wi-9" || r.Scope != "box/shop" || r.Path != "handlers.go" {
		t.Fatalf("binding wrong: %+v", r)
	}
	// A run that never landed has nothing to teach.
	parked := item
	parked.State = queuepkg.StateParked
	if _, err := Curate(ctx, b, parked, "http://remote/env.git", "curator-agent", []Draft{{Claim: "x"}}); err == nil {
		t.Fatal("a parked run curated knowledge")
	}
	// The promoted claim is retrievable under the box scope.
	found, _ := b.Retrieve(ctx, Query{Granted: []string{"box/shop"}})
	if len(found) != 1 || found[0].Run != "wi-9" {
		t.Fatalf("retrieve: %+v", found)
	}
}
