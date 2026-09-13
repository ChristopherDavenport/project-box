package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// sectioned is the loop's repository with two regions declared.
func sectioned(t *testing.T) *env {
	t.Helper()
	e := setup(t, []string{"git", "log", "-1"})
	e.loop.Repo.Sections = []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"api"}},
		{Name: "web", Purpose: "the room", Paths: []string{"web"}},
	}
	return e
}

func (e *env) submit(t *testing.T, key string, shape func(*queue.WorkItem)) queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	item := ItemFor(ing, "wi-"+key, "change "+key)
	if shape != nil {
		shape(&item)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, err = q.Submit(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func hold(t *testing.T, e *env, section, run string) forge.Lease {
	t.Helper()
	l, err := e.store.Leaser("runner-1").Acquire(context.Background(), forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef(section), Run: run, TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestDisjointSectionsRunAtOnce. A section is the unit two pieces of work are
// held apart by: with api held by another run, a unit expressed in web lands,
// and a unit expressed in api waits — naming the section and the holder,
// because "not my turn" is only useful if it says whose turn it is.
func TestDisjointSectionsRunAtOnce(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	hold(t, e, "api", "wi-elsewhere")

	web := e.submit(t, "web-1", func(w *queue.WorkItem) { w.Sections = []string{"web"} })
	web, err := e.loop.Run(ctx, web)
	if err != nil || web.State != queue.StateDone {
		t.Fatalf("a unit in a free section did not land: %v %+v", err, web)
	}

	api := e.submit(t, "api-1", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	api, err = e.loop.Run(ctx, api)
	if err != nil {
		t.Fatal(err)
	}
	if api.State != queue.StateWaiting {
		t.Fatalf("a unit in a held section should wait, got %s: %s", api.State, api.Note)
	}
	for _, want := range []string{"section api", "held by wi-elsewhere"} {
		if !strings.Contains(api.Note, want) {
			t.Errorf("the wait does not say %q: %s", want, api.Note)
		}
	}
}

// TestAnUnsectionedUnitIsTheWholeRepository. A unit that names no section
// leases every declared one, so it can never run beside a sectioned unit —
// in either order.
func TestAnUnsectionedUnitIsTheWholeRepository(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	hold(t, e, "web", "wi-elsewhere")

	whole := e.submit(t, "whole-1", nil)
	whole, err := e.loop.Run(ctx, whole)
	if err != nil {
		t.Fatal(err)
	}
	if whole.State != queue.StateWaiting || !strings.Contains(whole.Note, "section web") {
		t.Fatalf("the whole-repository unit should wait on web, got %s: %s", whole.State, whole.Note)
	}
	// And nothing is left held by the unit that waited: a partial hold would
	// block everyone else while it waits for the rest.
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef("api"), Run: "probe", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("api stayed held by a unit that waited: %v", err)
	}
}

// TestSectionLeasesAreReleasedAtSettle: the region is free again the moment
// the unit settles, landed or not.
func TestSectionLeasesAreReleasedAtSettle(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	item := e.submit(t, "api-2", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	if item, err := e.loop.Run(ctx, item); err != nil || item.State != queue.StateDone {
		t.Fatalf("run: %v %+v", err, item)
	}
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef("api"), Run: "probe", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("api still held after the unit settled: %v", err)
	}
}

// TestAnUndeclaredSectionParks. The plan and the repository disagree about
// where a unit works; running it unscoped would be the silent widening, so
// it parks with both sides of the disagreement named.
func TestAnUndeclaredSectionParks(t *testing.T) {
	e := sectioned(t)
	item := e.submit(t, "db-1", func(w *queue.WorkItem) { w.Sections = []string{"db"} })
	item, err := e.loop.Run(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("an undeclared section should park, got %s", item.State)
	}
	for _, want := range []string{`"db"`, "api", "web"} {
		if !strings.Contains(item.Note, want) {
			t.Errorf("the park does not name %s: %s", want, item.Note)
		}
	}
}

// TestARepositoryWithNoSectionsLeasesNothing: the shape every existing box
// has, unchanged — one region, itself, serialized by the land window alone.
func TestARepositoryWithNoSectionsLeasesNothing(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	item := e.submit(t, "plain-1", nil)
	if item, err := e.loop.Run(context.Background(), item); err != nil || item.State != queue.StateDone {
		t.Fatalf("run: %v %+v", err, item)
	}
}

// TestOneStartPerRunNotPerClaim is issue #45 by name: a claim that reaches
// leaseSections and finds its section held goes to Wait without ever
// touching the builder, and must not be charged a start it never used. The
// same item claimed again once the section frees runs to completion and is
// charged exactly one — not two for having been claimed twice, and not zero
// for having waited once.
func TestOneStartPerRunNotPerClaim(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	meter := &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1", Budget: capacity.Budget{MaxConcurrent: 2}}
	e.loop.Meter = meter

	holder := hold(t, e, "api", "wi-elsewhere")
	item := e.submit(t, "api-1", func(w *queue.WorkItem) { w.Sections = []string{"api"} })

	release, err := meter.Acquire(ctx, item.ID)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	item, err = e.loop.Run(ctx, item)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("want waiting on the held section, got %s: %s", item.State, item.Note)
	}
	if got := meter.Report().Started; got != 0 {
		t.Fatalf("a wait must not charge a start, got %d", got)
	}

	if err := e.store.Leaser("runner-1").Release(ctx, holder); err != nil {
		t.Fatal(err)
	}

	release, err = meter.Acquire(ctx, item.ID)
	if err != nil {
		t.Fatalf("second acquire, once the section frees: %v", err)
	}
	item, err = e.loop.Run(ctx, item)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("want done once the section frees, got %s: %s", item.State, item.Note)
	}
	if got := meter.Report().Started; got != 1 {
		t.Fatalf("want exactly one start across the wait and the run that finished it, got %d", got)
	}
}

// TestAYieldedUnitNeverTouchesALease is issue #109's pump-side fairness rule,
// tested from the loop's half: a claim carrying YieldTo goes straight to
// waiting on the reservation, naming who it is next in line behind, and never
// even attempts a section lease — Inspect must find every declared section
// unheld afterward.
func TestAYieldedUnitNeverTouchesALease(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	item := e.submit(t, "api-3", func(w *queue.WorkItem) {
		w.Sections = []string{"api"}
		w.YieldTo = "wi-whole-1"
		w.SectionWaits = 2
	})
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a yielded unit should wait, got %s: %s", item.State, item.Note)
	}
	for _, want := range []string{"reserved for wi-whole-1", "next in line", "waited 2 turns"} {
		if !strings.Contains(item.Note, want) {
			t.Errorf("the wait does not say %q: %s", want, item.Note)
		}
	}
	for _, name := range []string{"api", "web"} {
		if _, ok, err := e.store.Leaser("runner-1").Inspect(ctx, tn, e.repo.Name, SectionRef(name)); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Fatalf("section %s was held by a unit that yielded without trying", name)
		}
	}
}

// TestASelfHeldSectionIsTakenOverNotWaitedOn is issue #143 by name: a claim
// whose own run already holds the section lease — a rebase, re-gate, or push
// still finishing, or a stale grant surviving a lost release — must never
// wait on itself. leaseSections takes the grant over instead of reporting
// busy, and proceeds through to a real run.
func TestASelfHeldSectionIsTakenOverNotWaitedOn(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	item := e.submit(t, "api-5", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	hold(t, e, "api", item.ID) // this run's own grant, surviving a lost release

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a self-held section should be taken over and the run completed, got %s: %s", item.State, item.Note)
	}
	if strings.Contains(item.Note, item.ID) {
		t.Fatalf("the note names the item itself as its own blocker: %s", item.Note)
	}
	// The section is free again, taken over under this run's own token and
	// released at settle like any other hold.
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef("api"), Run: "probe", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("api stayed held after the self-takeover settled: %v", err)
	}
}

// TestSectionWaitsCountsAndResets: a claim deferred by a held section counts
// the deferral, and a claim that gets through zeroes it — the signal the
// pump reads to name an oldest waiter must not carry stale history once a
// unit has actually run.
func TestSectionWaitsCountsAndResets(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	holder := hold(t, e, "api", "wi-elsewhere")

	item := e.submit(t, "api-4", func(w *queue.WorkItem) {
		w.Sections = []string{"api"}
		w.SectionWaits = 2
	})
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("want waiting on the held section, got %s: %s", item.State, item.Note)
	}
	if item.SectionWaits != 3 {
		t.Fatalf("a deferred claim should count the wait, got SectionWaits=%d", item.SectionWaits)
	}
	if !strings.Contains(item.Note, "waited 2 turns") {
		t.Errorf("the wait note does not carry the prior count: %s", item.Note)
	}

	if err := e.store.Leaser("runner-1").Release(ctx, holder); err != nil {
		t.Fatal(err)
	}
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("want done once the section frees, got %s: %s", item.State, item.Note)
	}
	if item.SectionWaits != 0 {
		t.Fatalf("a claim that got through should reset the count, got SectionWaits=%d", item.SectionWaits)
	}
}

// TestDisjointSectionsEachChargeTheirOwnStart: two units in disjoint declared
// sections of the same repo, run without contention, produce exactly two
// starts — the count is additive across real runs, not shared by the repo.
func TestDisjointSectionsEachChargeTheirOwnStart(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	meter := &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1", Budget: capacity.Budget{MaxConcurrent: 2}}
	e.loop.Meter = meter

	web := e.submit(t, "web-2", func(w *queue.WorkItem) { w.Sections = []string{"web"} })
	release, err := meter.Acquire(ctx, web.ID)
	if err != nil {
		t.Fatal(err)
	}
	web, err = e.loop.Run(ctx, web)
	release()
	if err != nil || web.State != queue.StateDone {
		t.Fatalf("web run: %v %+v", err, web)
	}

	api := e.submit(t, "api-2", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	release, err = meter.Acquire(ctx, api.ID)
	if err != nil {
		t.Fatal(err)
	}
	api, err = e.loop.Run(ctx, api)
	release()
	if err != nil || api.State != queue.StateDone {
		t.Fatalf("api run: %v %+v", err, api)
	}

	if got := meter.Report().Started; got != 2 {
		t.Fatalf("two uncontended runs should charge two starts, got %d", got)
	}
}

// TestARestoredMeterAgreesWithTheLiveStartCount closes the loop on Restore's
// `case "spend": m.started++`: it was already correct, once "spend" records
// exist only for real starts. A fresh meter over the same store, after a mix
// of a wait-then-run and an uncontended run, must read back the same started
// count the live meter holds in memory.
func TestARestoredMeterAgreesWithTheLiveStartCount(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	budget := capacity.Budget{MaxConcurrent: 2, Window: time.Hour}
	meter := &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1", Budget: budget}
	e.loop.Meter = meter

	holder := hold(t, e, "api", "wi-elsewhere")
	waited := e.submit(t, "api-3", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	release, err := meter.Acquire(ctx, waited.ID)
	if err != nil {
		t.Fatal(err)
	}
	waited, err = e.loop.Run(ctx, waited)
	release()
	if err != nil || waited.State != queue.StateWaiting {
		t.Fatalf("expected a wait: %v %+v", err, waited)
	}
	if err := e.store.Leaser("runner-1").Release(ctx, holder); err != nil {
		t.Fatal(err)
	}
	release, err = meter.Acquire(ctx, waited.ID)
	if err != nil {
		t.Fatal(err)
	}
	waited, err = e.loop.Run(ctx, waited)
	release()
	if err != nil || waited.State != queue.StateDone {
		t.Fatalf("expected the wait to complete once the section freed: %v %+v", err, waited)
	}

	web := e.submit(t, "web-3", func(w *queue.WorkItem) { w.Sections = []string{"web"} })
	release, err = meter.Acquire(ctx, web.ID)
	if err != nil {
		t.Fatal(err)
	}
	web, err = e.loop.Run(ctx, web)
	release()
	if err != nil || web.State != queue.StateDone {
		t.Fatalf("expected the disjoint run to complete without contention: %v %+v", err, web)
	}

	live := meter.Report().Started
	if live != 2 {
		t.Fatalf("want two live starts (one wait-then-run, one uncontended), got %d", live)
	}

	fresh := &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1", Budget: budget}
	if err := fresh.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fresh.Report().Started; got != live {
		t.Fatalf("a restored meter disagreed with the live one: restored=%d live=%d", got, live)
	}
}
