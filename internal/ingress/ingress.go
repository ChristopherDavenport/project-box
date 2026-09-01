// Package ingress models where work originates.
//
// Every WorkItem traces to exactly one Ingress, and the invariant is worth
// stating plainly: there is no orphan work. "Why does this change exist" always
// has an answer, which makes the ingress the root of provenance rather than a
// vague notion of intent.
//
// The kind is not a label. It sets the authority envelope, the completion
// semantics, and the priority — most consequentially, only an incident may
// invoke break-glass.
package ingress

import (
	"context"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Kind is the shape of an origination. The set is deliberately closed:
// automated origination folds into these rather than forming a fourth kind, so
// a scheduled dependency bump is a standing campaign and detected drift opens a
// low-severity incident.
type Kind string

const (
	// KindIssue is one discrete request — a bug, a feature, a task. It finishes
	// when its acceptance criteria pass.
	KindIssue Kind = "issue"
	// KindCampaign is a program of change across many repositories. It finishes
	// when its predicate holds everywhere in scope.
	KindCampaign Kind = "campaign"
	// KindIncident is something wrong now. It finishes when service is restored
	// *and* the postmortem has landed — restoration alone does not close it.
	KindIncident Kind = "incident"
)

// Ingress is one origination of work.
type Ingress struct {
	ID     string
	Kind   Kind
	Tenant tenant.ID
	Box    string
	// Title is what a human reads in the control room.
	Title string
	// Repo is the target repository, copied from the route.
	Repo string `json:"repo,omitempty"`
	// Route is the name of the route that admitted this work, and Key the
	// delivery key that identified the occurrence. Both are recorded because
	// an operator looking at a queue needs to know where a unit came from,
	// and the id hashes them past recovery. Empty on ingresses opened before
	// they were carried.
	Route string `json:"route,omitempty"`
	Key   string `json:"key,omitempty"`
	// Note carries the delivery's payload as the ingress's own words — the
	// task text intake hands the work item.
	Note string `json:"note,omitempty"`
	// Requester is the authenticated subject whose authority this work
	// inherits. Routed work copies it from the route; the external system
	// that fired the route is identified by the route and its delivery,
	// never by this field. Autonomous origination still names one: work with
	// no principal behind it has no authorization to inherit.
	Requester string
	// Depth is how many times this work has already been reopened by the
	// factory itself — a repair of a repair of a repair. Externally
	// delivered work is depth 0 and stays there; only the factory's own
	// routes increment it, and only so that a chain it starts can be
	// stopped. See loop.OpenRepair.
	Depth  int `json:"depth,omitempty"`
	Opened time.Time
	// Closed is zero while open.
	Closed time.Time
	// Campaign is set when Kind is KindCampaign, Incident when KindIncident.
	// An issue needs neither.
	Campaign *Campaign
	Incident *Incident
}

// MayBreakGlass reports whether this ingress can authorize an emergency bypass.
//
// Only an open incident can. An emergency bypass with no incident behind it is
// precisely the finding an audit looks for, so the coupling is a control: every
// invocation inherits a why, a timeline, and a postmortem obligation, and
// "show me every break-glass invocation" resolves to a list of incidents.
func (i Ingress) MayBreakGlass() bool {
	return i.Kind == KindIncident && i.Closed.IsZero()
}

// Campaign is a predicate over a Box's repositories, continuously evaluated.
//
// It is not a checklist of work created up front. Enumerating makes a campaign
// wrong the moment a repository is added or retired; reconciling makes it
// self-healing. The mental model is the deploy plane's — a campaign moves
// repository state toward a predicate the way a reconciler moves cluster state
// toward a manifest.
type Campaign struct {
	// Predicate is what "done" means, evaluated per repository.
	Predicate string
	// Roles narrows which repositories are considered by forge role, Repos
	// by canonical name. Both empty means every repository in the Box. Two
	// fields rather than one mixed list: a repository named "service" must
	// never be confusable with the role.
	Roles []forge.Role
	Repos []string
	// Rollout throttles this campaign's landings.
	Rollout Rollout
}

// Rollout throttles a campaign.
//
// A campaign is the most dangerous ingress, because one bad change lands
// everywhere at once. It is throttled for the same reason a deployment is.
type Rollout struct {
	// Batch is how many repositories may be in flight at once. Zero means one,
	// not unlimited — an unset rollout must never mean an unthrottled one.
	Batch int
	// Soak is how long a landed wave must hold before the next begins.
	Soak time.Duration
	// HaltAfter stops the campaign once this many waves have failed. Zero
	// halts on the first failure.
	HaltAfter int
}

// Severity orders incidents against each other and against ordinary work.
// Higher is more urgent.
type Severity int

// Incident carries what closing one requires.
type Incident struct {
	Severity Severity
	// Restored is when service came back. Setting it does not close the
	// incident.
	Restored time.Time
	// Postmortem references the evidence record that closes this out. An
	// incident with a Restored time and no postmortem is still open, which is
	// the point.
	Postmortem string
	// BrokeGlass records whether an emergency bypass was actually used, so the
	// control room can list them without scanning the evidence chain.
	BrokeGlass bool
}

// RouteKind is a way work enters. Adding a source of work should be
// configuration, not a new code path, so the set is open where [Kind] is
// closed — many routes, still only three things they can open.
type RouteKind string

const (
	RouteIssue    RouteKind = "forge-issue" // an issue opened at the forge
	RouteWebhook  RouteKind = "webhook"     // push, pull request, release
	RouteSchedule RouteKind = "schedule"    // standing campaigns
	RouteAlert    RouteKind = "alert"       // monitoring, paging
	RouteScanner  RouteKind = "scanner"     // vulnerability and policy scans
	RouteCensus   RouteKind = "census"      // the estate disagreeing with policy
	RouteDrift    RouteKind = "drift"       // the reconciler disagreeing with git
	RouteRedMain  RouteKind = "red-main"    // post-land checks disagreeing with an optimistic branch
)

// Route is a configured path from an external event to an [Ingress].
//
// RouteCensus deserves note: the estate is itself a source of work. A
// repository missing required checks, running an unsupported runtime, or
// enrolled nowhere is a fact the census produces and a campaign consumes, which
// closes the loop — the same sweep that tells us what exists tells us what is
// wrong with it.
type Route struct {
	// Tenant owns this route, and every ingress it opens. Routes are
	// configured per tenant; work cannot be opened into a tenant the route
	// does not name.
	Tenant tenant.ID
	Name   string
	Kind   RouteKind
	// Opens is which ingress kind this route produces. A route opens exactly
	// one kind: a scanner that sometimes files an issue and sometimes raises an
	// incident is two routes, so that each has its own policy.
	Opens Kind
	// Repo names the repository this route's work targets, when the Box has
	// more than one. Like everything else on a route it is operator
	// configuration: a caller names a route and inherits its target, never
	// chooses one.
	Repo string
	// Box scopes what this route may open work against. It is required: a
	// route is a standing authority grant, and one that could open work in
	// every Box would have unbounded blast radius.
	Box string
	// Requester is the principal whose authority routed work inherits. A route
	// with no principal behind it can open nothing — autonomous work still has
	// to be authorized by someone.
	Requester string
}

// Delivery is one firing of a route.
//
// External sources redeliver. Webhooks retry, alerts flap, scanners re-report
// the same finding every run, and a schedule that overlaps its own execution
// fires twice. Without idempotency a flapping alert opens incidents until
// something falls over, so [Delivery.Key] is required and deduplication is the
// route's problem rather than the operator's.
type Delivery struct {
	Route string
	// Key identifies the external occurrence, not the delivery attempt. Two
	// deliveries carrying the same key are the same event and must produce one
	// ingress.
	Key string
	// Payload is the source's own representation, retained so a wrongly routed
	// event can be diagnosed against what actually arrived.
	Payload []byte
	// Depth is set only by the factory reopening its own work; see
	// [Ingress.Depth]. An external delivery leaves it zero.
	Depth    int
	Received time.Time
}

// Router turns deliveries into ingresses.
type Router interface {
	// Open records a delivery and returns the ingress it opened, which
	// inherits the route's Tenant, Box, and Requester. A repeated key returns
	// the existing ingress with opened false, rather than an error:
	// redelivery is normal traffic, not a fault.
	Open(ctx context.Context, r Route, d Delivery) (ing Ingress, opened bool, err error)
}

// Evaluator answers which repositories a campaign still has work in.
//
// Implementations must be side-effect free: evaluation runs continuously and
// must be safe to call when the answer will be discarded.
type Evaluator interface {
	// Unsatisfied returns the repositories in scope that do not yet satisfy the
	// predicate. An empty result means the campaign is complete — for now,
	// since a new repository can reopen it.
	Unsatisfied(ctx context.Context, c Campaign) ([]string, error)
}
