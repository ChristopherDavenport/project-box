// Package approval implements the human gate.
//
// Most gates in a dark factory are machines, and that is deliberate — an
// automated gate that cannot be bypassed is a stronger control than a human
// review that is famously rubber-stamped. But some changes warrant a person
// regardless: production infrastructure, security-sensitive paths, a campaign's
// first wave, a break-glass invocation. This package covers those.
//
// Approvals arrive from two sources. GitHub pull request reviews reuse an
// interface reviewers already have, with notifications, CODEOWNERS and required
// reviewer counts already solved. Direct attestation covers the decisions that
// have no pull request behind them — approving a deployment, a rollout wave, or
// an emergency bypass.
package approval

import (
	"context"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Source is where an approval came from.
type Source string

const (
	// SourceGitHub is a pull request review.
	SourceGitHub Source = "github"
	// SourceDirect is an attestation recorded through the control room, for
	// decisions with no pull request behind them.
	SourceDirect Source = "direct"
)

// Approval is one person's decision.
type Approval struct {
	// Tenant owns this approval. An approval is an authority artifact and
	// grants root in a tenant; one tenant's approval must be indistinguishable
	// from absent in any other.
	Tenant tenant.ID
	// Subject is the authenticated approver.
	Subject string
	// Commit is the exact revision approved.
	//
	// Approval binds to content, never to a pull request. A review approves
	// the head it saw, and an agent that pushes again afterwards holds an
	// approval for something nobody looked at — which is why GitHub has
	// dismiss-stale-reviews and why this field is checked rather than assumed.
	Commit forge.Commit
	// PlanRef is the exact plan approved: a sha256 digest computed by
	// Plan.Seal (see internal/plan.Plan.Ref), never a name that can move.
	//
	// A plan approved and then re-decomposed is not the plan that runs, the
	// same way a commit approved and then force-pushed is not the revision
	// that runs — so this binds to the sealed digest, and an approval names
	// either a Commit or a PlanRef, never derives one from the other.
	PlanRef string
	// Repo is the repository's canonical key: host/owner/name (see
	// estate.Enrollment.Repo).
	Repo   string
	Source Source
	// ExternalID is the review id when Source is SourceGitHub, so the approval
	// can be traced back to the artifact a reviewer actually saw.
	ExternalID string
	At         time.Time
}

// Requirement is what a Box's policy demands before a ChangeSet may land.
//
// Requirements are derived from what the change touches — repository roles,
// paths, blast radius, whether it is a campaign's first wave — rather than set
// per change, so that the answer to "did this need a human" does not depend on
// who was asked.
type Requirement struct {
	// Count is how many distinct approvers are needed. Zero means the change
	// is machine-gated, which is the common case.
	Count int
	// Roles restricts who may approve. Empty means any principal holding the
	// approve grant for the resource.
	Roles []string
	// Sources restricts where approvals may come from. Empty accepts either.
	Sources []Source
}

// Checker decides whether the approvals in hand satisfy a requirement.
type Checker interface {
	// Satisfied reports whether the approvals cover head, and why not when
	// they do not. The reason is recorded on the verdict, so a reviewer who
	// approved the wrong revision learns that rather than being ignored.
	Satisfied(ctx context.Context, req Requirement, head forge.Commit, requester authz.Actor, approvals []Approval) (satisfied bool, reason string, err error)
}

// SelfApproved reports whether an approval was given by the principal who
// requested the work, directly or as the delegator behind an agent.
//
// Self-approval is the failure a separation-of-duties control exists to
// prevent, and an agent acting on behalf of a person inherits that person's
// disqualification — otherwise the rule is evaded by routing the change through
// an automation.
func SelfApproved(a Approval, requester authz.Actor) bool {
	if a.Subject == requester.Principal.Subject {
		return true
	}
	return requester.OnBehalfOf != nil && a.Subject == requester.OnBehalfOf.Subject
}

// Distinct counts approvals that cover head and are not self-approvals.
//
// Approvals of a superseded revision are not counted at all rather than counted
// and warned about: a stale approval is indistinguishable from no approval for
// the content actually being landed.
func Distinct(approvals []Approval, head forge.Commit, requester authz.Actor) int {
	seen := make(map[string]struct{}, len(approvals))
	for _, a := range approvals {
		if a.Commit != head || SelfApproved(a, requester) {
			continue
		}
		seen[a.Subject] = struct{}{}
	}
	return len(seen)
}

// DistinctForPlan counts approvals that name ref and are not self-approvals.
//
// Approvals of a superseded plan are not counted at all rather than counted
// and warned about: a stale approval is indistinguishable from no approval
// for the plan actually being run.
func DistinctForPlan(approvals []Approval, ref string, requester authz.Actor) int {
	seen := make(map[string]struct{}, len(approvals))
	for _, a := range approvals {
		if a.PlanRef != ref || SelfApproved(a, requester) {
			continue
		}
		seen[a.Subject] = struct{}{}
	}
	return len(seen)
}
