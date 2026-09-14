package loop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// pulseFixture is a fake-clocked store plus one submitted-and-claimed work
// item, for driving [Heartbeat.Pulse] directly as the stand-in for the
// daemon's per-frame progress hook — none of these tests need a full
// [Loop.Run], only the queue and lease machinery Pulse touches.
type pulseFixture struct {
	store *sqlite.Store
	q     *queue.Queue
	now   time.Time
}

func newPulseFixture(t *testing.T, runner string, ttl time.Duration) *pulseFixture {
	t.Helper()
	f := &pulseFixture{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(func() time.Time { return f.now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f.store = s
	f.q = &queue.Queue{Store: s, Runner: runner, TTL: ttl}
	return f
}

func (f *pulseFixture) submitAndClaim(t *testing.T, id string) (queue.WorkItem, forge.Lease) {
	t.Helper()
	ctx := context.Background()
	item, err := f.q.Submit(ctx, queue.WorkItem{ID: id, Tenant: tn, Ingress: "ing-" + id, Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := f.q.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	return item, lease
}

// TestPulseKeepsAClaimLeaseAliveAcrossThreeTTLsAndRetainSucceeds is the
// issue's first case: the 2026-09-13 incident was a build and a review that
// together outran the claim's TTL many times over, silently — this proves
// frames arriving every TTL/pulseFraction keep the lease's expiry moving
// forward, and that q.Retain — the exact call that answered "forge: lease
// expired" in the incident — succeeds afterward instead.
func TestPulseKeepsAClaimLeaseAliveAcrossThreeTTLsAndRetainSucceeds(t *testing.T) {
	const ttl = 4 * time.Second
	f := newPulseFixture(t, "runner-1", ttl)
	ctx := context.Background()
	item, lease := f.submitAndClaim(t, "wi-1")

	held := &heldLeases{}
	held.set("claim", lease)
	hb := &Heartbeat{Now: func() time.Time { return f.now }}
	var records int
	detach := hb.Track(item.ID, PulseHold{
		Held:   held,
		Leaser: f.store.Leaser("runner-1"),
		TTL:    ttl,
		Record: func(context.Context, string, string, string) { records++ },
		Interrupt: func(string) {
			t.Fatal("Interrupt must not fire while the claim lease keeps renewing cleanly")
		},
	})
	defer detach()

	prevExpires := lease.Expires
	// Three TTLs of fake-clock advance, one "frame" every second — well
	// inside pulseFraction's throttle window (ttl/4 = 1s), so every pulse
	// here renews.
	for i := 0; i < int(3*ttl/time.Second); i++ {
		f.now = f.now.Add(time.Second)
		hb.Pulse(ctx, item.ID)

		got, ok, err := f.store.Leaser("runner-1").Inspect(ctx, tn, queue.ClaimRepo, queue.ClaimRef(item.ID))
		if err != nil || !ok {
			t.Fatalf("inspect after pulse %d: ok=%v err=%v", i, ok, err)
		}
		if !got.Expires.After(prevExpires) {
			t.Fatalf("pulse %d: expiry did not advance: got %v, previous %v", i, got.Expires, prevExpires)
		}
		if got.Token != lease.Token {
			t.Fatalf("pulse %d: token changed under a clean renewal: got %v, want %v", i, got.Token, lease.Token)
		}
		prevExpires = got.Expires
	}
	if records != 0 {
		t.Fatalf("recorded %d evidence entries, want 0 — nothing was ever lost", records)
	}

	// The incident's own failure: a claim this stale, renewed only at
	// checkout, could not Retain into a second attempt. With Pulse keeping
	// it alive throughout, it can.
	if _, _, err := f.q.Retain(ctx, item, lease, nil, 2); err != nil {
		t.Fatalf("Retain after three TTLs of pulsing: %v", err)
	}
}

// TestPulseRecordsLeaseLostOnceAndInterruptsWhenASuccessorClaims is the
// issue's second case: a run goes silent past its TTL, a second [queue.Queue]
// under a different runner claims the same item (Acquire on the expired,
// unsuperseded row bumps the token rather than refusing), and the first
// run's next Pulse must recognize it lost the claim rather than merely being
// slow.
func TestPulseRecordsLeaseLostOnceAndInterruptsWhenASuccessorClaims(t *testing.T) {
	const ttl = 4 * time.Second
	f := newPulseFixture(t, "runner-1", ttl)
	ctx := context.Background()
	item, lease := f.submitAndClaim(t, "wi-2")

	held := &heldLeases{}
	held.set("claim", lease)
	hb := &Heartbeat{Now: func() time.Time { return f.now }}
	var bodies []string
	var interrupted []string
	detach := hb.Track(item.ID, PulseHold{
		Held:   held,
		Leaser: f.store.Leaser("runner-1"),
		TTL:    ttl,
		Record: func(_ context.Context, kind, subject, body string) {
			bodies = append(bodies, kind+"|"+subject+"|"+body)
		},
		Interrupt: func(id string) { interrupted = append(interrupted, id) },
	})
	defer detach()

	// The first run goes silent well past its TTL: no Pulse call at all
	// while the clock advances.
	f.now = f.now.Add(ttl + time.Second)

	// A second queue, a different runner, claims the same item. The claim
	// row is expired and unsuperseded, so Acquire reclaims it and bumps the
	// token rather than refusing (sqlite's documented Acquire-on-expired
	// behavior) — the successor the incident's retry would have been.
	q2 := &queue.Queue{Store: f.store, Runner: "runner-2", TTL: ttl}
	loaded, err := q2.Load(ctx, tn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := q2.Claim(ctx, loaded); err != nil {
		t.Fatalf("second claimant's Claim on the expired row: %v", err)
	}

	// The first run's next pulse.
	hb.Pulse(ctx, item.ID)
	if len(bodies) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1: %v", len(bodies), bodies)
	}
	if !strings.HasPrefix(bodies[0], "lease-lost|") {
		t.Fatalf("record kind/subject = %q, want a lease-lost record", bodies[0])
	}
	if !strings.Contains(bodies[0], `"holder_runner":"runner-2"`) {
		t.Fatalf("lease-lost body does not name the second claimant's runner: %s", bodies[0])
	}
	if len(interrupted) != 1 || interrupted[0] != item.ID {
		t.Fatalf("interrupted = %v, want exactly one call naming %q", interrupted, item.ID)
	}

	// A repeated frame after the loss must not write a second record or
	// interrupt a second time — advance past the throttle window so this
	// pulse genuinely attempts a renewal rather than being skipped by it.
	f.now = f.now.Add(ttl)
	hb.Pulse(ctx, item.ID)
	if len(bodies) != 1 {
		t.Fatalf("a repeated frame after the loss re-recorded: now %d entries: %v", len(bodies), bodies)
	}
	if len(interrupted) != 1 {
		t.Fatalf("a repeated frame after the loss interrupted again: %v", interrupted)
	}
}

// TestPulseDuringALongGateEvaluationLetsRetainSucceedAfterward is the
// issue's third case: a gate whose own evaluation outlasts the claim TTL —
// here a fake [forge.Gate] that pulses as it "evaluates", standing in for
// the daemon calling Pulse on every frame a real gate's run emits — must
// leave the claim renewable afterward, exactly as a build does.
func TestPulseDuringALongGateEvaluationLetsRetainSucceedAfterward(t *testing.T) {
	const ttl = 4 * time.Second
	f := newPulseFixture(t, "runner-1", ttl)
	ctx := context.Background()
	item, lease := f.submitAndClaim(t, "wi-3")

	held := &heldLeases{}
	held.set("claim", lease)
	hb := &Heartbeat{Now: func() time.Time { return f.now }}
	detach := hb.Track(item.ID, PulseHold{
		Held:      held,
		Leaser:    f.store.Leaser("runner-1"),
		TTL:       ttl,
		Interrupt: func(string) { t.Fatal("Interrupt must not fire during a clean, pulsing gate evaluation") },
	})
	defer detach()

	g := pulsingGate{hb: hb, fixture: f, frames: int(2*ttl/time.Second) + 1}
	verdict, err := g.Evaluate(ctx, forge.ChangeSet{Run: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != forge.DecisionPass {
		t.Fatalf("verdict = %v, want pass", verdict.Decision)
	}

	if _, _, err := f.q.Retain(ctx, item, lease, nil, 2); err != nil {
		t.Fatalf("Retain after a gate evaluation longer than the TTL: %v", err)
	}
}

// pulsingGate is [forge.Gate] evaluated by advancing the fixture's fake
// clock and calling Pulse once per simulated frame before answering — the
// shape a real gate's daemon-observed progress would drive Pulse in.
type pulsingGate struct {
	hb      *Heartbeat
	fixture *pulseFixture
	frames  int
}

func (pulsingGate) Name() string { return "pulsing" }

func (g pulsingGate) Evaluate(ctx context.Context, cs forge.ChangeSet) (forge.Verdict, error) {
	for i := 0; i < g.frames; i++ {
		g.fixture.now = g.fixture.now.Add(time.Second)
		g.hb.Pulse(ctx, cs.Run)
	}
	return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPass}, nil
}

// TestPulseThrottlesRenewalWithinTheFractionWindow: two pulses arriving
// inside the pulseFraction window must renew once, not twice — the whole
// reason Pulse throttles rather than renewing on every single frame.
func TestPulseThrottlesRenewalWithinTheFractionWindow(t *testing.T) {
	const ttl = 4 * time.Second
	f := newPulseFixture(t, "runner-1", ttl)
	ctx := context.Background()
	item, lease := f.submitAndClaim(t, "wi-4")

	held := &heldLeases{}
	held.set("claim", lease)
	counting := &countingLeaser{Leaser: f.store.Leaser("runner-1")}
	hb := &Heartbeat{Now: func() time.Time { return f.now }}
	detach := hb.Track(item.ID, PulseHold{Held: held, Leaser: counting, TTL: ttl})
	defer detach()

	hb.Pulse(ctx, item.ID) // first pulse ever: always renews

	f.now = f.now.Add(ttl / 8) // well inside ttl/pulseFraction (1s)
	hb.Pulse(ctx, item.ID)

	if counting.renews != 1 {
		t.Fatalf("two pulses inside the fraction window performed %d renewals, want 1", counting.renews)
	}
}

// countingLeaser counts Renew calls so throttling can be asserted directly
// rather than inferred from Expires, which a same-instant renewal would not
// visibly move.
type countingLeaser struct {
	forge.Leaser
	renews int
}

func (c *countingLeaser) Renew(ctx context.Context, l forge.Lease, ttl time.Duration) (forge.Lease, error) {
	c.renews++
	return c.Leaser.Renew(ctx, l, ttl)
}

// TestPulseForAnUnregisteredRunIsANoop: a planner run (keyed
// "plan-<ingress>") is never Tracked, so a pulse naming it must do nothing
// and never panic.
func TestPulseForAnUnregisteredRunIsANoop(t *testing.T) {
	hb := &Heartbeat{}
	hb.Pulse(context.Background(), "plan-some-ingress")
}

// TestNilHeartbeatPulseAndTrackAreNoops: every Loop that predates this field
// leaves Pulse unset, so a nil *Heartbeat must behave like an unconfigured
// one everywhere it is used.
func TestNilHeartbeatPulseAndTrackAreNoops(t *testing.T) {
	var hb *Heartbeat
	detach := hb.Track("run", PulseHold{})
	detach()
	hb.Pulse(context.Background(), "run")
}
