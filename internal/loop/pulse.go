package loop

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// pulseFraction bounds how often [Heartbeat.Pulse] spends a store round trip
// renewing: only once more than TTL/pulseFraction has elapsed since the last
// successful renewal for a run. Four leaves three renewal windows of margin
// before the claim lease's nominal deadline — frequent enough that a run
// talking every few seconds never goes quiet for a whole TTL, infrequent
// enough that a busy build is not spending a renewal on every single frame.
const pulseFraction = 4

// PulseHold is what one run registers with a [Heartbeat]: the live lease
// snapshot to renew on every pulse, and what to do when the claim lease
// specifically fails to renew.
type PulseHold struct {
	// Held is the same tracker [Loop.renewOnResume] populates — read live on
	// every Pulse, not copied at registration, so a claim retaken mid-run
	// (Retain) or a section widened later is renewed under its current
	// lease, not a stale one.
	Held *heldLeases
	// Leaser renews and inspects. The run's own l.Store.Leaser(l.Runner).
	Leaser forge.Leaser
	// TTL is the lease lifetime a successful renewal restores, and the base
	// pulseFraction divides for the throttle.
	TTL time.Duration
	// Record writes one evidence entry on the run's own chain. Called at
	// most once per run, for the "lease-lost" case. May be nil.
	Record func(ctx context.Context, kind, subject, body string)
	// Interrupt cancels the build running under this run's id, mirroring
	// [Loop.reapGhost]'s use of [Loop.Interrupt]. May be nil — no one
	// listening is every Loop that predates this field.
	Interrupt func(id string)
}

// Heartbeat is a run-keyed renewal registry: [Loop.RunHolding] tracks a run
// here the moment it starts holding a claim, right beside held.set("claim",
// claim), and the daemon's per-frame progress hook drives Pulse once per
// recorded frame — the fix for issue #211, a claim lease renewed once at
// checkout and never again while a build or review keeps talking well past
// its TTL.
//
// Because item.ID is also the reviewer gate's ChangeSet.Run, one Track call
// covers both the builder's frames and the reviewer's for free. A planner
// run — keyed "plan-<ingress>" — is never Tracked, so a pulse naming one is
// a silent no-op: see Pulse.
//
// A nil *Heartbeat is a harmless no-op throughout — the [SuspendWatch] /
// [Loop.PRLander] precedent every optional Loop field already follows — so a
// Loop that never sets Pulse behaves exactly as it did before this type
// existed.
//
// What this does NOT cover: a gate.Command check gate streams no frames
// while its subprocess runs, so a long silent check still leans on issue
// #267's 3x-build-timeout claim TTL margin rather than on Pulse. Renew on
// progress covers a run that talks; the margin covers one that legitimately
// cannot.
type Heartbeat struct {
	// Now reads the clock, for a test that must not wait in real time. Nil
	// means time.Now — the [SuspendWatch] precedent.
	Now func() time.Time

	mu    sync.Mutex
	byRun map[string]*pulseEntry
}

// pulseEntry is one tracked run's registration plus the state Pulse needs
// across calls: when it last renewed, and whether it has already recorded
// this run's claim as lost.
type pulseEntry struct {
	hold PulseHold

	mu   sync.Mutex
	last time.Time // zero: never renewed yet
	lost bool      // a "lease-lost" record for this run has already been written
}

// Track registers run with h so a later Pulse(ctx, run) renews everything
// hold.Held currently holds. detach removes the registration; call it
// alongside [Loop.renewOnResume]'s own detachRenewal defer. A nil receiver
// returns a no-op detach, so a caller need not branch on whether l.Pulse is
// configured.
func (h *Heartbeat) Track(run string, hold PulseHold) (detach func()) {
	if h == nil {
		return func() {}
	}
	h.mu.Lock()
	if h.byRun == nil {
		h.byRun = make(map[string]*pulseEntry)
	}
	h.byRun[run] = &pulseEntry{hold: hold}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.byRun, run)
		h.mu.Unlock()
	}
}

// Pulse is driven once per frame the daemon records for run. It is a no-op
// for a nil receiver, an untracked run (see [Heartbeat]'s doc on planner
// runs), or a call inside the throttle window (see pulseFraction).
// Otherwise it renews every lease run currently holds with hold.TTL — the
// [Loop.renewOnResume] shape, not just the claim — and classifies a claim
// renewal failure specifically:
//
//   - Renew succeeds: done.
//   - Renew fails, [forge.Leaser.Inspect] reports the row still outstanding
//     under this run's own token: "still mine, just slow" — the settle
//     fence's own case (queue.go's fence doc comment). Renew already
//     refuses a merely-expired, unsuperseded row by design, so there is
//     nothing to re-extend; this logs and waits for the next pulse.
//   - Renew fails, Inspect reports the row outstanding under a DIFFERENT
//     token: a successor claimed this item while run kept spending. A
//     "lease-lost" record is written once, naming the ref and the new
//     holder, and hold.Interrupt stops the spend.
//   - Renew fails, Inspect reports the row released (ok=false): a racing
//     settle, or resolve/retry left it released. There is no holder to
//     name — the settle fence already guards the outcome — so this logs
//     only.
//   - Renew fails, Inspect itself errors: unknown never reads as zero, so
//     this logs and waits rather than guessing either way.
//
// A non-claim lease that fails to renew (a section, the workspace, the land
// window) is logged and never fatal, exactly as [Loop.renewOnResume] treats
// the same failure on a resume.
func (h *Heartbeat) Pulse(ctx context.Context, run string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	e, ok := h.byRun[run]
	h.mu.Unlock()
	if !ok {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := h.now()
	if !e.last.IsZero() && now.Sub(e.last) < e.hold.TTL/pulseFraction {
		return
	}
	e.last = now

	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()

	for key, lease := range e.hold.Held.keyed() {
		if _, err := e.hold.Leaser.Renew(rctx, lease, e.hold.TTL); err != nil {
			if key == "claim" {
				h.classifyClaimLoss(rctx, run, e, lease, err)
				continue
			}
			slog.WarnContext(ctx, "loop: pulse lease renew", "key", key, "ref", lease.Ref, "run", lease.Run, "err", err)
		}
	}
}

// classifyClaimLoss runs when Pulse's own renewal of the claim lease fails.
// See Pulse's doc for the four cases this tells apart.
func (h *Heartbeat) classifyClaimLoss(ctx context.Context, run string, e *pulseEntry, claim forge.Lease, renewErr error) {
	if e.lost {
		// Already recorded and interrupted for this run; a repeated frame
		// after that must not write a second record (issue #211).
		return
	}
	outstanding, ok, err := e.hold.Leaser.Inspect(ctx, claim.Tenant, claim.Repo, claim.Ref)
	if err != nil {
		slog.WarnContext(ctx, "loop: pulse claim renew failed, inspect also failed",
			"ref", claim.Ref, "run", run, "renew_err", renewErr, "inspect_err", err)
		return
	}
	switch {
	case !ok:
		slog.WarnContext(ctx, "loop: pulse claim renew failed, claim released",
			"ref", claim.Ref, "run", run, "err", renewErr)
	case outstanding.Token == claim.Token:
		slog.WarnContext(ctx, "loop: pulse claim renew failed, still outstanding under our own token",
			"ref", claim.Ref, "run", run, "err", renewErr)
	default:
		e.lost = true
		body, _ := json.Marshal(struct {
			Ref    string `json:"ref"`
			Holder string `json:"holder_run"`
			Runner string `json:"holder_runner"`
		}{claim.Ref, outstanding.Run, outstanding.Runner})
		if e.hold.Record != nil {
			e.hold.Record(ctx, "lease-lost", claim.Ref, string(body))
		}
		if e.hold.Interrupt != nil {
			e.hold.Interrupt(run)
		}
	}
}

func (h *Heartbeat) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// trackPulse registers item's run with l.Pulse, if configured — see
// [Loop.RunHolding], right beside held.set("claim", claim). A nil l.Pulse
// returns a no-op detach, the same [Loop.renewOnResume] shape, so this call
// is unconditional at its one call site rather than branching on whether
// renewal-awareness is configured.
func (l *Loop) trackPulse(item queue.WorkItem, held *heldLeases) (detach func()) {
	if l.Pulse == nil {
		return func() {}
	}
	return l.Pulse.Track(item.ID, PulseHold{
		Held:   held,
		Leaser: l.Store.Leaser(l.Runner),
		TTL:    l.TTL,
		Record: func(ctx context.Context, kind, subject, body string) {
			l.record(ctx, item, kind, subject, body)
		},
		Interrupt: l.Interrupt,
	})
}
