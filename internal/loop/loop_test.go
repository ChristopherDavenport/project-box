package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/checks"
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

// countingBuilder is scriptedBuilder that says how often it was asked. The
// count is the whole point of the resume tests: a build is an agent run, and
// an agent run is the dominant cost of running this factory at all.
type countingBuilder struct {
	scriptedBuilder
	n int
}

func (c *countingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	c.n++
	return c.scriptedBuilder.Build(ctx, ws, task)
}

// holdingGate is the human-approval shape without the approval machinery:
// pending until someone flips the flag. approval.GateAdapter is the real one;
// all these tests need is the three-state Decision and a gate that keeps
// saying "not yet".
type holdingGate struct{ approved *bool }

func (holdingGate) Name() string { return "holding" }

func (g holdingGate) Evaluate(context.Context, forge.ChangeSet) (forge.Verdict, error) {
	if *g.approved {
		return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPass, Reason: "approved"}, nil
	}
	return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPending, Reason: "waiting on a person"}, nil
}

// TestWaitingOnAGateDoesNotRebuild is the cost regression. A pending gate means
// a person has not answered yet, which can last days, and the item is
// re-drained every tick throughout. Each of those ticks used to be a full agent
// run producing the same commit — routine review latency was the most expensive
// thing the factory could do. The build happens once.
func TestWaitingOnAGateDoesNotRebuild(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	approved := false
	builder := &countingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{holdingGate{approved: &approved}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-45"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-wait", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("pending gate should wait, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("first pass should build once, built %d times", builder.n)
	}
	built := item.Built
	if built == "" {
		t.Fatal("a waiting item carries no resume point: the next tick will rebuild")
	}

	// The person still has not answered. Drain reloads from the fold every
	// tick, so the resume point has to survive the store, not just the call.
	for tick := 1; tick <= 3; tick++ {
		loaded, lerr := q.Load(ctx, tn, "wi-wait")
		if lerr != nil {
			t.Fatal(lerr)
		}
		if loaded.Built != built {
			t.Fatalf("tick %d: the fold lost the resume point: %q", tick, loaded.Built)
		}
		if tick == 2 {
			// The daemon's housekeeping sweep runs on a timer of its own and
			// knows nothing about an item mid-wait. Its rule — reclaim trees
			// whose lease no longer answers — describes every waiting item,
			// because waiting releases the lease. It must not take this one.
			if perr := e.loop.Mirror.Prune(ctx); perr != nil {
				t.Fatalf("prune: %v", perr)
			}
		}
		item, err = e.loop.Run(ctx, loaded)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if item.State != queue.StateWaiting {
			t.Fatalf("tick %d: state %s (%s)", tick, item.State, item.Note)
		}
		if builder.n != 1 {
			t.Fatalf("tick %d rebuilt while waiting: %d builds", tick, builder.n)
		}
	}

	// The approval arrives. The commit the reviewer was looking at is the one
	// that lands — a rebuild here would land something nobody approved.
	approved = true
	loaded, err := q.Load(ctx, tn, "wi-wait")
	if err != nil {
		t.Fatal(err)
	}
	item, err = e.loop.Run(ctx, loaded)
	if err != nil {
		t.Fatalf("approved run: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("approved item should land, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("the approval should not rebuild: %d builds", builder.n)
	}
	if item.Head != built {
		t.Fatalf("landed %s but the gate approved %s", item.Head, built)
	}
	if onMain := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main")); onMain != built {
		t.Fatalf("remote main is %s, expected the approved %s", onMain, built)
	}
}

// interlope lands a commit on main from outside the factory: the other unit
// of work that won the race, seen from this one's point of view.
func interlope(t *testing.T, remote, file, body string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	mustSh(t, "", "git", "clone", remote, other)
	if err := os.WriteFile(filepath.Join(other, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustSh(t, other, "git", "add", ".")
	mustSh(t, other, "git", "commit", "-m", "someone else")
	mustSh(t, other, "git", "push", "origin", "HEAD:refs/heads/main")
}

// TestMovedBranchReplaysRatherThanRebuilds is what makes a narrow branch
// lease affordable (issue #18). Concurrency on one branch guarantees losers:
// whoever does not land first built against a base that has moved. If the
// loser had to rebuild, narrowing the lease would buy wall-clock with tokens
// — an agent session per collision — which is the wrong trade. It replays.
func TestMovedBranchReplaysRatherThanRebuilds(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	approved := false
	builder := &countingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{holdingGate{approved: &approved}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-46"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-moved", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting || builder.n != 1 {
		t.Fatalf("expected waiting after one build, got %s / %d builds", item.State, builder.n)
	}
	firstBuilt := item.Built

	interlope(t, e.remote, "elsewhere.txt", "moved\n")

	loaded, err := q.Load(ctx, tn, "wi-moved")
	if err != nil {
		t.Fatal(err)
	}
	item, err = e.loop.Run(ctx, loaded)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if builder.n != 1 {
		t.Fatalf("a moved base must replay, not rebuild: %d builds", builder.n)
	}
	if item.Built == firstBuilt {
		t.Fatal("the resume point should be the replayed commit, not the old one")
	}

	// And the replayed commit is what lands, carrying both changes.
	approved = true
	loaded, _ = q.Load(ctx, tn, "wi-moved")
	item, err = e.loop.Run(ctx, loaded)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("state %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("landing a replayed change rebuilt it: %d builds", builder.n)
	}
	for _, want := range []string{"elsewhere.txt", "answer.txt"} {
		out := mustSh(t, e.remote, "git", "ls-tree", "--name-only", "refs/heads/main")
		if !strings.Contains(out, want) {
			t.Fatalf("main lost %s after the replay:\n%s", want, out)
		}
	}
}

// TestConflictingReplayFallsBackToTheAgent: replaying is an optimization and
// stops where judgement begins. A conflict is a decision about intent, the
// agent is who makes those, and a resolution nobody reviewed is not something
// to land.
func TestConflictingReplayFallsBackToTheAgent(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	approved := false
	builder := &countingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{holdingGate{approved: &approved}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-47"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-conflict", "add a thing"))

	if item, err = e.loop.Run(ctx, item); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("expected waiting, got %s", item.State)
	}

	// The interloper writes the very file the builder writes, differently.
	interlope(t, e.remote, "answer.txt", "someone else's answer\n")

	loaded, _ := q.Load(ctx, tn, "wi-conflict")
	if _, err = e.loop.Run(ctx, loaded); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if builder.n != 2 {
		t.Fatalf("a conflicting replay must fall back to the agent: %d builds", builder.n)
	}
}

// TestBranchIsFreeWhileBuilding is issue #18's property, asserted directly
// rather than by timing: the build no longer holds the branch, so a second
// unit could be building against the same base at the same moment. Under the
// old shape this lease was held from before checkout until after the land.
func TestBranchIsFreeWhileBuilding(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()

	var heldDuringBuild error
	e.loop.Builder = probingBuilder{probe: func() {
		// A DIFFERENT runner asks for the branch mid-build.
		l, err := e.store.Leaser("runner-2").Acquire(ctx, forge.LeaseRequest{
			Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "probe", TTL: time.Minute,
		})
		if err != nil {
			heldDuringBuild = err
			return
		}
		e.store.Leaser("runner-2").Release(ctx, l)
	}}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-48"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-free", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if heldDuringBuild != nil {
		t.Fatalf("the branch was still held across the build: %v", heldDuringBuild)
	}
	// And the landing still happened under the branch lease, fenced as ever.
	if item.State != queue.StateDone {
		t.Fatalf("state %s (%s)", item.State, item.Note)
	}
}

// probingBuilder runs a probe in the middle of the build, then behaves like
// scriptedBuilder.
type probingBuilder struct {
	scriptedBuilder
	probe func()
}

func (b probingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.probe()
	return b.scriptedBuilder.Build(ctx, ws, task)
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

// TestAuthorizationIsEnforcedInThePipeline closes the audit gap: the
// evaluator's rules bind the actual loop, not only its own tests. The
// runner's authority intersects with the requester's — an agent lands
// nothing its human could not.
func TestAuthorizationIsEnforcedInThePipeline(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	grants := authz.Static{}
	e.loop.Authz = &authz.Evaluator{Grants: grants}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-authz"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}

	// Nobody holds anything: parked at propose, with the denial recorded.
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-az1", "change something"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "not authorized") {
		t.Fatalf("ungranted work was not parked: %+v", item)
	}

	// Runner may propose and land; the REQUESTER may only propose:
	// delegation intersects in the pipeline, so the land is refused.
	grants["runner-1"] = []authz.Grant{{ID: "g-runner", Subject: "runner-1", Tenant: string(tn),
		Actions: []authz.Action{authz.ActionPropose, authz.ActionLand}}}
	grants["chris"] = []authz.Grant{{ID: "g-chris", Subject: "chris", Tenant: string(tn),
		Actions: []authz.Action{authz.ActionPropose}}}
	item2, _ := q.Submit(ctx, ItemFor(ing, "wi-az2", "change again"))
	item2, err = e.loop.Run(ctx, item2)
	if err != nil {
		t.Fatal(err)
	}
	if item2.State != queue.StateParked || !strings.Contains(item2.Note, "delegator") {
		t.Fatalf("delegation escalated in the pipeline: %+v", item2)
	}

	// Grant the requester land too: through, with the evidence showing WHO
	// (actor), FOR WHOM (on-behalf-of), and WHY ALLOWED (the grant).
	grants["chris"][0].Actions = append(grants["chris"][0].Actions, authz.ActionLand)
	item3, _ := q.Submit(ctx, ItemFor(ing, "wi-az3", "third time"))
	item3, err = e.loop.Run(ctx, item3)
	if err != nil || item3.State != queue.StateDone {
		t.Fatalf("granted work did not land: %+v %v", item3, err)
	}
	sawGrant, sawOBO := false, false
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-az3", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "authz" && r.Grant == "g-runner" {
			sawGrant = true
		}
		if r.OnBehalfOf == "chris" && r.Actor == "runner-1" {
			sawOBO = true
		}
	}
	if !sawGrant || !sawOBO {
		t.Fatalf("evidence missing the authority story: grant=%v obo=%v", sawGrant, sawOBO)
	}
}

// TestEscalationAndHandoff: refusal parks with the agent's reason attributed
// to the SEAT, and closure runs on settle — the handoff hook fires for
// parked work too, because "here is where I got stuck" is what the next
// occupant needs most.
func TestEscalationAndHandoff(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	var handedOff []queue.WorkItem
	e.loop.Builder = escalatingBuilder{}
	e.loop.Handoff = func(_ context.Context, item queue.WorkItem) error {
		handedOff = append(handedOff, item)
		return nil
	}
	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-esc"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-esc", "do the impossible"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("escalation did not park with attribution: %+v", item)
	}
	saw := false
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-esc", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "escalated" && r.Actor == "mason" && strings.Contains(string(r.Body), "credentials") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("escalation evidence missing or unattributed")
	}
	if len(handedOff) != 1 || handedOff[0].State != queue.StateParked {
		t.Fatalf("handoff did not run on settle: %+v", handedOff)
	}
}

type escalatingBuilder struct{}

func (escalatingBuilder) Build(context.Context, forge.Workspace, string) (forge.Commit, error) {
	return "", &Escalation{Seat: "mason", Reason: "task needs credentials I do not hold"}
}

// scripted is a gate that answers from a script, one entry per evaluation.
// The counter lives behind a pointer because the loop rebuilds its gates for
// the confirming re-run.
type scripted struct {
	label string
	calls *int
	step  func(n int) (forge.Verdict, error)
}

func (s scripted) Name() string { return s.label }
func (s scripted) Evaluate(context.Context, forge.ChangeSet) (forge.Verdict, error) {
	n := *s.calls
	*s.calls++
	return s.step(n)
}

// optimistic reconfigures the test loop for the land-then-check lane and
// counts how often repair is summoned.
func optimistic(e *env) *int {
	reds := new(int)
	e.loop.Lane = forge.LaneOptimistic
	e.loop.Red = func(context.Context, queue.WorkItem, forge.Commit, string) (string, error) {
		*reds++
		return "ing-repair", nil
	}
	return reds
}

func chainStatuses(t *testing.T, e *env, sha string) []checks.Status {
	t.Helper()
	var out []checks.Status
	for rec, err := range e.store.Log().Since(context.Background(), tn, "checks/app", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject != sha {
			continue
		}
		var st checks.Status
		if err := json.Unmarshal(rec.Body, &st); err != nil {
			t.Fatal(err)
		}
		out = append(out, st)
	}
	return out
}

// TestFlakyRedIsRecordedNotRepaired is issue #2: a post-land red is re-run
// once before anyone is summoned. Red-then-green is a flaky check — both
// looks go on the chain, the commit ends green, and no repair work opens.
func TestFlakyRedIsRecordedNotRepaired(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	reds := optimistic(e)
	calls := new(int)
	e.loop.Gates = func(ws forge.Workspace) []forge.Gate {
		return []forge.Gate{scripted{label: "post-land", calls: calls, step: func(n int) (forge.Verdict, error) {
			if n == 0 {
				return forge.Verdict{Gate: "post-land", Decision: forge.DecisionFail, Reason: "spurious timeout"}, nil
			}
			return forge.Verdict{Gate: "post-land", Decision: forge.DecisionPass, Reason: "clean"}, nil
		}}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-flaky"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-flaky", "land through a flaky check"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone || !strings.Contains(item.Note, "flaky") {
		t.Fatalf("flaky red should finish with the flake on record: %+v", item)
	}
	if *reds != 0 {
		t.Fatalf("a flaky red summoned repair %d times", *reds)
	}
	if *calls != 2 {
		t.Fatalf("the check ran %d times, want observe + confirm", *calls)
	}
	// The commit ends green — promotable — with both observations recorded.
	green, err := checks.IsGreen(ctx, e.store, tn, "app", item.Head)
	if err != nil || !green {
		t.Fatalf("flaky-then-green commit should be green: %v %v", green, err)
	}
	sts := chainStatuses(t, e, item.Head)
	if len(sts) != 2 || sts[0].Green || !sts[1].Green || !sts[1].Flaky {
		t.Fatalf("want observed red then flaky green on the chain: %+v", sts)
	}
}

// TestConfirmedRedOpensRepair: red twice is a real red — the check runs
// again, both observations go on the chain, and repair opens exactly once.
func TestConfirmedRedOpensRepair(t *testing.T) {
	e := setup(t, []string{"false"})
	ctx := context.Background()
	reds := optimistic(e)

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-red"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-red", "break the build"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone || !strings.Contains(item.Note, "repair opened") {
		t.Fatalf("confirmed red should finish with repair on record: %+v", item)
	}
	if *reds != 1 {
		t.Fatalf("repair summoned %d times, want once", *reds)
	}
	if green, _ := checks.IsGreen(ctx, e.store, tn, "app", item.Head); green {
		t.Fatal("a confirmed red reads green")
	}
	sts := chainStatuses(t, e, item.Head)
	if len(sts) != 2 || sts[0].Green || sts[1].Green {
		t.Fatalf("want two red observations on the chain: %+v", sts)
	}
}

// TestUnconfirmedRedParksWithoutRepair: the confirming re-run could not
// conclude. One red observed is a third state — the red stands on the chain,
// nobody is summoned, and the item parks for a person.
func TestUnconfirmedRedParksWithoutRepair(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	reds := optimistic(e)
	calls := new(int)
	e.loop.Gates = func(ws forge.Workspace) []forge.Gate {
		return []forge.Gate{scripted{label: "post-land", calls: calls, step: func(n int) (forge.Verdict, error) {
			if n == 0 {
				return forge.Verdict{Gate: "post-land", Decision: forge.DecisionFail, Reason: "assert blew up"}, nil
			}
			return forge.Verdict{}, errors.New("runner lost the workspace")
		}}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-unconfirmed"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-unconfirmed", "break the runner"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "unconfirmed") {
		t.Fatalf("an unconfirmable red should park as such: %+v", item)
	}
	if *reds != 0 {
		t.Fatalf("an unconfirmed red summoned repair %d times", *reds)
	}
	// The change did land — a parked item does not set Head, so ask the remote.
	head := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if green, _ := checks.IsGreen(ctx, e.store, tn, "app", head); green {
		t.Fatal("an unconfirmed red reads green")
	}
	if sts := chainStatuses(t, e, head); len(sts) != 1 || sts[0].Green {
		t.Fatalf("the observed red should stand alone on the chain: %+v", sts)
	}
}

// TestRepairChainIsBounded: one repair per red sha is not a bound on the
// CHAIN. Each repair produces a NEW sha, which can itself be red, so an
// optimistic lane over a genuinely broken tree opens repair after repair —
// every one a real agent session — and sha-keyed idempotency cannot collapse
// them because no two are the same sha.
func TestRepairChainIsBounded(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	route := ingress.Route{Name: "red-main", Kind: ingress.RouteRedMain,
		Opens: ingress.KindIssue, Box: "shop", Requester: "gargoyle"}
	red := OpenRepair(e.router, route, 2)

	id, err := red(ctx, queue.WorkItem{ID: "wi-0"}, "sha-a", "tests fail")
	if err != nil || id == "" {
		t.Fatalf("the first repair should open: %q %v", id, err)
	}
	// The repair is one deeper than the work that summoned it, and that is
	// what the next one will be measured against.
	head, err := e.store.Log().Head(ctx, tn, "ingress/"+id)
	if err != nil {
		t.Fatal(err)
	}
	var ing ingress.Ingress
	if err := json.Unmarshal(head.Body, &ing); err != nil {
		t.Fatal(err)
	}
	if ing.Depth != 1 {
		t.Fatalf("repair opened at depth %d, want 1", ing.Depth)
	}

	if _, err := red(ctx, queue.WorkItem{ID: "wi-1", Depth: 1}, "sha-b", "still failing"); err != nil {
		t.Fatalf("depth 1 is still inside the bound: %v", err)
	}
	// At the bound the factory stops summoning itself. The red stays on the
	// check chain either way, so promotion still never takes the commit.
	if _, err := red(ctx, queue.WorkItem{ID: "wi-2", Depth: 2}, "sha-c", "still failing"); err == nil {
		t.Fatal("the repair chain was allowed to run away")
	}
}

// countingGate is an expensive deterministic check — the build, the test
// suite — that says how often it was asked.
type countingGate struct {
	name string
	n    *int
}

func (g countingGate) Name() string { return g.name }

func (g countingGate) Evaluate(context.Context, forge.ChangeSet) (forge.Verdict, error) {
	*g.n++
	return forge.Verdict{Gate: g.name, Decision: forge.DecisionPass, Reason: "ok"}, nil
}

// TestWaitingOnAPersonDoesNotRerunTheTestSuite: a worker that is only waiting
// should cost nothing, and tokens were only half of that. The item is
// re-drained every tick for as long as the review takes, and re-running the
// whole gate suite each time is how a factory with nothing to do keeps a
// machine busy — `go test` every fifteen seconds until somebody clicks
// approve. A gate that passed on this exact commit passed; the one whose
// answer can change is the human one, and it is a store read.
func TestWaitingOnAPersonDoesNotRerunTheTestSuite(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	approved := false
	suite := 0
	builder := &countingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{
			countingGate{name: "build-and-test", n: &suite},
			holdingGate{approved: &approved},
		}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-49"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-idle", "add a thing"))

	if item, err = e.loop.Run(ctx, item); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("expected waiting, got %s (%s)", item.State, item.Note)
	}
	if suite != 1 {
		t.Fatalf("the suite should have run once, ran %d times", suite)
	}
	if !slices.Contains(item.PassedGates, "build-and-test") {
		t.Fatalf("the satisfied gate was not carried: %+v", item.PassedGates)
	}

	// The person takes their time. The factory should be doing nothing.
	for tick := 1; tick <= 4; tick++ {
		loaded, lerr := q.Load(ctx, tn, "wi-idle")
		if lerr != nil {
			t.Fatal(lerr)
		}
		if item, err = e.loop.Run(ctx, loaded); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if item.State != queue.StateWaiting {
			t.Fatalf("tick %d: state %s", tick, item.State)
		}
		if suite != 1 {
			t.Fatalf("tick %d re-ran the suite while only a person was outstanding: %d runs", tick, suite)
		}
		if builder.n != 1 {
			t.Fatalf("tick %d rebuilt: %d builds", tick, builder.n)
		}
	}

	// The approval arrives and the carried verdict is good enough to land on:
	// it was reached on this exact commit and the commit has not moved.
	approved = true
	loaded, _ := q.Load(ctx, tn, "wi-idle")
	if item, err = e.loop.Run(ctx, loaded); err != nil {
		t.Fatalf("approved run: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("state %s (%s)", item.State, item.Note)
	}
	if suite != 1 || builder.n != 1 {
		t.Fatalf("landing re-did the work: %d suite runs, %d builds", suite, builder.n)
	}
}

// TestMovedHeadRetiresTheCarriedVerdicts: carrying a pass is sound only while
// the content stands. Replayed onto a new base, the change is a different
// question and every gate is asked again — otherwise the cheap path would
// land a commit nothing had judged.
func TestMovedHeadRetiresTheCarriedVerdicts(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	approved := false
	suite := 0
	e.loop.Builder = &countingBuilder{}
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{
			countingGate{name: "build-and-test", n: &suite},
			holdingGate{approved: &approved},
		}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-50"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-retire", "add a thing"))
	if _, err = e.loop.Run(ctx, item); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if suite != 1 {
		t.Fatalf("suite ran %d times", suite)
	}

	interlope(t, e.remote, "elsewhere.txt", "moved\n")

	loaded, _ := q.Load(ctx, tn, "wi-retire")
	if _, err = e.loop.Run(ctx, loaded); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if suite != 2 {
		t.Fatalf("replayed content must be judged again: %d suite runs", suite)
	}
}
