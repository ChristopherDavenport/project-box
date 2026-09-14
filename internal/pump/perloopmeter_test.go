package pump

import (
	"context"
	"os"
	"path/filepath"
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
)

// twoLoopWorld is [TestTwoReposOneTenantSideBySide]'s rig, factored out so
// issue #217's tests can wire a distinct *capacity.Meter onto each of the two
// loops instead of the one meter that test shares between them.
type twoLoopWorld struct {
	store          *sqlite.Store
	pump           *Pump
	q              *queue.Queue
	router         *ingress.StoreRouter
	apiRem, webRem string
	meterA, meterB *capacity.Meter
}

func newTwoLoopWorld(t *testing.T, budgetA, budgetB capacity.Budget) *twoLoopWorld {
	t.Helper()
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
	meterA := &capacity.Meter{Store: s, Tenant: tn, Actor: "d", Budget: budgetA}
	meterB := &capacity.Meter{Store: s, Tenant: tn, Actor: "d", Budget: budgetB}
	mkLoop := func(name, remote string, meter *capacity.Meter) *loop.Loop {
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
			TTL:   time.Hour,
			Meter: meter,
		}
	}
	q := &queue.Queue{Store: s, Runner: "d", TTL: time.Hour}
	p := &Pump{
		Store: s, Tenant: tn, Runner: "d", Queue: q,
		Kinds: kind.Seeded("shop"), Decomposers: plan.Default(),
		Plans: &plan.Store{Log: s.Log(), Actor: "d"},
		Meter: meterA, // fallback only; both loops below carry their own
		Loops: map[string]*loop.Loop{
			"shop/api": mkLoop("api", apiRem, meterA),
			"shop/web": mkLoop("web", webRem, meterB),
		},
	}
	return &twoLoopWorld{
		store: s, pump: p, q: q,
		router: &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "d"},
		apiRem: apiRem, webRem: webRem,
		meterA: meterA, meterB: meterB,
	}
}

// submit opens an ingress for repo and submits a work item under it directly,
// bypassing intake's own decomposition — the same shortcut [world.submitItem]
// takes, mirrored here for the two-loop rig.
func (tw *twoLoopWorld) submit(t *testing.T, route, repo, key, task string) queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	ing, _, err := tw.router.Open(ctx, ingress.Route{
		Name: route, Kind: ingress.RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: repo, Requester: "chris",
	}, ingress.Delivery{Key: key, Payload: []byte(task)})
	if err != nil {
		t.Fatal(err)
	}
	item, err := tw.q.Submit(ctx, loop.ItemFor(ing, "wi-"+key, task))
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestDrainSkipsOnlyTheExhaustedLoopsMeter is issue #217's own property: two
// loops, two distinct meters, api's spent to its one seat before Drain ever
// runs. The old break on a busy or exhausted meter would have stranded web's
// candidate behind api's the moment api's Acquire refused, even though the
// two loops' accounts have nothing to do with one another. Now api's refusal
// only marks api's own meter refused, and web's candidate — paced by its own,
// still-open meter — dispatches and lands in the very same Drain pass.
func TestDrainSkipsOnlyTheExhaustedLoopsMeter(t *testing.T) {
	tw := newTwoLoopWorld(t, capacity.Budget{MaxConcurrent: 1}, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()

	// Stands in for a build already in flight on api's account, holding its
	// only seat — exhausted before Drain is ever asked to acquire on it.
	releaseA, err := tw.meterA.Acquire(ctx, "hog")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()

	apiItem := tw.submit(t, "shop-api", "api", "api-1", "fix module alpha")
	webItem := tw.submit(t, "shop-web", "web", "web-1", "fix module beta")

	if _, err := tw.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !tw.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched run")
	}

	if got, err := tw.q.Load(ctx, tn, apiItem.ID); err != nil || got.State != queue.StatePending {
		t.Fatalf("api's item should still be waiting on its own exhausted meter: %+v %v", got, err)
	}
	if got, err := tw.q.Load(ctx, tn, webItem.ID); err != nil || got.State != queue.StateDone {
		t.Fatalf("web's item should have dispatched in the same pass despite api's meter being exhausted: %+v %v", got, err)
	}
}

// TestNilLoopMeterPacesOnThePumpsMeter is the fallback half of issue #217:
// a loop with no Meter of its own is not unpaced, it paces on [Pump.Meter],
// exactly as every loop did before this issue. With MaxConcurrent 1 and one
// build already occupying that one seat, a second candidate through the same
// nil-Meter loop still waits for the next tick.
func TestNilLoopMeterPacesOnThePumpsMeter(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	w.pump.Loops["shop"].Meter = nil
	ctx := context.Background()

	blocker := newBlockingBuilder(blockAll)
	w.pump.Loops["shop"].Builder = blocker

	a := w.submitItem(t, "a-1", "fix module a", nil)
	b := w.submitItem(t, "b-1", "fix module b", nil)

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	waitBuildEntered(t, blocker, 1)

	if got, err := w.q.Load(ctx, tn, a.ID); err != nil || got.State != queue.StateClaimed {
		t.Fatalf("a should be claimed and building: %+v %v", got, err)
	}
	if got, err := w.q.Load(ctx, tn, b.ID); err != nil || got.State != queue.StatePending {
		t.Fatalf("with lp.Meter nil, b should still have paced on the pump's own meter: %+v %v", got, err)
	}

	close(blocker.release)
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the dispatched build")
	}
}
