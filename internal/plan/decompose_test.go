package plan

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// TestIdentityCarriesTheOriginationsTitleAsSummary. The one-node identity plan
// is what every ordinary issue gets, and the room's name for a unit comes from
// the plan's Summary, not its Task — so the origination's title has to survive
// the one decomposer that runs on the critical path of every intake.
func TestIdentityCarriesTheOriginationsTitleAsSummary(t *testing.T) {
	ing := ingress.Ingress{ID: "ing-1", Kind: kind.SeedIssue, Tenant: tn, Box: "shop",
		Repo: "app", Requester: "chris", Note: "fix the thing", Title: "the thing is broken"}
	res, err := Identity{}.Decompose(context.Background(), ing, kind.Declaration{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("nodes: %d", len(res.Nodes))
	}
	if got := res.Nodes[0].Summary; got != "the thing is broken" {
		t.Fatalf("summary: %q", got)
	}
}
