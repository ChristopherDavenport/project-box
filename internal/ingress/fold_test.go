package ingress

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// TestTheChainFoldSelects is the bug, reproduced the way it happens.
//
// An origination's chain carries the origination AND every authority it
// exercised — a preemption, a break-glass, and now the plan its decomposition
// produced. Three readers took the HEAD of that chain and unmarshalled it as
// an origination, which is correct only while nothing has ever happened to it.
//
// Two of them broke loudly and badly. A break-glass body carries `incident` as
// a string where the struct had a pointer, so the unmarshal failed: the router
// then refused every REDELIVERY of that key, and the pump's intake aborted its
// whole pass — for that tenant, on every tick, permanently.
//
// The third broke silently, and it is the one that says what the defect
// actually is. The room unmarshalled into a narrow anonymous struct, which
// succeeded happily, and the queue row came back with no route and no key and
// nothing anywhere saying why.
//
// So the type error in the first two was ACCIDENT rather than protection: it
// depended on one key in one body colliding with a field of a different type.
// This branch supplies the record that does not collide — a plan — and it
// unmarshals cleanly into a zeroed Ingress that every caller would treat as
// real. That is asserted below, because it is the failure the accident was
// hiding.
func TestTheChainFoldSelects(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIncident,
		Box: "shop", Repo: "app", Requester: "chris"}
	ing, opened, err := r.Open(ctx, route, Delivery{Route: "issues", Key: "k1", Payload: []byte("checkout is down")})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}

	// The act that displaces the origination from the head of its own chain.
	if err := BreakGlass(ctx, ing, incidentDecl(), s.Log(), "chris", "bypass the gate"); err != nil {
		t.Fatal(err)
	}
	// And a record whose body does NOT collide with the struct, which is what
	// makes this a fold problem rather than a lucky type error. A plan is one;
	// so is anything else a later writer puts on this chain.
	if _, err := s.Log().Append(ctx, evidence.Record{
		Tenant: tn2, Scope: Scope(ing.ID), Kind: "planned", Subject: "plan-abc",
		Actor: "boxd", Body: []byte(`{"ingress":"` + ing.ID + `","nodes":[{"id":"work","task":"restore"}]}`),
	}); err != nil {
		t.Fatal(err)
	}
	head, err := s.Log().Head(ctx, tn2, Scope(ing.ID))
	if err != nil {
		t.Fatal(err)
	}
	if head.Kind == RecordOpen {
		t.Fatal("the origination is still the head; this test no longer reproduces anything")
	}
	var asHead Ingress
	if err := json.Unmarshal(head.Body, &asHead); err != nil {
		t.Fatalf("the head no longer parses as an Ingress, which is the accident this test exists to stop relying on: %v", err)
	}
	if asHead.Box != "" || asHead.Requester != "" || asHead.Kind != "" {
		t.Fatalf("the head carries the origination after all: %+v", asHead)
	}

	// The fold reads the origination whole, past everything it authorized and
	// everything written about it.
	got, found, err := Load(ctx, s.Log(), tn2, ing.ID)
	if err != nil || !found {
		t.Fatalf("load: %v %v", err, found)
	}
	if got.Tenant != tn2 || got.Box != "shop" || got.Requester != "chris" || got.Kind != kind.SeedIncident {
		t.Fatalf("the fold read the wrong record: %+v", got)
	}
	if got.Route != "issues" || got.Key != "k1" {
		t.Fatalf("route and key lost: %+v", got)
	}

	// And the router agrees with the fold, because it IS the fold now.
	again, opened, err := r.Open(ctx, route, Delivery{Route: "issues", Key: "k1", Payload: []byte("checkout is down")})
	if err != nil {
		t.Fatalf("redelivery after a break-glass: %v", err)
	}
	if opened {
		t.Fatal("redelivery opened a second origination")
	}
	if again.Box != "shop" || again.Requester != "chris" {
		t.Fatalf("redelivery returned a hollow origination: %+v", again)
	}
}

// TestLoadReportsAbsenceRatherThanEmptiness. An id nothing was ever opened
// under is not an origination with no fields, and intake keys on the
// difference: absence is the only answer that may create work.
func TestLoadReportsAbsenceRatherThanEmptiness(t *testing.T) {
	s := openStore(t)
	got, found, err := Load(context.Background(), s.Log(), tn2, "ing-nothing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("an id nobody opened answered: %+v", got)
	}
}
