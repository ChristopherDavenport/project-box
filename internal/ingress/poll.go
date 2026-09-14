package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Issue is what a [Lister] reports about one open item at the forge — the
// fields a delivery's task text is built from.
type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
}

// Lister lists a forge repository's open issues. target is forge-specific
// and opaque to this package — a GitHub-backed Lister expects a github.com
// remote URL, and internal/ingress never parses it. label is advisory: a
// Lister may use it server-side to narrow the request, but [Poller] filters
// on the labels the listing actually returned regardless, so a Lister that
// ignores label entirely still cannot open work for an unlabeled issue.
type Lister interface {
	List(ctx context.Context, target, label string) ([]Issue, error)
}

// Poller is one route's reader: it lists a forge's open issues and opens a
// delivery for each labeled one through the same [Router.Open] a hand-run
// `box submit` uses, so redelivery is the one idempotency layer that already
// exists — a second poll of an issue already opened reports opened=false
// rather than a duplicate ingress.
type Poller struct {
	Lister Lister
	Router Router
	Log    evidence.Log
	// Tenant and Actor stamp the evidence this poller writes on a listing
	// failure. They are not read off the [Route] passed to Poll: a route's
	// own Tenant field is set only in tests today (StoreRouter is bound to
	// its own tenant at construction and never reads it), so a poller needs
	// its own, the same way StoreRouter does.
	Tenant tenant.ID
	Actor  string
}

// Poll runs one poll of route against target, opening a delivery for every
// open issue the listing returns that carries route.Label. now is the
// poll's own time, stamped onto every delivery it opens so the occurrence
// recorded is the poll's, not whenever intake later reads it.
//
// A listing error is a RECORDED failure — appended as a "poll-failed"
// record on a standing scope naming the route — and opens nothing: an
// unreachable forge must never read as "no work". The append happening
// before the error returns is deliberate; a failure that could not even be
// recorded is a louder problem than one that was.
func (p *Poller) Poll(ctx context.Context, route Route, target string, now time.Time) error {
	issues, err := p.Lister.List(ctx, target, route.Label)
	if err != nil {
		return p.recordFailure(ctx, route, target, err)
	}
	for _, iss := range issues {
		if !hasLabel(iss.Labels, route.Label) {
			continue
		}
		if _, _, err := p.Router.Open(ctx, route, Delivery{
			Key:      fmt.Sprintf("issue-%d", iss.Number),
			Title:    iss.Title,
			Payload:  []byte(taskText(iss, route.Contract)),
			Received: now,
			Source:   Source{Subject: p.Actor, Method: "poll", Channel: "route:" + route.Name},
		}); err != nil {
			return fmt.Errorf("ingress: poll %s: opening issue #%d: %w", route.Name, iss.Number, err)
		}
	}
	return nil
}

func (p *Poller) recordFailure(ctx context.Context, route Route, target string, listErr error) error {
	body, _ := json.Marshal(map[string]string{"route": route.Name, "target": target, "error": listErr.Error()})
	if _, err := p.Log.Append(ctx, evidence.Record{
		Tenant: p.Tenant, Scope: pollScope(route.Name), Kind: RecordPollFailed,
		Subject: route.Name, Actor: p.Actor, Body: body,
	}); err != nil {
		return fmt.Errorf("ingress: poll %s failed (%w) and the failure could not be recorded: %w", route.Name, listErr, err)
	}
	return fmt.Errorf("ingress: poll %s: %w", route.Name, listErr)
}

// pollScope is the standing chain a route's poll failures accumulate on —
// standing rather than per-entity because a poll is not about one issue, it
// is a fact about the route itself.
func pollScope(route string) string { return "route/" + route }

// RecordPollFailed is the evidence kind a poll's listing failure is
// recorded under.
const RecordPollFailed = "poll-failed"

// hasLabel reports whether labels contains label, case-sensitively — the
// same exactness GitHub's own label matching uses.
func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// taskText builds a polled issue's task text exactly as
// hack/dogfood/submit.py's did by hand: the issue number and title, its
// trimmed body, and — when the route configures one — the working contract
// after a "---" separator. A route with no contract drops both the
// separator and the contract, rather than leaving a trailing one behind.
func taskText(iss Issue, contract string) string {
	task := fmt.Sprintf("GitHub issue #%d: %s\n\n%s", iss.Number, iss.Title, strings.TrimSpace(iss.Body))
	if contract != "" {
		task += "\n\n---\n\n" + contract
	}
	return task
}
