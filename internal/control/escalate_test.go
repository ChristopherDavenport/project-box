package control

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// clocked builds a room and a queue over a store whose clock a test can move,
// so an exception can be four hours old without a test that takes four hours.
type clocked struct {
	room    *Room
	queue   *queue.Queue
	advance func(time.Duration)
}

func clockedSetup(t *testing.T, objective time.Duration) *clocked {
	t.Helper()
	var mu sync.Mutex
	offset := time.Duration(0)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return time.Now().Add(offset) }
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &clocked{
		room:    &Room{Store: s, Tenant: tn, Actor: "chris", Objective: objective, Now: now},
		queue:   &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour},
		advance: func(d time.Duration) { mu.Lock(); defer mu.Unlock(); offset += d },
	}
}

// parkAs drives an item to the exception queue at a stated severity.
func parkAs(t *testing.T, w *clocked, id string, sev queue.Severity, reason string) {
	t.Helper()
	ctx := context.Background()
	item, err := w.queue.Submit(ctx, queue.WorkItem{
		ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := w.queue.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.queue.Park(ctx, item, lease, sev, reason); err != nil {
		t.Fatal(err)
	}
}

// recorder is a Notifier that keeps what it was told — the seam, satisfied by
// a test rather than by a mail server.
type recorder struct {
	mu   sync.Mutex
	sent []Notice
	err  error
}

func (r *recorder) Notify(_ context.Context, n Notice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return r.err
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// TestExceptionsAgeAndAreOrderedLoudestFirst. The queue could say WHAT was
// parked and never for how long, and it returned them in arrival order —
// which is the order the factory would take work in and exactly the wrong
// order for a person clearing a queue.
func TestExceptionsAgeAndAreOrderedLoudestFirst(t *testing.T) {
	w := clockedSetup(t, 4*time.Hour)
	ctx := context.Background()

	parkAs(t, w, "wi-old-routine", queue.SeverityMedium, "gate: fail (go vet)")
	w.advance(2 * time.Hour)
	parkAs(t, w, "wi-new-critical", queue.SeverityCritical, "landed red and repair would not open")
	w.advance(30 * time.Minute)
	parkAs(t, w, "wi-new-routine", queue.SeverityMedium, "build: agent gave up")

	got, err := w.room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 exceptions, got %d: %+v", len(got), got)
	}
	// Loudest first, then oldest — the order a person should work them.
	want := []string{"wi-new-critical", "wi-old-routine", "wi-new-routine"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order[%d] = %s, want %s (%+v)", i, got[i].ID, id, ids(got))
		}
	}
	// The clock the exception queue never had.
	byID := map[string]Exception{}
	for _, x := range got {
		byID[x.ID] = x
	}
	if a := byID["wi-old-routine"].AgeSec; a < int64((2*time.Hour + 25*time.Minute).Seconds()) {
		t.Fatalf("the oldest exception reads as %ds old", a)
	}
	if byID["wi-old-routine"].Parked.IsZero() {
		t.Fatal("an exception with no parked time cannot be late")
	}
	if byID["wi-new-routine"].Overdue {
		t.Fatal("a fresh exception is already overdue against a 4h objective")
	}
	// Nothing has passed four hours yet, so the objective holds.
	h, err := w.room.ExceptionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Met || h.Overdue != 0 {
		t.Fatalf("objective missed while nothing is overdue: %+v", h)
	}
}

// TestOldestParkedIsMeasuredAgainstTheObjective is the CC7 metric. A count
// says how much there is to do; only the oldest item says whether anything is
// being done, and an exception queue with an unbounded oldest-item age is the
// finding written for you.
func TestOldestParkedIsMeasuredAgainstTheObjective(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()

	parkAs(t, w, "wi-forgotten", queue.SeverityMedium, "nobody has looked at this")
	w.advance(30 * time.Minute)
	parkAs(t, w, "wi-recent", queue.SeverityHigh, "gate could not conclude")

	h, err := w.room.ExceptionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Parked != 2 || h.ObjectiveSec != 3600 || !h.Met {
		t.Fatalf("inside the objective: %+v", h)
	}
	if h.Oldest != "wi-forgotten" {
		t.Fatalf("oldest = %q, want wi-forgotten", h.Oldest)
	}
	if h.BySeverity[queue.SeverityHigh] != 1 || h.BySeverity[queue.SeverityMedium] != 1 {
		t.Fatalf("severity breakdown: %+v", h.BySeverity)
	}

	// Past the objective the metric says so, and it names the item rather
	// than only raising a number.
	w.advance(45 * time.Minute)
	h, err = w.room.ExceptionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Met || h.Overdue != 1 || h.Oldest != "wi-forgotten" {
		t.Fatalf("objective missed and not reported: %+v", h)
	}
	if h.OldestAgeSec < 4500 {
		t.Fatalf("oldest age = %ds, want at least 75m", h.OldestAgeSec)
	}
}

// TestStaleExceptionGoesOutOfBand is the whole point of the issue: an
// exception nobody reads is identical to no exception, and in-band
// notification guarantees nobody reads it. Nothing leaves the room before the
// objective, and what leaves carries enough to act on.
func TestStaleExceptionGoesOutOfBand(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	told := &recorder{}
	esc := &Escalator{Room: w.room, Notify: told}

	parkAs(t, w, "wi-1", queue.SeverityHigh, "gate could not conclude")

	// Inside the objective the queue is the notification.
	if sent, err := esc.Sweep(ctx); err != nil || len(sent) != 0 {
		t.Fatalf("escalated before the objective: %d %v", len(sent), err)
	}
	w.advance(61 * time.Minute)
	sent, err := esc.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || told.count() != 1 {
		t.Fatalf("stale exception did not leave the room: sent=%d told=%d", len(sent), told.count())
	}
	n := sent[0]
	if n.Item != "wi-1" || n.Severity != queue.SeverityHigh || n.Nth != 1 {
		t.Fatalf("notice: %+v", n)
	}
	if n.Reason != "gate could not conclude" || n.ObjectiveSec != 3600 || n.AgeSec < 3660 {
		t.Fatalf("a page that tells nobody anything: %+v", n)
	}

	// Sweeping again straight away must not tell anybody twice.
	if sent, err := esc.Sweep(ctx); err != nil || len(sent) != 0 {
		t.Fatalf("re-told without waiting another objective: %d %v", len(sent), err)
	}

	// The telling is on the chain, carrying the objective in force at the
	// time — an auditor reads what the factory was measuring itself against
	// then, not what it is configured with today.
	kinds := map[string]int{}
	for rec, err := range w.room.Store.Log().Since(ctx, tn, escalationScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[rec.Kind]++
		if rec.Subject != "wi-1" || rec.Actor != "chris" {
			t.Fatalf("escalation record: %+v", rec)
		}
	}
	if kinds[escalationKind] != 1 {
		t.Fatalf("escalation chain: %v", kinds)
	}

	// And the room shows what it has already said, so an operator opening the
	// queue does not re-raise something already raised.
	got, err := w.room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Escalations != 1 || !got[0].Overdue {
		t.Fatalf("the room does not show what it told: %+v", got)
	}
}

// TestReescalationIsBounded. An exception nobody answers must not become an
// alarm nobody can silence — that is how a page gets muted, and a muted page
// is worse than no page because it looks like coverage.
func TestReescalationIsBounded(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	told := &recorder{}
	esc := &Escalator{Room: w.room, Notify: told, Max: 2}

	parkAs(t, w, "wi-1", queue.SeverityCritical, "the forge is unreachable")
	// Ten objectives pass with nobody answering; three tellings, then quiet.
	for range 10 {
		w.advance(61 * time.Minute)
		if _, err := esc.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if told.count() != 3 {
		t.Fatalf("told %d times, want 1 + 2 re-escalations", told.count())
	}
	if last := told.sent[2]; !last.Final || last.Nth != 3 {
		t.Fatalf("the last telling does not say it is the last: %+v", last)
	}
	// Bounded telling is not bounded existence: the item is still parked and
	// still counted against the objective.
	h, err := w.room.ExceptionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Parked != 1 || h.Met {
		t.Fatalf("a silenced exception vanished from the metric: %+v", h)
	}

	// A negative bound is "tell me once", never "tell me forever": the one
	// reading an operator asking for fewer re-escalations cannot have meant.
	quiet := &recorder{}
	once := &Escalator{Room: w.room, Notify: quiet, Max: -1}
	for range 5 {
		w.advance(61 * time.Minute)
		if _, err := once.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if quiet.count() != 0 {
		t.Fatalf("Max=-1 kept telling an already-told exception %d more times", quiet.count())
	}
	parkAs(t, w, "wi-2", queue.SeverityCritical, "and another one")
	for range 5 {
		w.advance(61 * time.Minute)
		if _, err := once.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if quiet.count() != 1 {
		t.Fatalf("Max=-1 told %d times, want exactly one", quiet.count())
	}
}

// TestQuietExceptionsStayInTheRoom. Severity is what makes routing possible;
// without the threshold every exception has to be shouted or none can be.
func TestQuietExceptionsStayInTheRoom(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	told := &recorder{}
	esc := &Escalator{Room: w.room, Notify: told} // default threshold: medium

	parkAs(t, w, "wi-quiet", queue.SeverityLow, "a lint nit somebody can look at whenever")
	parkAs(t, w, "wi-loud", queue.SeverityMedium, "gate: fail")
	w.advance(2 * time.Hour)
	sent, err := esc.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].Item != "wi-loud" {
		t.Fatalf("threshold not honoured: %+v", sent)
	}
	// The quiet one is still an exception; nobody was woken for it.
	got, err := w.room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a low-severity exception was dropped rather than merely unshouted: %+v", got)
	}
}

// TestNoNotifierEscalatesNothing. The notifier is a seam, not a dependency:
// a room wired without one still ages, still measures, and still refuses to
// pretend it told anybody.
func TestNoNotifierEscalatesNothing(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	esc := &Escalator{Room: w.room}

	parkAs(t, w, "wi-1", queue.SeverityCritical, "the forge is unreachable")
	w.advance(5 * time.Hour)
	if sent, err := esc.Sweep(ctx); err != nil || len(sent) != 0 {
		t.Fatalf("sweep with no notifier: %d %v", len(sent), err)
	}
	for rec, err := range w.room.Store.Log().Since(ctx, tn, escalationScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("recorded a telling nobody made: %+v", rec)
	}
	h, err := w.room.ExceptionHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Met || h.Overdue != 1 {
		t.Fatalf("an unescalated exception stopped being late: %+v", h)
	}
}

// TestAFailedNotifierIsReportedAndStillCounted. An alerting channel that is
// down is an operational problem in its own right, not a licence to retry
// until somebody mutes the factory.
func TestAFailedNotifierIsReportedAndStillCounted(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	told := &recorder{err: errors.New("smtp: connection refused")}
	esc := &Escalator{Room: w.room, Notify: told}

	parkAs(t, w, "wi-1", queue.SeverityHigh, "gate could not conclude")
	w.advance(61 * time.Minute)
	if _, err := esc.Sweep(ctx); err == nil {
		t.Fatal("a notifier that failed was reported as a success")
	}
	// Counted, so a dead channel cannot spin the bound forever.
	if _, err := esc.Sweep(ctx); err != nil {
		t.Fatalf("retried immediately after a failed telling: %v", err)
	}
	if told.count() != 1 {
		t.Fatalf("told %d times within one objective", told.count())
	}
}

// TestRetryStartsTheClockAndTheTallyAgain. A parking is the unit, not the
// item: an item retried and parked again is a fresh wait a person owes, and
// inheriting the previous parking's tally would have it born already silenced.
func TestRetryStartsTheClockAndTheTallyAgain(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	told := &recorder{}
	esc := &Escalator{Room: w.room, Notify: told, Max: -1} // one telling per parking

	parkAs(t, w, "wi-1", queue.SeverityHigh, "gate could not conclude")
	w.advance(61 * time.Minute)
	if _, err := esc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if told.count() != 1 {
		t.Fatalf("first parking told %d times", told.count())
	}

	// An operator retries it; it parks again an hour later.
	if _, err := w.queue.Retry(ctx, tn, "wi-1", "chris", "the gate was flaky"); err != nil {
		t.Fatal(err)
	}
	w.advance(time.Minute)
	item, err := w.queue.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := w.queue.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.queue.Park(ctx, item, lease, queue.SeverityHigh, "the gate failed again"); err != nil {
		t.Fatal(err)
	}

	got, err := w.room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Escalations != 0 || got[0].AgeSec > 60 {
		t.Fatalf("the re-parking inherited the old clock or tally: %+v", got)
	}
	w.advance(61 * time.Minute)
	if _, err := esc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if told.count() != 2 {
		t.Fatalf("the second parking was never escalated: told %d", told.count())
	}
}

func ids(xs []Exception) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.ID
	}
	return out
}
