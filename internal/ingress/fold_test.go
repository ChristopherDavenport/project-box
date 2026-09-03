package ingress

import (
	"context"
	"encoding/json"
	"testing"
)

// TestTheChainFoldSelects is the bug, reproduced the way it happens.
//
// An origination's chain carries the origination AND every authority it
// exercised — a preemption, a break-glass. Three readers took the HEAD of that
// chain and unmarshalled it as an origination, which is only correct while
// nothing has ever been authorized.
//
// Two of them break loudly and badly. A break-glass body carries `incident` as
// a STRING where the struct has a pointer, so the unmarshal fails outright: the
// router then refuses every redelivery of that key, and the pump's intake
// aborts its whole pass — for that tenant, on every tick, permanently.
//
// The third breaks silently, and it is the one that says what the real defect
// is. The room unmarshals into a narrow anonymous struct, which succeeds
// happily, and the queue row comes back with no route and no key and nothing
// anywhere saying why.
//
// So the type error in the first two is ACCIDENT, not protection. It depends on
// one key in one body colliding with a field of a different type; any record
// whose body does not collide unmarshals cleanly into a zeroed Ingress and is
// treated as real. The fix is to select on the record kind rather than to hope
// the shapes keep disagreeing.
func TestTheChainFoldSelects(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: KindIncident,
		Box: "shop", Repo: "app", Requester: "chris"}
	ing, opened, err := r.Open(ctx, route, Delivery{Route: "issues", Key: "k1", Payload: []byte("checkout is down")})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}
	ing.Incident = &Incident{}

	// The act that displaces the origination from the head of its own chain.
	if err := BreakGlass(ctx, ing, s.Log(), "chris", "bypass the gate"); err != nil {
		t.Fatal(err)
	}
	head, err := s.Log().Head(ctx, tn2, Scope(ing.ID))
	if err != nil {
		t.Fatal(err)
	}
	if head.Kind == RecordOpen {
		t.Fatal("the origination is still the head; this test no longer reproduces anything")
	}
	// What the room's reader does with that head: succeeds, and loses the row's
	// whole trail back to the occurrence.
	var narrow struct {
		Route string `json:"route"`
		Key   string `json:"key"`
	}
	if err := json.Unmarshal(head.Body, &narrow); err != nil {
		t.Fatalf("the narrow read failed, which is not the bug: %v", err)
	}
	if narrow.Route != "" || narrow.Key != "" {
		t.Fatalf("the head carries a route after all: %+v", narrow)
	}

	// The fold reads the origination whole, past anything it authorized.
	got, found, err := Load(ctx, s.Log(), tn2, ing.ID)
	if err != nil || !found {
		t.Fatalf("load: %v %v", err, found)
	}
	if got.Tenant != tn2 || got.Box != "shop" || got.Requester != "chris" || got.Kind != KindIncident {
		t.Fatalf("the fold read the wrong record: %+v", got)
	}
	if got.Route != "issues" || got.Key != "k1" {
		t.Fatalf("route and key lost: %+v", got)
	}

	// And the router agrees with the fold, because it IS the fold now. Before
	// this, a redelivery after a break-glass returned an unmarshal error —
	// which is to say a repeated webhook became a hard failure because of
	// something that had happened to the ingress in between.
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
