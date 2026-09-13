package loop

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestLandWaitsCountsAndResets is issue #166's land-window analogue of
// TestSectionWaitsCountsAndResets: a gated-lane claim that reaches the land
// window with a passed candidate but finds the branch busy counts the wait
// (and keeps the candidate rather than rebuilding it), and a claim that
// lands resets the count.
func TestLandWaitsCountsAndResets(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"}) // a real command that passes in the workspace
	ctx := context.Background()

	branch, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: "refs/heads/" + e.loop.Branch, Run: "wi-elsewhere", TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	item := e.submit(t, "land-1", nil)
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("want waiting on the busy branch, got %s: %s", item.State, item.Note)
	}
	if item.LandWaits != 1 {
		t.Fatalf("a land-window wait should count, got LandWaits=%d", item.LandWaits)
	}
	if item.SectionWaits != 0 {
		t.Fatalf("a land-window wait must not touch SectionWaits, got %d", item.SectionWaits)
	}
	if item.Built == "" || len(item.PassedGates) == 0 {
		t.Fatalf("the passed candidate should be kept across the wait: %+v", item)
	}

	if err := e.store.Leaser("runner-1").Release(ctx, branch); err != nil {
		t.Fatal(err)
	}
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("want done once the branch frees, got %s: %s", item.State, item.Note)
	}
	if item.LandWaits != 0 {
		t.Fatalf("a landed claim should reset the count, got LandWaits=%d", item.LandWaits)
	}
}
