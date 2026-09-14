package loop

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// SuspendWatch detects a machine suspend by comparing the wall clock against
// a monotonic clock across ticks: a suspend stalls a process's monotonic
// clock (or at least does not advance it by the suspended duration) while
// the wall clock keeps moving, so a wall delta that outruns the monotonic
// delta since the last observation by more than a tick is time the machine
// was gone, not time any run spent — see Observe.
//
// Wall and Mono are separate injectable sources, deliberately not one
// combined clock, so a test can walk the wall clock forward by the length
// of a suspend while the monotonic clock barely advances, without needing
// to fabricate Go's own opaque monotonic reading (time.Time carries one
// internally, but nothing outside the runtime can construct or inspect it
// directly). Both default to time.Now when nil: two separate calls, one
// Round(0)-stripped to pure wall time and one left alone so its Sub against
// the previous call's reading uses Go's real monotonic clock — see the Go
// subtlety noted on Observe.
//
// internal/loop is the shared home for this rather than a new top-level
// package: internal/agent/acp already imports internal/loop (for
// [Escalation] and its kin) and internal/daemon is expected to import it
// too, to drive Observe from the tick loop, while internal/loop imports
// neither — so this is the one place both reach without minting a package
// that sits in no declared section of docs/PLAN.md §6.
type SuspendWatch struct {
	// Wall reads the wall clock. Nil means time.Now.
	Wall func() time.Time
	// Mono reads a clock whose Sub against an earlier reading from the same
	// func measures real elapsed time, suspend excluded (or not — see the
	// platform caveat on Observe). Nil means time.Now.
	Mono func() time.Time

	mu       sync.Mutex
	have     bool
	lastWall time.Time
	lastMono time.Time
	total    time.Duration
	nextSub  int
	subs     map[int]func(time.Duration)
}

// Observe is called once per daemon tick (by the unit that drives this
// watcher from the tick loop). It reads both clocks and compares their
// deltas since the previous call: gap = wallDelta - monoDelta. When gap
// exceeds threshold — the caller's own tick interval, so ordinary
// scheduling jitter within one tick never counts as a suspend — that gap is
// a suspend of that length. It is added to the cumulative total (see
// Total), every subscriber is notified with it before Observe returns (see
// Subscribe), and it is also the return value, so the caller can record it
// itself.
//
// The Go subtlety this exists to route around: time.Time.Sub uses the
// monotonic reading when both operands carry one, so naively subtracting
// two time.Now() values measures the monotonic clock, not the wall clock —
// the wrong clock for THIS purpose, whichever way that clock happens to
// behave across a suspend. Getting the wall delta requires stripping the
// monotonic reading from both operands first (Round(0)); getting the
// monotonic delta requires NOT stripping it. Observe does both, from two
// independent readings, rather than one naive Sub of either.
//
// The first call after construction has nothing to compare against and
// always reports zero, recording only the baseline.
//
// Platform caveat: on a platform where Go's monotonic clock reading itself
// stalls across a suspend exactly as much as the wall clock does — true of
// some, not of Linux's CLOCK_MONOTONIC, which by definition excludes
// suspended time, unlike CLOCK_BOOTTIME — wallDelta and monoDelta advance
// together and gap never exceeds threshold: no suspend is ever detected.
// That is honest, not a bug: on such a platform the deadline this watcher
// exists to protect genuinely did elapse, because the operating system
// charged the suspended time against the monotonic clock too.
func (r *SuspendWatch) Observe(threshold time.Duration) time.Duration {
	wall := r.wallNow().Round(0)
	mono := r.monoNow()

	r.mu.Lock()
	if !r.have {
		r.lastWall, r.lastMono, r.have = wall, mono, true
		r.mu.Unlock()
		return 0
	}
	wallDelta := wall.Sub(r.lastWall)
	monoDelta := mono.Sub(r.lastMono)
	r.lastWall, r.lastMono = wall, mono

	gap := wallDelta - monoDelta
	if gap <= threshold {
		r.mu.Unlock()
		return 0
	}
	r.total += gap
	notify := make([]func(time.Duration), 0, len(r.subs))
	for _, fn := range r.subs {
		notify = append(notify, fn)
	}
	r.mu.Unlock()

	// Called outside the lock: a subscriber renewing leases (see
	// [Loop.renewOnResume]) does real work, including store round-trips,
	// and holding this mutex across that would block every other Observe
	// caller and every Subscribe/detach for the duration.
	for _, fn := range notify {
		fn(gap)
	}
	return gap
}

// Total is the cumulative suspend time this watcher has ever observed, so a
// run can snapshot it at attempt start and later ask "did a resume happen
// since" by comparing against a fresh read.
func (r *SuspendWatch) Total() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

// Subscribe registers fn to be called, synchronously and in Observe's own
// goroutine, with the gap every time Observe detects a suspend. The
// returned detach removes it; a run in flight must call detach when it
// returns — an undetached subscription outlives the run it was for and
// keeps firing against whatever it closed over, the same shape as any other
// goroutine leak in a long-lived daemon process.
func (r *SuspendWatch) Subscribe(fn func(gap time.Duration)) (detach func()) {
	r.mu.Lock()
	id := r.nextSub
	r.nextSub++
	if r.subs == nil {
		r.subs = make(map[int]func(time.Duration))
	}
	r.subs[id] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

func (r *SuspendWatch) wallNow() time.Time {
	if r.Wall != nil {
		return r.Wall()
	}
	return time.Now()
}

func (r *SuspendWatch) monoNow() time.Time {
	if r.Mono != nil {
		return r.Mono()
	}
	return time.Now()
}

// heldLeases is the leases one [Loop.RunHolding] call currently holds,
// tracked so a resume notification (see [SuspendWatch.Subscribe]) can renew
// exactly what is live at the moment it fires. It exists because that
// notification fires from whatever goroutine called [SuspendWatch.Observe]
// — almost certainly not this run's own — while the set of leases a claim
// holds changes shape as Run progresses: a widened section, a claim lease
// retaken for another attempt (see [Loop.RunHolding]'s q.Retain calls), a
// land lease taken only once gates pass. A plain local variable read
// without a lock under that would race.
type heldLeases struct {
	mu    sync.Mutex
	byKey map[string]forge.Lease
}

func (h *heldLeases) set(key string, lease forge.Lease) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byKey == nil {
		h.byKey = make(map[string]forge.Lease)
	}
	h.byKey[key] = lease
}

// setSections registers every lease of a section grant, keyed by section
// name under its own namespace so it cannot collide with "claim",
// "workspace" or "land".
func (h *heldLeases) setSections(leases map[string]forge.Lease) {
	for name, lease := range leases {
		h.set("section:"+name, lease)
	}
}

func (h *heldLeases) snapshot() []forge.Lease {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]forge.Lease, 0, len(h.byKey))
	for _, lease := range h.byKey {
		out = append(out, lease)
	}
	return out
}

// renewOnResume subscribes a fresh [heldLeases] tracker to l.Suspend so
// that every lease it currently tracks is renewed, with l.TTL, the moment a
// resume is detected. Renewing with the full TTL from post-resume now is
// strictly at least "extended by the gap" — the gap is bounded by however
// long the suspend actually was, and a fresh full-TTL renewal buys more
// than that without this having to compute remaining-plus-gap itself.
//
// A nil l.Suspend still returns a usable tracker and a no-op detach, so
// [Loop.RunHolding] can call held.set unconditionally on both the
// resume-aware and the plain path instead of branching on whether Suspend
// is configured at every call site.
func (l *Loop) renewOnResume(ctx context.Context) (held *heldLeases, detach func()) {
	held = &heldLeases{}
	if l.Suspend == nil {
		return held, func() {}
	}
	detach = l.Suspend.Subscribe(func(time.Duration) {
		leaser := l.Store.Leaser(l.Runner)
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		for _, lease := range held.snapshot() {
			// A lease a deferred release already dropped renews against a
			// row that is gone — [forge.ErrExpired] or
			// [forge.ErrStaleToken] — which fails harmlessly here: the run
			// that held it is unwinding either way, and failing the run
			// for a renewal it no longer needs would be the exact ghost
			// this package already treats as harmless elsewhere (issue
			// #162). Logged, never fatal.
			if _, err := leaser.Renew(rctx, lease, l.TTL); err != nil {
				slog.WarnContext(ctx, "loop: lease renew after resume", "ref", lease.Ref, "run", lease.Run, "err", err)
			}
		}
	})
	return held, detach
}
