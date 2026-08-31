package loop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

// scriptedBuilder is the loop's Builder seam filled with a deterministic
// change: proof the LOOP works unattended. Whether a real agent produces
// landable changes is the premise the live run tests, not this one.
type scriptedBuilder struct{}

func (scriptedBuilder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if err := os.WriteFile(filepath.Join(ws.Dir, "answer.txt"), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", task); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

type env struct {
	store  *sqlite.Store
	remote string
	repo   forge.Repo
	loop   *Loop
	router *ingress.StoreRouter
}

func setup(t *testing.T, gateCmd []string) *env {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	remote := filepath.Join(t.TempDir(), "remote.git")
	mustSh(t, "", "git", "init", "--bare", remote)
	mustSh(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), "seed")
	mustSh(t, "", "git", "clone", remote, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644)
	mustSh(t, seed, "git", "add", ".")
	mustSh(t, seed, "git", "commit", "-m", "seed")
	mustSh(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")

	repo := forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
	m, err := git.NewMirror(t.TempDir(), tn, s.Leaser("runner-1"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{
		store: s, remote: remote, repo: repo,
		loop: &Loop{
			Store: s, Runner: "runner-1", Mirror: m, Repo: repo, Branch: "main",
			Builder: scriptedBuilder{},
			Gates: func(ws forge.Workspace) []forge.Gate {
				return []forge.Gate{gate.Command{Label: "build-and-test", Dir: ws.Dir, Commands: [][]string{gateCmd}}}
			},
			TTL: time.Hour,
		},
		router: &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "runner-1"},
	}
}

func issueRoute() ingress.Route {
	return ingress.Route{Name: "forge-issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "chris"}
}

// TestIssueToLandedCommitUnattended is the Phase 3 exit criterion end to end:
// opened issue -> work item -> build -> gate -> landed commit, no hands, and
// the whole chain answerable from the landed sha.
func TestIssueToLandedCommitUnattended(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"}) // a real command that passes in the workspace
	ctx := context.Background()

	ing, opened, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-42", Payload: []byte(`{"title":"fix it"}`)})
	if err != nil || !opened {
		t.Fatalf("open: %v opened=%v", err, opened)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, ItemFor(ing, "wi-1", "close issue 42"))
	if err != nil {
		t.Fatal(err)
	}

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone || item.Head == "" {
		t.Fatalf("item did not finish: %+v", item)
	}
	// The remote really moved to the produced commit.
	got := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if got != item.Head {
		t.Fatalf("remote main=%s want %s", got, item.Head)
	}
	// The auditor's query: everything about this sha, from one call.
	kinds := map[string]bool{}
	for r, err := range e.store.Log().BySubject(ctx, tn, item.Head) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind] = true
	}
	for _, want := range []string{"built", "gate", "land", "done"} {
		if !kinds[want] {
			t.Fatalf("provenance for %s missing %q (have %v)", item.Head, want, kinds)
		}
	}
	// And the item traces to its ingress — no orphan work.
	if item.Ingress != ing.ID {
		t.Fatalf("item lost its origination: %+v", item)
	}
	// The chains verify end to end.
	for _, scope := range []string{"workitem/wi-1", "ingress/" + ing.ID, "changeset/cs-wi-1"} {
		if err := e.store.Log().Verify(ctx, tn, scope); err != nil {
			t.Fatalf("chain %s: %v", scope, err)
		}
	}
}

// TestFailingGateParksInsteadOfLanding: the other half of the criterion.
func TestFailingGateParksInsteadOfLanding(t *testing.T) {
	e := setup(t, []string{"false"})
	ctx := context.Background()

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-43"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-2", "break things"))
	before := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("failing gate should park, got %s", item.State)
	}
	if !strings.Contains(item.Note, "build-and-test") {
		t.Fatalf("parked reason should name the gate: %q", item.Note)
	}
	after := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if before != after {
		t.Fatal("a parked change reached the remote")
	}
	// The queue's fold agrees with the returned state.
	loaded, err := q.Load(ctx, tn, "wi-2")
	if err != nil || loaded.State != queue.StateParked {
		t.Fatalf("fold: %+v %v", loaded, err)
	}
}

// TestRedeliveryOpensOneIngress: webhooks retry, alerts flap; the same
// occurrence names one ingress however many times it arrives.
func TestRedeliveryOpensOneIngress(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	first, opened, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-44"})
	if err != nil || !opened {
		t.Fatalf("first: %v %v", err, opened)
	}
	second, opened, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-44"})
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("redelivery opened a second ingress")
	}
	if second.ID != first.ID {
		t.Fatalf("redelivery named a different ingress: %s vs %s", second.ID, first.ID)
	}
	// A route with no principal opens nothing.
	r := issueRoute()
	r.Requester = ""
	if _, _, err := e.router.Open(ctx, r, ingress.Delivery{Key: "issue-45"}); err == nil {
		t.Fatal("a requesterless route must be refused")
	}
	// An orphan work item is refused at submit.
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	if _, err := q.Submit(ctx, queue.WorkItem{ID: "wi-x", Tenant: tn, Box: "shop", Task: "who asked"}); err == nil {
		t.Fatal("orphan work was accepted")
	}
}

func sh(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("%s %v: %w: %s", name, args, err, out.String())
	}
	return out.String(), err
}

func mustSh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := sh(dir, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
