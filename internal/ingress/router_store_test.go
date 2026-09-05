package ingress

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/kind"
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
