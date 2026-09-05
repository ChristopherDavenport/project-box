package pump

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

type fixer struct{}

func (fixer) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	os.WriteFile(filepath.Join(ws.Dir, "ok.txt"), []byte("green\n"), 0o644)
	os.WriteFile(filepath.Join(ws.Dir, strings.Fields(task)[len(strings.Fields(task))-1]+".txt"), []byte(task+"\n"), 0o644)
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", task); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

type world struct {
	store   *sqlite.Store
	advance func(time.Duration)
	remote  string
	router  *ingress.StoreRouter
	pump    *Pump
	q       *queue.Queue
	meter   *capacity.Meter
}

func setup(t *testing.T, budget capacity.Budget) *world {
	t.Helper()
	var mu sync.Mutex
	offset := time.Duration(0)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return time.Now().Add(offset) }
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	remote := filepath.Join(t.TempDir(), "app.git")
	mustSh(t, "", "git", "init", "--bare", remote)
	mustSh(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), "seed")
	mustSh(t, "", "git", "clone", remote, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("app\n"), 0o644)
	mustSh(t, seed, "git", "add", ".")
	mustSh(t, seed, "git", "commit", "-m", "seed")
	mustSh(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")

	repo := forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
	m, err := gitforge.NewMirror(t.TempDir(), tn, s.Leaser("pump-runner"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Fetch(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: s, Runner: "pump-runner", TTL: time.Hour}
	// One meter, shared by the pump (which acquires the concurrency slot at
	// claim) and the loop (which starts it at build) — the same account, seen
	// from both sides of the seam this issue is about.
	meter := &capacity.Meter{Store: s, Tenant: tn, Actor: "pump-runner", Budget: budget, Now: now}
	return &world{
		store: s, remote: remote, q: q, meter: meter,
		advance: func(d time.Duration) { mu.Lock(); defer mu.Unlock(); offset += d },
		router:  &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "pump-runner"},
		pump: &Pump{
			Store: s, Tenant: tn, Runner: "pump-runner", Queue: q,
			Kinds: kind.Seeded("shop"), Decomposers: plan.Default(),
			Plans: &plan.Store{Log: s.Log(), Actor: "pump-runner"},
			Meter: meter,
			Loops: map[string]*loop.Loop{"shop": {
				Store: s, Runner: "pump-runner", Mirror: m, Repo: repo, Branch: "main",
				Builder: fixer{}, Lane: forge.LaneOptimistic,
				Gates: func(ws forge.Workspace) []forge.Gate {
					return []forge.Gate{gate.Command{Label: "post-land", Dir: ws.Dir, Commands: [][]string{{"test", "-f", "ok.txt"}}}}
				},
				TTL:   time.Hour,
				Meter: meter,
			}},
		},
	}
}

func (w *world) open(t *testing.T, key, task string) {
	t.Helper()
	_, _, err := w.router.Open(context.Background(), ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte(task)})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStepUpIngressAndStepBack is the contract by name: three issues arrive,
// nobody submits anything, nobody runs anything — the daemon's ticks turn
// ingress into landed commits.
func TestStepUpIngressAndStepBack(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	w.open(t, "issue-1", "fix module alpha")
	w.open(t, "issue-2", "fix module beta")
	w.open(t, "issue-3", "fix module gamma")

	d := &Daemon{Every: 30 * time.Millisecond, Pump: w.pump}
	go d.Run(ctx)

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for _, key := range []string{"issue-1", "issue-2", "issue-3"} {
			ing, _, _ := w.router.Open(ctx, ingress.Route{
				Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
			}, ingress.Delivery{Key: key})
			item, err := w.q.Load(ctx, tn, "wi-"+ing.ID)
			if err == nil && item.State == queue.StateDone {
				done++
			}
		}
		if done == 3 {
			cancel()
			rctx := context.Background() // assertions outlive the daemon's context
			// The remote really carries all three, landed by nobody's hand.
			n := strings.TrimSpace(mustSh(t, w.remote, "git", "rev-list", "--count", "refs/heads/main"))
			if n != "4" { // seed + three
				t.Fatalf("main has %s commits, want 4", n)
			}
			// And the items carry their requester: automation on behalf of
			// the person who opened the issue.
			for rec, err := range w.store.Log().Since(rctx, tn, "index/pending", 1) {
				if err != nil {
					t.Fatal(err)
				}
				item, _ := w.q.Load(rctx, tn, rec.Subject)
				if item.Requester != "chris" {
					t.Fatalf("item lost its requester: %+v", item)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the factory did not build itself in time")
}

// TestBudgetPacesWithoutLosingWork: two starts per window; the third item
// WAITS — recorded, not dropped — and completes when the window turns.
func TestBudgetPacesWithoutLosingWork(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1, MaxPerWindow: 2, Window: time.Hour})
	ctx := context.Background()

	w.open(t, "issue-1", "fix module one")
	w.open(t, "issue-2", "fix module two")
	w.open(t, "issue-3", "fix module three")

	for range 6 { // several ticks inside one window
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	states := map[queue.State]int{}
	for rec, err := range w.store.Log().Since(ctx, tn, "index/pending", 1) {
		if err != nil {
			t.Fatal(err)
		}
		item, err := w.q.Load(ctx, tn, rec.Subject)
		if err != nil {
			t.Fatal(err)
		}
		states[item.State]++
	}
	if states[queue.StateDone] != 2 || states[queue.StatePending] != 1 {
		t.Fatalf("budget did not pace: %v", states)
	}
	// The refusal is on the record.
	refused := false
	for rec, err := range w.store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "budget-refused" {
			refused = true
		}
	}
	if !refused {
		t.Fatal("an exhausted budget left no record")
	}
	// The window turns; the third completes. Nothing was lost.
	w.advance(2 * time.Hour)
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	states = map[queue.State]int{}
	for rec, _ := range w.store.Log().Since(ctx, tn, "index/pending", 1) {
		item, _ := w.q.Load(ctx, tn, rec.Subject)
		states[item.State]++
		_ = rec
	}
	if states[queue.StateDone] != 3 {
		t.Fatalf("the waiting item did not complete after the window: %v", states)
	}
}

// TestStopHaltsTheDaemonsClaims: the same stop button, the same bound.
func TestStopHaltsTheDaemonsClaims(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx := context.Background()
	if err := w.q.Stop(ctx, tn, "chris", "hold everything"); err != nil {
		t.Fatal(err)
	}
	w.open(t, "issue-1", "fix module alpha")
	for range 3 {
		if _, err := w.pump.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for rec, _ := range w.store.Log().Since(ctx, tn, "index/pending", 1) {
		item, _ := w.q.Load(ctx, tn, rec.Subject)
		if item.State != queue.StatePending {
			t.Fatalf("stopped pump still ran work: %+v", item)
		}
		_ = rec
	}
	if err := w.q.Resume(ctx, tn, "chris", "carry on"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for rec, _ := range w.store.Log().Since(ctx, tn, "index/pending", 1) {
		item, _ := w.q.Load(ctx, tn, rec.Subject)
		if item.State != queue.StateDone {
			t.Fatalf("resumed pump did not finish: %+v", item)
		}
		_ = rec
	}
}

func sh(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustSh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := sh(dir, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return strings.TrimSpace(out)
}

// TestTwoReposOneTenantSideBySide is the confirmation by name: two
// repositories, one tenant, one box, one daemon — simultaneously in flight,
// because their leases key on (tenant, repo, ref) and contend for nothing.
func TestTwoReposOneTenantSideBySide(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mkRemote := func(name string) string {
		remote := filepath.Join(t.TempDir(), name+".git")
		mustSh(t, "", "git", "init", "--bare", remote)
		mustSh(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
		seed := filepath.Join(t.TempDir(), name+"-seed")
		mustSh(t, "", "git", "clone", remote, seed)
		os.WriteFile(filepath.Join(seed, "README.md"), []byte(name+"\n"), 0o644)
		mustSh(t, seed, "git", "add", ".")
		mustSh(t, seed, "git", "commit", "-m", "seed")
		mustSh(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")
		return remote
	}
	apiRem, webRem := mkRemote("api"), mkRemote("web")
	m, err := gitforge.NewMirror(t.TempDir(), tn, s.Leaser("d"))
	if err != nil {
		t.Fatal(err)
	}
	mkLoop := func(name, remote string) *loop.Loop {
		repo := forge.Repo{Name: name, Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
		if err := m.Fetch(context.Background(), repo); err != nil {
			t.Fatal(err)
		}
		return &loop.Loop{
			Store: s, Runner: "d", Mirror: m, Repo: repo, Branch: "main",
			Builder: fixer{}, Lane: forge.LaneOptimistic,
			Gates: func(ws forge.Workspace) []forge.Gate {
				return []forge.Gate{gate.Command{Label: "post-land", Dir: ws.Dir, Commands: [][]string{{"test", "-f", "ok.txt"}}}}
			},
			TTL: time.Hour,
		}
	}
	q := &queue.Queue{Store: s, Runner: "d", TTL: time.Hour}
	p := &Pump{
		Store: s, Tenant: tn, Runner: "d", Queue: q,
		Kinds: kind.Seeded("shop"), Decomposers: plan.Default(),
		Plans: &plan.Store{Log: s.Log(), Actor: "d"},
		Meter: &capacity.Meter{Store: s, Tenant: tn, Actor: "d", Budget: capacity.Budget{MaxConcurrent: 2}},
		Loops: map[string]*loop.Loop{
			"shop/api": mkLoop("api", apiRem),
			"shop/web": mkLoop("web", webRem),
		},
	}
	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "d"}
	open := func(route, repo, key, task string) {
		if _, _, err := router.Open(context.Background(), ingress.Route{
			Name: route, Kind: ingress.RouteIssue, Opens: kind.SeedIssue,
			Box: "shop", Repo: repo, Requester: "chris",
		}, ingress.Delivery{Key: key, Payload: []byte(task)}); err != nil {
			t.Fatal(err)
		}
	}
	open("shop-api", "api", "api-1", "fix module alpha")
	open("shop-web", "web", "web-1", "fix module beta")

	// One tick: both intaken; with MaxConcurrent 2, both run in the same
	// drain — genuinely side by side, distinct leases, distinct workspaces.
	ctx := context.Background()
	for range 4 {
		if _, err := p.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for name, remote := range map[string]string{"api": apiRem, "web": webRem} {
		n := strings.TrimSpace(mustSh(t, remote, "git", "rev-list", "--count", "refs/heads/main"))
		if n != "2" {
			t.Fatalf("%s advanced by %s commits, want seed+1", name, n)
		}
	}
	// And every item found its own executor: nothing parked, nothing lost.
	for rec, err := range s.Log().Since(ctx, tn, "index/pending", 1) {
		if err != nil {
			t.Fatal(err)
		}
		item, err := q.Load(ctx, tn, rec.Subject)
		if err != nil || item.State != queue.StateDone {
			t.Fatalf("item %s: %+v %v", rec.Subject, item, err)
		}
		if item.Repo != "api" && item.Repo != "web" {
			t.Fatalf("item lost its target repo: %+v", item)
		}
	}
}

// halfThenWhole hands off on its first session of an item and finishes on the
// next — a builder standing in for an agent that ran out of room.
type halfThenWhole struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (h *halfThenWhole) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	h.mu.Lock()
	first := !h.seen[ws.Run]
	h.seen[ws.Run] = true
	h.mu.Unlock()
	if first {
		os.WriteFile(filepath.Join(ws.Dir, "half.txt"), []byte(task+"\n"), 0o644)
		if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
			return "", err
		}
		if _, err := sh(ws.Dir, "git", "commit", "-m", "half of "+task); err != nil {
			return "", err
		}
		out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		return "", &loop.Handoff{Seat: "mason", Reason: "out of room",
			Commit: forge.Commit(strings.TrimSpace(out))}
	}
	// The second session finds the first's work already here, or says so.
	if _, err := os.Stat(filepath.Join(ws.Dir, "half.txt")); err != nil {
		return "", fmt.Errorf("the predecessor's work is not in this workspace")
	}
	return fixer{}.Build(context.Background(), ws, task)
}

// TestAHandedOffItemIsRedispatched is the re-dispatch half of issue #12, at
// the level that actually decides it. The loop can end a session however it
// likes; whether a fresh one ever happens is the drain's selection, and a
// state the drain does not select is a state that means "abandoned quietly".
//
// The same executor picks it up, which is what "the same seat" amounts to
// here: the pump routes by box and repo, and a repo has one builder.
func TestAHandedOffItemIsRedispatched(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()
	for _, lp := range w.pump.Loops {
		lp.Builder = &halfThenWhole{seen: map[string]bool{}}
	}
	w.open(t, "issue-1", "fix module alpha")

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var id string
	for rec, err := range w.store.Log().Since(ctx, tn, "index/pending", 1) {
		if err != nil {
			t.Fatal(err)
		}
		id = rec.Subject
	}
	item, err := w.q.Load(ctx, tn, id)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateHandedOff || item.Hops != 1 {
		t.Fatalf("first tick: %+v", item)
	}

	// The next tick is the re-dispatch. Nobody asked for it.
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if item, err = w.q.Load(ctx, tn, id); err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a handed-off item was never picked up again: %+v", item)
	}
	log := mustSh(t, w.remote, "git", "log", "--format=%s", "refs/heads/main")
	for _, want := range []string{"half of fix module alpha", "fix module alpha"} {
		if !strings.Contains(log, want) {
			t.Fatalf("the landed history lost %q:\n%s", want, log)
		}
	}
}
