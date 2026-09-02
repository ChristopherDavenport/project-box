package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// escalationScope holds one record per out-of-band telling. It is a scope
// rather than a counter because the count has to survive a restart and be
// auditable: "who was told, when, about what, and how loudly" is the CC7
// evidence, and folding it back is also what bounds the repetition.
const escalationScope = "control/escalations"

// escalationKind is the record kind on that scope. Deliberately not
// "escalated", which is already an agent declining on a work item's own
// chain — two different acts by two different parties.
const escalationKind = "exception-escalated"

// DefaultMaxReescalations bounds the tellings after the first: Gas Town's
// max_reescalations, and the same number for the same reason.
const DefaultMaxReescalations = 2

// Notice is one out-of-band escalation: which exception, how loud, how long
// it has waited, and which telling this is.
type Notice struct {
	Tenant   tenant.ID      `json:"tenant"`
	Item     string         `json:"item"`
	Box      string         `json:"box,omitempty"`
	Severity queue.Severity `json:"severity"`
	// Reason is the park note — what the factory said when it gave up. A
	// notice without it is a page that tells nobody anything.
	Reason       string    `json:"reason,omitempty"`
	Parked       time.Time `json:"parked"`
	AgeSec       int64     `json:"age_sec"`
	ObjectiveSec int64     `json:"objective_sec"`
	// Nth is which telling this is; 1 is the first.
	Nth int `json:"nth"`
	// Final marks the last one this exception will produce, so a recipient
	// knows silence afterwards is the bound and not the resolution.
	Final bool `json:"final"`
}

// Notifier carries a notice out of band.
//
// The seam is the whole design. The room decides that somebody must be told
// and knows nothing whatever about telling them — mail, a pager, a webhook,
// a ticket in whatever the operator already reads. In-band notification is
// precisely what makes an exception queue go unread: a queue that announces
// itself only inside the control room announces itself only to people
// already in it, and an exception nobody reads is identical to no exception.
//
// So an implementation that writes back into the factory does not satisfy
// this interface in spirit, whatever the compiler says.
type Notifier interface {
	Notify(ctx context.Context, n Notice) error
}

// NotifierFunc adapts a function to [Notifier].
type NotifierFunc func(context.Context, Notice) error

// Notify satisfies [Notifier].
func (f NotifierFunc) Notify(ctx context.Context, n Notice) error { return f(ctx, n) }

// LogNotice writes a notice to the process log at error level: the weakest
// out-of-band sink there is, and the only one that needs no configuration to
// exist. It leaves the exception queue for the log stream an operator's
// alerting already watches, which is a low bar and still categorically more
// than a queue nobody opens.
//
// It is the daemon's default because the alternative default is nothing, and
// nothing is the state this whole mechanism exists to end. Replace it at the
// same seam.
func LogNotice(ctx context.Context, n Notice) error {
	slog.ErrorContext(ctx, "exception escalated",
		"item", n.Item, "severity", n.Severity, "age_sec", n.AgeSec,
		"objective_sec", n.ObjectiveSec, "nth", n.Nth, "final", n.Final,
		"reason", n.Reason)
	return nil
}

// Escalator tells somebody, out of band, about exceptions that have gone
// stale — the standing task that makes an unattended queue audible.
type Escalator struct {
	Room *Room
	// Notify is the seam. Nil escalates nothing: a choice an operator can
	// make and see, never a default anywhere this is wired.
	Notify Notifier
	// Threshold is the lowest severity that leaves the room. Empty means
	// [queue.SeverityMedium], so [queue.SeverityLow] is the explicit opt-out
	// a stage chooses and a stage that said nothing is still heard.
	Threshold queue.Severity
	// Max bounds RE-escalation: the tellings after the first. Zero means
	// [DefaultMaxReescalations]; negative means none, one telling and then
	// silence.
	//
	// The bound is why re-escalation is safe to have at all. An exception
	// nobody answers must not become an alarm nobody can silence — that is
	// how a page gets muted, and a muted page is worse than no page because
	// it looks like coverage.
	Max int
}

func (e *Escalator) threshold() queue.Severity {
	if e.Threshold == "" {
		return queue.SeverityMedium
	}
	return e.Threshold
}

// max is always a bound. Negative resolves to zero rather than to "no
// limit": the one reading an operator can never have meant by asking for
// fewer re-escalations is unlimited ones.
func (e *Escalator) max() int {
	switch {
	case e.Max == 0:
		return DefaultMaxReescalations
	case e.Max < 0:
		return 0
	}
	return e.Max
}

// Sweep tells somebody about every exception that has gone stale and returns
// the notices it sent. Safe to run on a timer: an exception already told
// about is told again only when it has waited another objective, and only
// while the bound allows.
//
// The record comes first and the notice second. The count is what bounds the
// repetition, so a telling the chain never saw is a telling that would be
// repeated forever — which makes an unrecordable escalation an unsent one,
// the same posture break-glass takes for the same reason. A notifier that
// fails is reported and still counted: an alerting channel that is down is
// an operational problem in its own right, not a licence to retry until
// somebody mutes the factory.
func (e *Escalator) Sweep(ctx context.Context) ([]Notice, error) {
	if e.Notify == nil {
		return nil, nil
	}
	items, err := e.Room.Exceptions(ctx)
	if err != nil {
		return nil, err
	}
	objective, threshold, max := e.Room.objective(), e.threshold(), e.max()
	var sent []Notice
	var errs []error
	for _, x := range items {
		severity := x.Severity.OrDefault()
		if !severity.AtLeast(threshold) {
			continue
		}
		nth := x.Escalations + 1
		if nth > max+1 {
			continue // told enough; the queue still holds it, and the metric still counts it
		}
		// The first telling at the objective, one more per objective after
		// it. Reusing the objective as the interval keeps the promise and
		// the alarm denominated in the same number.
		if time.Duration(x.AgeSec)*time.Second < time.Duration(nth)*objective {
			continue
		}
		n := Notice{
			Tenant: e.Room.Tenant, Item: x.ID, Box: x.Box, Severity: severity,
			Reason: x.Note, Parked: x.Parked, AgeSec: x.AgeSec,
			ObjectiveSec: int64(objective.Seconds()),
			Nth:          nth, Final: nth == max+1,
		}
		if err := e.Room.recordEscalation(ctx, n); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := e.Notify.Notify(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("control: telling somebody about %s: %w", x.ID, err))
		}
		sent = append(sent, n)
	}
	return sent, errors.Join(errs...)
}

// recordEscalation puts the telling on the chain before it happens, carrying
// the objective in force at the time — an auditor reads what the factory was
// measuring itself against then, not what it is configured with today.
func (r *Room) recordEscalation(ctx context.Context, n Notice) error {
	body, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("control: escalation for %s: %w", n.Item, err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: r.Tenant, Scope: escalationScope, Kind: escalationKind,
		Subject: n.Item, Actor: r.Actor, Body: body,
	}); err != nil {
		return fmt.Errorf("control: escalation for %s not recorded, so not sent: %w", n.Item, err)
	}
	return nil
}

// escalations folds the escalation chain into when each item was told. Times
// rather than counts, because a retried item that parks again starts a fresh
// parking and must not inherit the previous one's tally — see
// [Exception.Parked].
func (r *Room) escalations(ctx context.Context) (map[string][]time.Time, error) {
	out := map[string][]time.Time{}
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, escalationScope, 1) {
		if err != nil {
			return nil, err
		}
		out[rec.Subject] = append(out[rec.Subject], rec.At)
	}
	return out, nil
}

func countAfter(times []time.Time, since time.Time) int {
	n := 0
	for _, t := range times {
		if !t.Before(since) {
			n++
		}
	}
	return n
}
