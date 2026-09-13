package loop

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// windowSpentBuilder returns &WindowSpent for its first failFor calls — the
// run's own provider account report that the window is exhausted, met
// mid-build rather than at pump.Acquire (issue #112) — then builds for real.
type windowSpentBuilder struct {
	scriptedBuilder
	n       int
	failFor int
	ws      WindowSpent
}

func (b *windowSpentBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.n++
	if b.n <= b.failFor {
		w := b.ws
		return "", &w
	}
	return b.scriptedBuilder.Build(ctx, ws, task)
}

// TestWindowSpentBuildWaitsWithTheNamedReset: a build refused by the run's
// own account report goes back to waiting — not parked, not an exception —
// with the reset the run named in the note verbatim, a "deferred" record on
// the chain carrying the window and the reset, no resume point (a build
// refused mid-run keeps nothing), and no exceptions index record. A build
// that then answers lands and resets the streak.
func TestWindowSpentBuildWaitsWithTheNamedReset(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	resetsAt := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	builder := &windowSpentBuilder{failFor: 1, ws: WindowSpent{
		Window: "five_hour", ResetsAt: resetsAt, Reason: "You've hit your session limit",
	}}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-1"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-1", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a window-spent build should re-queue as waiting, got %s (%s)", item.State, item.Note)
	}
	if item.DeferredTries != 1 {
		t.Fatalf("DeferredTries = %d, want 1", item.DeferredTries)
	}
	if item.Built != "" {
		t.Fatalf("a build refused mid-run keeps nothing to resume from, got Built=%q", item.Built)
	}
	if !strings.Contains(item.Note, "waiting for the account window") || !strings.Contains(item.Note, resetsAt.Format(time.RFC3339)) {
		t.Fatalf("waiting note should read verbatim as an account-window wait: %q", item.Note)
	}

	deferred := 0
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-window-1", 0) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != "deferred" {
			continue
		}
		deferred++
		body := string(r.Body)
		if !strings.Contains(body, "five_hour") || !strings.Contains(body, fmt.Sprintf("%d", resetsAt.Unix())) {
			t.Fatalf("deferred record should carry the window and reset: %s", body)
		}
	}
	if deferred != 1 {
		t.Fatalf("expected exactly one deferred record on the chain, got %d", deferred)
	}

	exceptions := 0
	for _, err := range e.store.Log().Since(ctx, tn, "index/exceptions", 0) {
		if err != nil {
			t.Fatal(err)
		}
		exceptions++
	}
	if exceptions != 0 {
		t.Fatalf("a build deferral must write no exceptions record, got %d", exceptions)
	}

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a build that stops deferring should proceed to land, got %s (%s)", item.State, item.Note)
	}
	if item.DeferredTries != 0 {
		t.Fatalf("a clean build should reset the counter, got %d", item.DeferredTries)
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2", builder.n)
	}
}

// TestWindowSpentBuildPastTheBoundParks: a claim that keeps deferring for the
// account window past MaxWindowDefers parks instead of waiting forever, with
// the streak count named in the note — the same shape MaxAbortedRetries
// takes for a dropped transport.
func TestWindowSpentBuildPastTheBoundParks(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	resetsAt := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	builder := &windowSpentBuilder{failFor: 1000, ws: WindowSpent{
		Window: "five_hour", ResetsAt: resetsAt, Reason: "session limit",
	}}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-bound"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-bound", "do a thing"))

	for i := 0; i <= MaxWindowDefers; i++ {
		item, err = e.loop.Run(ctx, item)
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	}
	if item.State != queue.StateParked {
		t.Fatalf("deferring past the bound should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, fmt.Sprintf("%d", MaxWindowDefers)) {
		t.Fatalf("park note should say how many times it deferred: %q", item.Note)
	}
	if want := MaxWindowDefers + 1; builder.n != want {
		t.Fatalf("builder ran %d times, want %d", builder.n, want)
	}
}

// TestWindowSpentZeroResetResolvesFromTheMeter: a refusal at session or
// prompt start names no reset of its own — the run sent no usage updates —
// and the loop resolves one from the meter's own held account window
// instead.
func TestWindowSpentZeroResetResolvesFromTheMeter(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Meter = &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1"}
	resetsAt := time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC)
	e.loop.Meter.Charge(ctx, "seed", capacity.Spend{
		Reported: true, Total: 10,
		Limit: &capacity.Limit{Status: "rejected", Window: "five_hour", Utilization: 1, ResetsAt: resetsAt.Unix()},
	})
	builder := &windowSpentBuilder{failFor: 1, ws: WindowSpent{
		Window: "five_hour", Reason: "session limit, no reset named",
	}}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-zero"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-zero", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a zero-reset refusal with a meter-held window should still wait, got %s (%s)", item.State, item.Note)
	}
	// Meter.Window() reads ResetsAt back as unix seconds and resolves it with
	// time.Unix, which yields the local zone — compare the same way rather
	// than against resetsAt's own (UTC) Format.
	want := time.Unix(resetsAt.Unix(), 0).Format(time.RFC3339)
	if !strings.Contains(item.Note, want) {
		t.Fatalf("note should resolve the reset from the meter's held window (want %q): %q", want, item.Note)
	}
}

// TestWindowSpentWithNoResetAnywhereParksLikeAnyOtherError: a zero-reset
// refusal with no account window on record anywhere is not a bounded wait —
// unknown never reads as zero — so it takes today's ordinary build-error park
// path instead of deferring blind.
func TestWindowSpentWithNoResetAnywhereParksLikeAnyOtherError(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	builder := &windowSpentBuilder{failFor: 1000, ws: WindowSpent{
		Window: "five_hour", Reason: "session limit, no reset named",
	}}
	e.loop.Builder = builder // no Meter set: nothing anywhere names a reset

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-noreset"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-noreset", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a window refusal with no reset anywhere should park like any other build error, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "session limit, no reset named") {
		t.Fatalf("park note should carry the underlying error: %q", item.Note)
	}
	if item.DeferredTries != 0 {
		t.Fatalf("a park that never deferred should not increment DeferredTries, got %d", item.DeferredTries)
	}
}

// windowSpentGate always errors with a *WindowSpent wrapped in — the run's
// own account report, met at the gate rather than at the build. n counts
// every ask, so a test can assert evaluateGate never burned a retry on it.
type windowSpentGate struct {
	name string
	n    *int
	ws   WindowSpent
}

func (g windowSpentGate) Name() string { return g.name }

func (g windowSpentGate) Evaluate(context.Context, forge.ChangeSet) (forge.Verdict, error) {
	*g.n++
	w := g.ws
	return forge.Verdict{}, fmt.Errorf("%s: %w", g.name, &w)
}

// TestWindowSpentGateWaitsKeepingTheCandidate: a review refused by the
// account's window keeps the built commit — #76's shape — and resumes at the
// gate rather than the builder, without burning a gate retry.
func TestWindowSpentGateWaitsKeepingTheCandidate(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	calls := 0
	resetsAt := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	e.loop.GateRetries = 2
	e.loop.GateBackoff = func(int) time.Duration { return 0 }
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{windowSpentGate{name: "review", n: &calls, ws: WindowSpent{
			Window: "five_hour", ResetsAt: resetsAt, Reason: "session limit",
		}}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-gate"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-gate", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a window-spent gate refusal should wait, got %s (%s)", item.State, item.Note)
	}
	if calls != 1 {
		t.Fatalf("a window-spent gate error must never be retried: gate was asked %d times", calls)
	}
	if item.Built == "" {
		t.Fatal("waiting on a window-spent gate refusal should keep the built commit so the next claim resumes at the gate")
	}
	if item.DeferredTries != 1 {
		t.Fatalf("DeferredTries = %d, want 1", item.DeferredTries)
	}
	if !strings.Contains(item.Note, "waiting for the account window") {
		t.Fatalf("waiting note should read as an account-window wait: %q", item.Note)
	}

	built := item.Built
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("expected the second window-spent gate refusal to wait too, got %s", item.State)
	}
	if item.Built != built {
		t.Fatal("resuming a window-spent wait must re-enter at the gate on the SAME commit, not rebuild")
	}
	if calls != 2 {
		t.Fatalf("gate should have been asked twice total (once per resumed claim), got %d", calls)
	}
}

// TestWindowSpentGatePastTheBoundParksKeepingTheCommit: a gate that keeps
// deferring for the account window past MaxWindowDefers parks — keeping the
// commit, since nothing here judged it wrong — with the streak named.
func TestWindowSpentGatePastTheBoundParksKeepingTheCommit(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	calls := 0
	resetsAt := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	e.loop.GateBackoff = func(int) time.Duration { return 0 }
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{windowSpentGate{name: "review", n: &calls, ws: WindowSpent{
			Window: "five_hour", ResetsAt: resetsAt, Reason: "session limit",
		}}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-window-gate-bound"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-window-gate-bound", "add a thing"))

	for i := 0; i <= MaxWindowDefers; i++ {
		item, err = e.loop.Run(ctx, item)
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	}
	if item.State != queue.StateParked {
		t.Fatalf("deferring past the bound should park, got %s (%s)", item.State, item.Note)
	}
	if item.Built == "" {
		t.Fatal("parking past the window-defer bound should keep the built commit (ParkKeeping)")
	}
	if !strings.Contains(item.Note, fmt.Sprintf("%d", MaxWindowDefers)) {
		t.Fatalf("park note should say how many times it deferred: %q", item.Note)
	}
	if calls != MaxWindowDefers+1 {
		t.Fatalf("gate should have been asked once per claim, got %d", calls)
	}
}
