package ingress

import (
	"context"
	"slices"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestOpenCarriesTheDeliveryTitle. The control room reads a unit's name off
// [Ingress.Title], and until a delivery could carry one that field was always
// empty — a route's source has to be able to hand over the human-readable
// title it already has, rather than that title being invented downstream.
func TestOpenCarriesTheDeliveryTitle(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: "chris"}

	ing, opened, err := r.Open(ctx, route, Delivery{
		Route: "issues", Key: "k-title", Payload: []byte("body text"), Title: "checkout is down",
	})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}
	if ing.Title != "checkout is down" {
		t.Fatalf("title: %q", ing.Title)
	}
}

// TestOpenCarriesTheDeliverySource is issue #188: every ingress records who
// delivered it, copied onto the ingress the same way the title is, and
// readable back through the one fold rather than a second reader of the
// chain.
func TestOpenCarriesTheDeliverySource(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: "chris"}
	source := Source{Subject: "chris", Method: "token", Channel: "hand"}

	ing, opened, err := r.Open(ctx, route, Delivery{
		Route: "issues", Key: "k-source", Payload: []byte("body text"), Source: source,
	})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}
	if ing.Source != source {
		t.Fatalf("source on the returned ingress = %+v, want %+v", ing.Source, source)
	}

	loaded, found, err := Load(ctx, s.Log(), tn2, ing.ID)
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if loaded.Source != source {
		t.Fatalf("source did not survive the fold: %+v, want %+v", loaded.Source, source)
	}
}

// TestOpenCarriesTheRouteSections is issue #275, intake half: a route that
// declares which sections its work runs in hands that declaration onto every
// ingress it opens, the same way Repo and Key are copied, and it survives the
// fold like Source does.
func TestOpenCarriesTheRouteSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: "chris", Sections: []string{"flaky"}}

	ing, opened, err := r.Open(ctx, route, Delivery{
		Route: "issues", Key: "k-sections",
	})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}
	want := []string{"flaky"}
	if !slices.Equal(ing.Sections, want) {
		t.Fatalf("sections on the returned ingress = %v, want %v", ing.Sections, want)
	}

	loaded, found, err := Load(ctx, s.Log(), tn2, ing.ID)
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if !slices.Equal(loaded.Sections, want) {
		t.Fatalf("sections did not survive the fold: %v, want %v", loaded.Sections, want)
	}
}

// TestOpenCarriesTheDeliveryPriority is issue #115 part 2: the priority a
// delivery names rides onto the ingress it opens, the same way Title and
// Source do.
func TestOpenCarriesTheDeliveryPriority(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: "chris"}

	ing, opened, err := r.Open(ctx, route, Delivery{
		Route: "issues", Key: "k-priority", Payload: []byte("body text"), Priority: queue.PriorityHigh,
	})
	if err != nil || !opened {
		t.Fatalf("open: %v %v", err, opened)
	}
	if ing.Priority != queue.PriorityHigh {
		t.Fatalf("priority on the returned ingress = %q, want %q", ing.Priority, queue.PriorityHigh)
	}

	loaded, found, err := Load(ctx, s.Log(), tn2, ing.ID)
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if loaded.Priority != queue.PriorityHigh {
		t.Fatalf("priority did not survive the fold: %q, want %q", loaded.Priority, queue.PriorityHigh)
	}
}

// TestOpenRefusesAnUnrecognisedPriority is issue #115 part 2: a non-empty
// priority outside the four levels is refused at Open rather than silently
// defaulted — the API and CLI rely on this refusal.
func TestOpenRefusesAnUnrecognisedPriority(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	r := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	route := Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: "chris"}

	_, _, err := r.Open(ctx, route, Delivery{
		Route: "issues", Key: "k-bad-priority", Payload: []byte("body text"), Priority: "banana",
	})
	if err == nil {
		t.Fatal("expected a refusal for an unrecognised priority")
	}
}
