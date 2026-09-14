package pump

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestASectionBusyCandidateNeverSpendsASeat is issue #120's incident,
// reproduced directly. Before this fix, every candidate's meter seat was
// spent BEFORE its section was ever checked, so under a tight budget a
// candidate ordered first whose section a foreign lease already held took
// the only seat merely to discover, inside it, that it had to wait — and the
// next candidate, ordered after it and running in a genuinely disjoint
// section, was never even tried. Now the section is taken, and checked,
// before Meter.Acquire is ever reached: A waits seatlessly, and B runs.
func TestASectionBusyCandidateNeverSpendsASeat(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()
	w.sectionedRepo()

	a := w.submitItem(t, "a-1", "fix module a", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
	})
	b := w.submitItem(t, "b-1", "fix module b", func(wi *queue.WorkItem) {
		wi.Sections = []string{"web"}
	})
	holdSection(t, w, "api", "wi-foreign")

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the pass's dispatched run")
	}
	a, err := w.q.Load(ctx, tn, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err = w.q.Load(ctx, tn, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != queue.StateWaiting {
		t.Fatalf("A should wait on the foreign-held section rather than spend the one seat, got %s: %s", a.State, a.Note)
	}
	if a.SectionWaits != 1 {
		t.Fatalf("A's section wait should be counted, got %d", a.SectionWaits)
	}
	for _, want := range []string{"api", "wi-foreign"} {
		if !strings.Contains(a.Note, want) {
			t.Errorf("A's wait note does not name %q: %s", want, a.Note)
		}
	}
	if b.State != queue.StateDone {
		t.Fatalf("B, disjoint and ordered after A, should have run under the seat A never spent, got %s: %s", b.State, b.Note)
	}
}

// TestTwoCandidatesInTheSameFreeSectionSerialize: sections still serialize
// two units contending for the same one — exactly one runs this tick, the
// other ends up waiting, and the seatless path this issue adds never lets
// both through.
//
// A count of end states alone is not enough to name this: the first attempt
// at this issue passed this same shape (done=1, waiting=1) while B, the
// loser of the section race, had already built a commit concurrently with
// A — reapGhost mistook A's live-but-not-yet-claimed grant for an abandoned
// one and let B take the same section too, and only the branch lease at the
// land window kept B from finishing outright. So the assertion that matters
// is on the loser directly: it never built anything at all.
func TestTwoCandidatesInTheSameFreeSectionSerialize(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 4})
	ctx := context.Background()
	w.sectionedRepo()

	a := w.submitItem(t, "a-1", "fix module a", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
	})
	b := w.submitItem(t, "b-1", "fix module b", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
	})

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the pass's dispatched run")
	}
	a, err := w.q.Load(ctx, tn, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err = w.q.Load(ctx, tn, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	done, waiting := 0, 0
	for _, s := range []queue.State{a.State, b.State} {
		switch s {
		case queue.StateDone:
			done++
		case queue.StateWaiting:
			waiting++
		}
	}
	if done != 1 || waiting != 1 {
		t.Fatalf("sections should serialize exactly one runner, got a=%s b=%s", a.State, b.State)
	}
	loser := a
	if loser.State != queue.StateWaiting {
		loser = b
	}
	if loser.Built != "" {
		t.Fatalf("the loser of the section race must never have built anything, got Built=%q", loser.Built)
	}
}

// TestAMeterRefusalReleasesTheGrantAndLeavesTheItemPending covers "together
// or not at all" from the other side: a candidate's section is free and
// TakeSections grants it, but the meter itself refuses the seat. The grant
// must not outlive that refusal — a section held past a refused seat is
// exactly the kind of lease nothing will ever release — and the item is left
// exactly where Drain found it: pending, never claimed.
func TestAMeterRefusalReleasesTheGrantAndLeavesTheItemPending(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 1})
	ctx := context.Background()
	w.sectionedRepo()

	item := w.submitItem(t, "a-1", "fix module a", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
	})
	// Holds the one concurrency slot from outside Drain entirely, standing in
	// for another run already in flight under the same meter.
	release, err := w.meter.Acquire(ctx, "held-elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	item, err = w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StatePending {
		t.Fatalf("a meter refusal should leave the item pending, untouched, got %s: %s", item.State, item.Note)
	}

	// The section grant TakeSections took must already be released: a fresh
	// acquire by a different run succeeds immediately, with nothing to reap.
	lease, err := w.store.Leaser("pump-runner").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: "app", Ref: loop.SectionRef("api"), Run: "wi-someone-else", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("the section grant was not released on the meter's refusal: %v", err)
	}
	w.store.Leaser("pump-runner").Release(ctx, lease)
}

// TestAResumePointSurvivesASeatlessWait: the seatless wait path this issue
// adds claims the item and waits it exactly as leaseSections' busy branch
// inside Run always has — it must not go anywhere near Built, the same
// guarantee every other KeepCandidate-before-Wait path already gives.
func TestAResumePointSurvivesASeatlessWait(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 4})
	ctx := context.Background()
	w.sectionedRepo()

	item := w.submitItem(t, "a-1", "fix module a", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
		wi.Built, wi.BuiltBase = "deadbeef", "cafebabe"
	})
	holdSection(t, w, "api", "wi-foreign")

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	item, err := w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("the item should wait on the held section, got %s: %s", item.State, item.Note)
	}
	if item.Built != "deadbeef" {
		t.Fatalf("the seatless wait must not touch a kept resume point, got Built=%q", item.Built)
	}
}

// TestASectionBusyCandidateNeverCountsAStart is issue #120's other closed
// half spelled out directly: Meter.Start is a build-only charge, reached
// only once the builder is actually invoked, so a candidate that spends a
// whole tick waiting on a busy section must never move Report().Started —
// and once its section frees and it actually runs, it counts exactly one.
func TestASectionBusyCandidateNeverCountsAStart(t *testing.T) {
	w := setup(t, capacity.Budget{MaxConcurrent: 4})
	ctx := context.Background()
	w.sectionedRepo()

	item := w.submitItem(t, "a-1", "fix module a", func(wi *queue.WorkItem) {
		wi.Sections = []string{"api"}
	})
	holder := holdSection(t, w, "api", "wi-foreign")

	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.meter.Report().Started; got != 0 {
		t.Fatalf("a section-busy candidate should never reach Meter.Start, got %d starts", got)
	}
	item, err := w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("the item should wait on the held section, got %s: %s", item.State, item.Note)
	}

	if err := w.store.Leaser("pump-runner").Release(ctx, holder); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the pass's dispatched run")
	}
	if got := w.meter.Report().Started; got != 1 {
		t.Fatalf("once the section frees and the item actually runs, it should count exactly one start, got %d", got)
	}
	item, err = w.q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("the item should complete once the section frees, got %s: %s", item.State, item.Note)
	}
}
