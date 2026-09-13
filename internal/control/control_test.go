package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func setup(t *testing.T) (*Room, *queue.Queue) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Room{Store: s, Tenant: tn, Actor: "chris"},
		&queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
}

func submit(t *testing.T, q *queue.Queue, id string) queue.WorkItem {
	t.Helper()
	w, err := q.Submit(context.Background(), queue.WorkItem{ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestStopHaltsNewClaimsImmediately(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-1")
	if err := q.Stop(ctx, tn, "chris", "investigating an incident"); err != nil {
		t.Fatal(err)
	}
	// The stated bound: not one new claim after the stop record lands.
	if _, _, err := q.Claim(ctx, w); !errors.Is(err, queue.ErrStopped) {
		t.Fatalf("claim after stop: want ErrStopped, got %v", err)
	}
	if err := q.Resume(ctx, tn, "chris", "root cause found"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Claim(ctx, w); err != nil {
		t.Fatalf("claim after resume: %v", err)
	}
	// Stop and resume are both records: who, when, why — never an erasure.
	kinds := []string{}
	for rec, err := range r.Store.Log().Since(ctx, tn, "control/claims", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, rec.Kind)
	}
	if len(kinds) != 2 || kinds[0] != "stop" || kinds[1] != "resume" {
		t.Fatalf("control chain: %v", kinds)
	}
}

func TestExceptionsListsOnlyWhatStillNeedsEyes(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	for _, id := range []string{"wi-a", "wi-b"} {
		w := submit(t, q, id)
		w, lease, err := q.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Park(ctx, w, lease, queue.SeverityMedium, "gate failed"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exceptions: %+v", got)
	}
	// An item that moves on drops out of the room; the index remembers, the
	// fold shows what still needs eyes.
	w, err := (&queue.Queue{Store: r.Store, Runner: "runner-1", TTL: time.Hour}).Load(ctx, tn, "wi-a")
	if err != nil {
		t.Fatal(err)
	}
	w, lease, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, w, lease, forge.Commit("resolvedsha")); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Exceptions(ctx)
	if len(got) != 1 || got[0].ID != "wi-b" {
		t.Fatalf("after resolution: %+v", got)
	}
}

// TestFailedItemsListBesideParkedOnes: queue.Fail has never written an
// index/exceptions record, so an item it settles is reachable only through
// index/pending — the same listing Backlog counts from (issue #111).
func TestFailedItemsListBesideParkedOnes(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	parked := submit(t, q, "wi-parked")
	_, lease, err := q.Claim(ctx, parked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, parked, lease, queue.SeverityMedium, "gate failed"); err != nil {
		t.Fatal(err)
	}

	failed := submit(t, q, "wi-failed")
	failed, lease2, err := q.Claim(ctx, failed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Fail(ctx, failed, lease2, "build panicked"); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want the parked item and the failed one, got %+v", got)
	}
	byID := map[string]Exception{}
	for _, x := range got {
		byID[x.ID] = x
	}
	if x, ok := byID["wi-parked"]; !ok || x.State != queue.StateParked {
		t.Fatalf("parked item missing or wrong state: %+v", byID["wi-parked"])
	}
	fx, ok := byID["wi-failed"]
	if !ok || fx.State != queue.StateFailed {
		t.Fatalf("failed item missing or wrong state: %+v", fx)
	}
	if fx.Parked.IsZero() {
		t.Fatal("failed item's clock (Parked) is zero — it should be when it entered failed")
	}
	// No index/exceptions record was ever written for it — this item is
	// found purely via index/pending.
	found := false
	for rec, err := range r.Store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == "wi-failed" {
			found = true
		}
	}
	if found {
		t.Fatal("test setup drifted: wi-failed unexpectedly has an index/exceptions record")
	}
}

// TestFailedCountAgreesWithBacklog: the status line's failed= count and the
// exception queue's page must fold the same rows — a count that is a second
// reading of the queue is a count that can disagree with it.
func TestFailedCountAgreesWithBacklog(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	for _, id := range []string{"wi-f1", "wi-f2"} {
		w := submit(t, q, id)
		w, lease, err := q.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Fail(ctx, w, lease, "failed"); err != nil {
			t.Fatal(err)
		}
	}
	submit(t, q, "wi-pending")

	exceptions, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	failedInExceptions := 0
	for _, x := range exceptions {
		if x.State == queue.StateFailed {
			failedInExceptions++
		}
	}
	backlog, err := r.Backlog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if failedInExceptions != backlog.ByState[queue.StateFailed] {
		t.Fatalf("exceptions had %d failed, backlog says %d", failedInExceptions, backlog.ByState[queue.StateFailed])
	}
	if backlog.ByState[queue.StateFailed] != 2 {
		t.Fatalf("want 2 failed in backlog, got %d", backlog.ByState[queue.StateFailed])
	}
}

// TestResolvedParkedItemDropsOutOfExceptions: an operator resolving an
// exception — no landing, just a judgement that there is nothing to build —
// takes it out of the room the same way a retry-to-done does.
func TestResolvedParkedItemDropsOutOfExceptions(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	w := submit(t, q, "wi-parked")
	w, lease, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, w, lease, queue.SeverityMedium, "gate failed"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("before resolve: %+v", got)
	}
	if _, err := q.Resolve(ctx, tn, "wi-parked", "chris", "nothing to build, closing"); err != nil {
		t.Fatal(err)
	}
	got, err = r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("resolved item still listed: %+v", got)
	}
}

func TestSampleIsDeterministicAndRecorded(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	for _, id := range []string{"wi-1", "wi-2", "wi-3", "wi-4", "wi-5"} {
		w := submit(t, q, id)
		w, lease, err := q.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Done(ctx, w, lease, forge.Commit("sha-"+id)); err != nil {
			t.Fatal(err)
		}
	}
	a, err := r.Sample(ctx, 2, "2026-08-review")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Sample(ctx, 2, "2026-08-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0].ID != b[0].ID || a[1].ID != b[1].ID {
		t.Fatalf("same seed, different sample: %v vs %v", a, b)
	}
	c, _ := r.Sample(ctx, 2, "2026-09-review")
	if a[0].ID == c[0].ID && a[1].ID == c[1].ID {
		t.Fatal("different seed, same sample — suspicious for 5 choose 2")
	}
	if err := r.SignOff(ctx, a[0].ID, "pass", "looks right"); err != nil {
		t.Fatal(err)
	}
	if err := r.SignOff(ctx, a[1].ID, "", ""); err == nil {
		t.Fatal("a verdictless sign-off was accepted")
	}
	// The selections and the sign-off are all on one reviewable chain.
	kinds := map[string]int{}
	for rec, err := range r.Store.Log().Since(ctx, tn, "control/review", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[rec.Kind]++
	}
	if kinds["review-sample"] != 3 || kinds["review-signoff"] != 1 {
		t.Fatalf("review chain: %v", kinds)
	}
}

// TestQueueListsWhatACountCannot is issue #20 as an assertion. Fifteen units
// went in one morning and `pending=14` was the entire view: no way to see what
// the factory was sitting on, and no way to read the notes it had already
// written about why. Every field here comes from records that existed the
// whole time.
func TestQueueListsWhatACountCannot(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	waiting := submit(t, q, "wi-waiting")
	waiting.Task = "teach the pump to breathe\nand a second line nobody needs in a listing"
	_, claim, err := q.Claim(ctx, waiting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Wait(ctx, waiting, claim, "branch main is leased"); err != nil {
		t.Fatal(err)
	}
	parked := submit(t, q, "wi-parked")
	_, claim2, err := q.Claim(ctx, parked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, parked, claim2, queue.SeverityMedium, "check: fail (go vet)"); err != nil {
		t.Fatal(err)
	}
	submit(t, q, "wi-fresh")

	all, err := r.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 items, got %d: %+v", len(all), all)
	}
	// Oldest first: the order the factory would take them.
	if all[0].ID != "wi-waiting" || all[2].ID != "wi-fresh" {
		t.Fatalf("not in queue order: %s, %s, %s", all[0].ID, all[1].ID, all[2].ID)
	}

	byID := map[string]QueueEntry{}
	for _, e := range all {
		byID[e.ID] = e
	}
	// The note is the whole point: the queue always wrote why, and nothing
	// ever showed it.
	if got := byID["wi-waiting"].Note; got != "branch main is leased" {
		t.Fatalf("the waiting reason is still invisible: %q", got)
	}
	if got := byID["wi-parked"].Note; got != "check: fail (go vet)" {
		t.Fatalf("the park reason is still invisible: %q", got)
	}
	// The task is a first line, not a whole prompt pasted into a listing.
	if got := byID["wi-waiting"].Task; got != "teach the pump to breathe" {
		t.Fatalf("task line: %q", got)
	}
	if byID["wi-waiting"].Opened.IsZero() || byID["wi-waiting"].AgeSec < 0 {
		t.Fatalf("no clock on the row: %+v", byID["wi-waiting"])
	}

	// Filtering is what makes it usable at fifteen units.
	pending, err := r.Queue(ctx, queue.StatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "wi-fresh" {
		t.Fatalf("state filter: %+v", pending)
	}
	two, err := r.Queue(ctx, queue.StateWaiting, queue.StateParked)
	if err != nil {
		t.Fatal(err)
	}
	if len(two) != 2 {
		t.Fatalf("multi-state filter: %+v", two)
	}
}

// TestQueueShowsStageAndWhatBlocks: the room could show what was stuck and
// what had landed, and nothing in between. A claimed item read "state=claimed"
// and nothing else, and a blocked one never named what it was blocked on —
// which is the first question when nothing is moving (issue #14).
func TestQueueShowsStageAndWhatBlocks(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	root := submit(t, q, "wi-root")
	dependent, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-dependent", Tenant: tn, Box: "shop", Ingress: "ing-1",
		Task: "waits on the root", DependsOn: []string{"wi-root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = dependent

	// The root gets claimed and starts building.
	_, claim, err := q.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := r.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]QueueEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	if got := byID["wi-root"].Stage; got != "claimed" {
		t.Fatalf("a claimed item should say where it got to, said %q", got)
	}
	if got := byID["wi-dependent"].BlockedBy; len(got) != 1 || got[0] != "wi-root" {
		t.Fatalf("blocked item does not name what blocks it: %v", got)
	}

	// The root lands; the dependent is no longer blocked by it.
	if _, err := q.Done(ctx, root, claim, "abc123"); err != nil {
		t.Fatal(err)
	}
	entries, err = r.Queue(ctx, queue.StatePending)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == "wi-dependent" && len(e.BlockedBy) != 0 {
			t.Fatalf("a settled dependency still reads as blocking: %v", e.BlockedBy)
		}
	}
}

// TestQueueRowCarriesSummaryAndClipsTask: the row a listing shows must carry
// the planner's summary when the item has one, and must never let a task
// with no newline — a single paragraph — through unbounded (issue #74).
func TestQueueRowCarriesSummaryAndClipsTask(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	if _, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-summary", Tenant: tn, Box: "shop", Ingress: "ing-1",
		Task: "t", Summary: "teach the pump to breathe",
	}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 250)
	if _, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-long", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: long,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-presummary", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "no summary here",
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := r.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]QueueEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	if got := byID["wi-summary"].Summary; got != "teach the pump to breathe" {
		t.Fatalf("summary not carried onto the row: %q", got)
	}

	wantClipped := long[:199] + "…"
	if got := byID["wi-long"].Task; got != wantClipped {
		t.Fatalf("task not clipped: got %q", got)
	}

	// An item queued before queue.Item.Summary existed has none — no
	// server-side guess fills it in.
	if got := byID["wi-presummary"].Summary; got != "" {
		t.Fatalf("summary should stay absent when the item never had one: %q", got)
	}
	if got := byID["wi-presummary"].Task; got != "no summary here" {
		t.Fatalf("task line: %q", got)
	}
}
