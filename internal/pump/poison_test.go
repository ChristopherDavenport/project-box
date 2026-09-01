package pump

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// corrupt plants an unparseable STATE record on an item's chain — the one
// thing the fold cannot recover from, and so the one thing worth asserting
// the pump survives. (Narrative prose in the chain is no longer corruption;
// see queue.TestFoldSkipsNarrativeRecords.)
func corrupt(t *testing.T, w *world, id string) {
	t.Helper()
	if _, err := w.store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: "workitem/" + id, Kind: "claimed", Subject: id,
		Actor: "somebody", Body: []byte("not a work item"),
	}); err != nil {
		t.Fatal(err)
	}
}

// TestIntakeNeverRequeuesAnUnreadableItem. Intake asked "does this item
// exist?" by calling Load and reading ANY error as no. So an item whose chain
// had stopped parsing was created again on every tick — each new queued
// record making it claimable again, each claim spending another agent
// session. An idempotency key that answers "no" to a question it could not
// read is not an idempotency key.
func TestIntakeNeverRequeuesAnUnreadableItem(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()

	w.open(t, "issue-1", "fix module alpha")
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}
	id := onlyItemID(t, w)
	corrupt(t, w, id)

	before := chainLen(t, w, id)
	for range 5 {
		if _, err := w.pump.Intake(ctx); err == nil {
			t.Fatal("intake read an unreadable chain as absence and carried on")
		}
	}
	if after := chainLen(t, w, id); after != before {
		t.Fatalf("chain grew %d -> %d across five intakes: the item is being re-queued", before, after)
	}
}

// TestOneUnreadableItemDoesNotStopTheDrain. Drain returned on the first Load
// error, so a single corrupt chain cost every OTHER item its tick — 31
// consecutive ticks settled nothing while seventeen healthy items sat
// pending. The unreadable item must cost only itself.
func TestOneUnreadableItemDoesNotStopTheDrain(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	w.open(t, "issue-1", "fix module alpha")
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}
	corrupt(t, w, onlyItemID(t, w))

	// A healthy item arrives behind the broken one.
	w.open(t, "issue-2", "fix module beta")

	deadline := time.Now().Add(50 * time.Second)
	for time.Now().Before(deadline) {
		// Intake now refuses while the bad chain is present, which is correct
		// and is not the property under test: drain the queue as it stands.
		w.pump.Intake(ctx)
		if _, err := w.pump.Drain(ctx); err == nil {
			t.Fatal("drain hid the unreadable item instead of reporting it")
		}
		n := strings.TrimSpace(mustSh(t, w.remote, "git", "rev-list", "--count", "refs/heads/main"))
		if n == "2" { // seed + the healthy item's landing
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the healthy item never landed: one unreadable chain still stops the factory")
}

func onlyItemID(t *testing.T, w *world) string {
	t.Helper()
	for rec, err := range w.store.Log().Since(context.Background(), tn, "index/pending", 1) {
		if err != nil {
			t.Fatal(err)
		}
		return rec.Subject
	}
	t.Fatal("no item was created")
	return ""
}

func chainLen(t *testing.T, w *world, id string) int {
	t.Helper()
	n := 0
	for _, err := range w.store.Log().Since(context.Background(), tn, "workitem/"+id, 1) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	return n
}

var _ = queue.ErrNoItem // the sentinel this file's properties are written against
