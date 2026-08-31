// Package admission verifies that a repository's own configuration still
// enforces what this system relies on, before the app is allowed to change it.
//
// Every safety property here — nothing lands ungated, no self-approval, no
// force push over history — is enforced by the *forge*, using configuration the
// control plane does not own at the moment it acts. Someone with admin can
// switch required checks off, and from then on the gate is decorative: runs
// still produce verdicts, and nothing makes the forge care.
//
// So configuration is checked rather than assumed, close to the action rather
// than at enrollment, and a repository that fails is closed to agents until it
// is fixed.
package admission

import (
	"context"
	"time"
)

// Restriction is a property a repository's configuration must have.
type Restriction string

const (
	// RestrictProtected requires branch protection or a ruleset on the
	// integration branch.
	RestrictProtected Restriction = "protected"
	// RestrictRequiredChecks requires our gate among the checks that must pass
	// before a merge. A gate the forge does not require is advice.
	RestrictRequiredChecks Restriction = "required-checks"
	// RestrictNoAdminBypass requires that protection apply to administrators
	// too. A rule with an exemption is a rule for whoever lacks the exemption.
	RestrictNoAdminBypass Restriction = "no-admin-bypass"
	// RestrictAppNotBypassing requires that *this app* is not a bypass actor.
	//
	// This is the subtle one, and the easiest to get wrong for the most
	// reasonable-sounding motive. Adding the app to a ruleset's bypass list
	// makes landing simpler — it can merge without waiting. It also means the
	// only actor that matters here is the one actor the rules do not apply to.
	// The app merges through the normal path once checks pass; when it must
	// bypass, that is break-glass, under an incident, evidenced.
	RestrictAppNotBypassing Restriction = "app-not-bypassing"
	// RestrictNoForcePush requires force pushes to be refused on protected
	// branches, so revert-forward stays the only way history changes.
	RestrictNoForcePush Restriction = "no-force-push"
	// RestrictNoDeletion requires deletion protection on the integration
	// branch.
	RestrictNoDeletion Restriction = "no-deletion"
	// RestrictReviewCount requires the review count a Box's approval policy
	// demands to be configured at the forge as well, so the requirement holds
	// even for a change that does not pass through this control plane.
	RestrictReviewCount Restriction = "review-count"
	// RestrictAppPermissions requires the app's installed permissions on this
	// repository to match what is expected. Permissions that widened are drift
	// worth catching wherever it came from.
	RestrictAppPermissions Restriction = "app-permissions"
)

// Finding is one restriction that does not hold.
type Finding struct {
	Restriction Restriction
	// Detail says what was observed, so the fix is obvious without a second
	// investigation.
	Detail string
}

// Result is the outcome of checking a repository.
type Result struct {
	Repo string
	// Required is what was checked, recorded so a later reader knows whether a
	// clean result means "all good" or "we barely looked".
	Required []Restriction
	Findings []Finding
	Checked  time.Time
}

// Admitted reports whether every required restriction held.
func (r Result) Admitted() bool { return len(r.Findings) == 0 }

// Fresh reports whether the result is recent enough to act on.
//
// Configuration is mutable by people outside this system, so an admission
// result is a statement about a moment, not a property of the repository. Long
// caching turns this control into a memory of a control.
func (r Result) Fresh(now time.Time, max time.Duration) bool {
	return now.Sub(r.Checked) <= max
}

// Controller evaluates repository configuration.
//
// Implementations must **fail closed**: an API error, a rate limit, or any
// state they cannot determine is a refusal, never an admission. An availability
// problem must not become an authorization bypass, which is the way this class
// of check usually fails in practice.
type Controller interface {
	// Check evaluates a repository against the required restrictions.
	Check(ctx context.Context, repo string, required []Restriction) (Result, error)
}

// Phase is when admission is being evaluated.
//
// Checking on every call would exhaust forge rate limits, and checking once at
// enrollment would be stale within the hour. Two moments earn the call: before
// a run takes a lease, so effort is not spent on a repository it cannot land
// in; and before landing, which is the moment the forge's enforcement is
// actually load-bearing. The second is not optional even if the first passed —
// configuration can change during a run, and a long agent run is exactly the
// window in which it would.
type Phase string

const (
	PhasePropose Phase = "propose"
	PhaseLand    Phase = "land"
)
