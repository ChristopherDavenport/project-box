package plan

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

func store(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestAPlanRidesTheOriginationsOwnChain, which is the durability decision:
// bounded per-entity, the same class as workitem/<id>, and therefore already
// out of the compaction sweep by name rather than by a new tier with its own
// reaper.
func TestAPlanRidesTheOriginationsOwnChain(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ps := &Store{Log: s.Log(), Actor: "boxd"}
	ing := issue("ing-1")

	// The origination itself, and an act it authorized, sharing the chain.
	openOn(t, s, ing)
	p, err := Make(ctx, Default(), ing, seed(t, kind.SeedIssue))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := ps.Record(ctx, ing, p)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.At.IsZero() {
		t.Fatal("the recorded plan has no time")
	}
	// The origination still reads back whole, because the ingress fold
	// selects: a plan on the chain is not an origination and must not be read
	// as one.
	got, ok, err := ingress.Load(ctx, s.Log(), tn, ing.ID)
	if err != nil || !ok {
		t.Fatalf("load: %v %v", err, ok)
	}
	if got.Box != "shop" || got.Requester != "chris" {
		t.Fatalf("the plan record was folded as an origination: %+v", got)
	}

	back, found, err := ps.Latest(ctx, tn, ing.ID)
	if err != nil || !found {
		t.Fatalf("latest: %v %v", err, found)
	}
	if back.Ref != p.Ref || back.FanOut() != 1 {
		t.Fatalf("round trip: %+v", back)
	}
}

// TestAStandingOriginationCarriesAPlanPerWave. A reconciler decomposes
// repeatedly, and the honest shape is one plan per wave with each item naming
// the wave that called for it — never one plan amended in place, which is the
// hard half this does not claim to answer.
func TestAStandingOriginationCarriesAPlanPerWave(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ps := &Store{Log: s.Log(), Actor: "boxd"}
	ing := issue("ing-c")
	openOn(t, s, ing)

	for i, nodes := range [][]Node{
		{{ID: "r1", Task: "bump r1"}},
		{{ID: "r2", Task: "bump r2"}},
	} {
		p := Plan{Ingress: ing.ID, Kind: "campaign", Decomposer: "campaign", Replayable: true, Nodes: nodes}
		if err := p.Seal(); err != nil {
			t.Fatal(err)
		}
		if _, err := ps.Record(ctx, ing, p); err != nil {
			t.Fatalf("wave %d: %v", i, err)
		}
	}
	all, err := ps.All(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("waves: %d", len(all))
	}
	if all[0].Ref == all[1].Ref {
		t.Fatal("two different waves share a ref")
	}
	latest, _, err := ps.Latest(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Ref != all[1].Ref {
		t.Fatal("latest is not the newest wave")
	}
}

func TestAnUnsealedPlanIsNotRecordable(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ps := &Store{Log: s.Log(), Actor: "boxd"}
	ing := issue("ing-1")
	if _, err := ps.Record(ctx, ing, Plan{Ingress: ing.ID, Nodes: []Node{{ID: "a", Task: "t"}}}); err == nil {
		t.Fatal("recorded a plan with no content address for its items to carry")
	}
	sealed := Plan{Ingress: "ing-other", Replayable: true, Nodes: []Node{{ID: "a", Task: "t"}}}
	if err := sealed.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Record(ctx, ing, sealed); err == nil {
		t.Fatal("recorded a plan against an origination it does not name")
	}
}

// TestByRefFindsAPlanByItsContentAddress. A ref is a sha256-derived content
// address, so ByRef walks every scope naming it rather than one ingress's
// own chain — the primitive a diff view needs when it is handed two refs
// and no ingress id at all.
func TestByRefFindsAPlanByItsContentAddress(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ps := &Store{Log: s.Log(), Actor: "boxd"}
	ing := issue("ing-1")
	openOn(t, s, ing)
	p, err := Make(ctx, Default(), ing, seed(t, kind.SeedIssue))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Record(ctx, ing, p); err != nil {
		t.Fatal(err)
	}

	got, ok, err := ps.ByRef(ctx, tn, p.Ref)
	if err != nil || !ok {
		t.Fatalf("ByRef: %v %v", err, ok)
	}
	if got.Ref != p.Ref || got.Ingress != ing.ID || got.At.IsZero() {
		t.Fatalf("round trip: %+v", got)
	}

	_, ok, err = ps.ByRef(ctx, tn, "plan-nonesuch")
	if err != nil || ok {
		t.Fatalf("an unknown ref should be a plain miss: %v %v", ok, err)
	}
}

// TestADeferralFoldsToDeferredAndStaysUnblocked. A deferral is the meter's
// own pacing, not an exception: Standing must still surface it so a room can
// explain why nothing is happening, but Blocked must stay false so intake
// keeps offering the origination to the planner every tick the meter allows.
func TestADeferralFoldsToDeferredAndStaysUnblocked(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ps := &Store{Log: s.Log(), Actor: "boxd"}
	ing := issue("ing-1")
	openOn(t, s, ing)

	if err := ps.Defer(ctx, ing, "capacity: the account's five_hour window is rejected"); err != nil {
		t.Fatal(err)
	}
	st, err := ps.Standing(ctx, tn, ing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "deferred" || st.Reason == "" {
		t.Fatalf("standing after a deferral: %+v", st)
	}
	if st.Blocked() {
		t.Fatal("a deferred origination must stay unblocked — intake retries it, the meter is what holds it")
	}

	// And it must not have landed in the exception queue: a deferral is not
	// an exception, so nothing lists it and nobody needs to retry or resolve
	// it by hand.
	for rec, err := range s.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == ing.ID {
			t.Fatalf("a deferral wrote an index/exceptions record: %+v", rec)
		}
	}
}

func openOn(t *testing.T, s *sqlite.Store, ing ingress.Ingress) {
	t.Helper()
	body, err := json.Marshal(ing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log().Append(context.Background(), evidence.Record{
		Tenant: ing.Tenant, Scope: ingress.Scope(ing.ID), Kind: ingress.RecordOpen,
		Subject: "delivery/" + ing.ID, Actor: ing.Requester, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}
