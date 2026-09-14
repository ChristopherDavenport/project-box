package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// ciLander is the [forge.Lander] seam filled with GitHub's independent-CI
// answer for issue #273: pending (optionally the result of a rerun this call
// just spent) or a red that stayed red across the rerun budget. It never
// calls the wrapped git.Lander — nothing is pushed — and records the
// CIRerunsLeft budget the loop asserted on each cs it was handed, so a test
// can see that budget fall as CIRerunsSpent persists across ticks.
type ciLander struct {
	n              int
	rerunSpent     bool
	failed         bool
	runID          int64
	url            string
	rerunsLeftSeen []int
}

func (s *ciLander) Land(_ context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	s.n++
	s.rerunsLeftSeen = append(s.rerunsLeftSeen, cs.CIRerunsLeft)
	sha := string(cs.Members[0].Head)
	if s.failed {
		return forge.Landing{}, &git.CIFailed{Sha: sha, RunID: s.runID, URL: s.url}
	}
	return forge.Landing{}, &git.CIPending{Sha: sha, URL: s.url, RerunSpent: s.rerunSpent}
}

// TestLandCIPendingWaitsKeepingTheCandidate is issue #273's pacing case: a
// pending independent CI is not a refusal, so the item goes back to
// StateWaiting — claimable, carrying the gate-passed candidate — rather than
// parking as an exception, and the room's note names the sha, the run URL,
// and where this wait stands against the bound.
func TestLandCIPendingWaitsKeepingTheCandidate(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := &ciLander{url: "https://ci.example/runs/1"}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	item := e.submit(t, "ci-pending", nil)
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	if item.State != queue.StateWaiting {
		t.Fatalf("a pending CI should wait, not park, got %s (%s)", item.State, item.Note)
	}
	if item.Built == "" || len(item.PassedGates) == 0 {
		t.Fatalf("a CI wait must keep the gate-passed candidate: %+v", item)
	}
	if item.CIWaits != 1 {
		t.Fatalf("CIWaits = %d, want 1", item.CIWaits)
	}
	if item.CIRerunsSpent != 0 {
		t.Fatalf("CIRerunsSpent = %d, want 0: nothing failed, so nothing was rerun", item.CIRerunsSpent)
	}
	wantNote := "waiting for CI on " + item.Built + " (https://ci.example/runs/1), wait 1 of 80"
	if item.Note != wantNote {
		t.Fatalf("note = %q, want %q", item.Note, wantNote)
	}
	if stub.n != 1 {
		t.Fatalf("Land should have been asked once: %d", stub.n)
	}
	if got := tryRecordCount(t, e, item.Built, "land"); got != 0 {
		t.Fatalf("a pending CI must never record a landing: %d land records", got)
	}
}

// TestLandCIPendingRerunSpentDecrementsTheBudget is issue #273's rerun-budget
// half: when [git.CIPending.RerunSpent] says this call already asked GitHub
// to rerun a red, the loop must debit its own persisted budget so the NEXT
// tick's ChangeSet carries less of it — the forge package never tracks a
// candidate's spend across ticks itself (forge.ChangeSet.CIRerunsLeft's own
// doc). The counters must survive the fold, not just the in-memory item Run
// handed back: the pump always re-Loads between ticks (Pump.Drain), so the
// close here goes through q.Load, the same as the pump would, and the SECOND
// Run is handed that reloaded item rather than the first call's return value.
func TestLandCIPendingRerunSpentDecrementsTheBudget(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	stub := &ciLander{url: "https://ci.example/runs/2", rerunSpent: true}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	item := e.submit(t, "ci-rerun", nil)
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a rerun-pending CI should still wait, got %s (%s)", item.State, item.Note)
	}
	if item.CIRerunsSpent != 1 {
		t.Fatalf("CIRerunsSpent = %d, want 1: a rerun this call just spent must be recorded", item.CIRerunsSpent)
	}
	if len(stub.rerunsLeftSeen) != 1 || stub.rerunsLeftSeen[0] != 1 {
		t.Fatalf("the first cs should carry the full default budget: %v", stub.rerunsLeftSeen)
	}

	loaded, err := q.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CIWaits != 1 || loaded.CIRerunsSpent != 1 {
		t.Fatalf("the counters must persist through the queue's fold, not just the in-memory item: CIWaits=%d CIRerunsSpent=%d", loaded.CIWaits, loaded.CIRerunsSpent)
	}

	item, err = e.loop.Run(ctx, loaded)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.CIWaits != 2 {
		t.Fatalf("CIWaits = %d, want 2 after a second wait", item.CIWaits)
	}
	if len(stub.rerunsLeftSeen) != 2 || stub.rerunsLeftSeen[1] != 0 {
		t.Fatalf("the second tick's cs should carry the spent budget back to zero: %v", stub.rerunsLeftSeen)
	}
}

// TestLandCIPendingOverBoundParksAsException is issue #273's bound case: a
// CI that never concludes must not wait forever — past [Loop.CIWaits] the
// item parks as an exception naming the sha, the CI url, and how long it
// held, exactly the shape a person sees on the exception queue.
func TestLandCIPendingOverBoundParksAsException(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.CIWaits = 1
	stub := &ciLander{url: "https://ci.example/runs/3"}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	item := e.submit(t, "ci-stuck", nil)
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting || item.CIWaits != 1 {
		t.Fatalf("setup: want one wait under the bound of 1, got %s CIWaits=%d", item.State, item.CIWaits)
	}

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a CI that outlives the wait bound should park, got %s (%s)", item.State, item.Note)
	}
	if item.Severity != queue.SeverityMedium {
		t.Fatalf("severity = %s, want %s", item.Severity, queue.SeverityMedium)
	}
	if item.Built == "" {
		t.Fatal("parking on a stuck CI should keep its resume point")
	}
	if !strings.Contains(item.Note, item.Built) || !strings.Contains(item.Note, "https://ci.example/runs/3") {
		t.Fatalf("note should name the sha and the CI url: %q", item.Note)
	}
	if !strings.Contains(item.Note, "2 waits") {
		t.Fatalf("note should say how long it held: %q", item.Note)
	}
	if stub.n != 2 {
		t.Fatalf("Land should have been asked exactly twice before parking: %d", stub.n)
	}
}

// TestLandCIFailedParksNamingTheRun is issue #273's real-failure case: a CI
// that stayed red with no rerun budget left is an exception, not a wait —
// [git.CIFailed] parks immediately, naming the run and that the rerun
// budget is what ran out.
func TestLandCIFailedParksNamingTheRun(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := &ciLander{failed: true, runID: 42, url: "https://ci.example/runs/42"}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	item := e.submit(t, "ci-failed", nil)
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a red CI with no rerun budget left should park, got %s (%s)", item.State, item.Note)
	}
	if item.Severity != queue.SeverityMedium {
		t.Fatalf("severity = %s, want %s", item.Severity, queue.SeverityMedium)
	}
	if item.Built == "" {
		t.Fatal("parking on a failed CI should keep its resume point")
	}
	if !strings.Contains(item.Note, "42") || !strings.Contains(item.Note, "https://ci.example/runs/42") {
		t.Fatalf("note should name the run id and url: %q", item.Note)
	}
	if !strings.Contains(item.Note, "rerun budget") {
		t.Fatalf("note should say CI stayed red across the rerun budget: %q", item.Note)
	}
	if item.CIWaits != 0 {
		t.Fatalf("a CIFailed park should not touch CIWaits: %d", item.CIWaits)
	}
}

// TestLandCIPendingWaitsInTheOptimisticLane confirms the commit body's claim:
// both land call sites reach [PRLander.Land] through the same
// landWithRetries, so the optimistic lane classifies a pending CI exactly as
// the gated lane does — a land-then-check repo now waits for CI before its
// immediate land.
func TestLandCIPendingWaitsInTheOptimisticLane(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	optimistic(e)
	stub := &ciLander{url: "https://ci.example/runs/5"}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	item := e.submit(t, "ci-optimistic", nil)
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a pending CI should wait in the optimistic lane too, got %s (%s)", item.State, item.Note)
	}
	if item.CIWaits != 1 {
		t.Fatalf("CIWaits = %d, want 1", item.CIWaits)
	}
	if got := tryRecordCount(t, e, item.Built, "land"); got != 0 {
		t.Fatalf("a pending CI must never record a landing: %d land records", got)
	}
}
