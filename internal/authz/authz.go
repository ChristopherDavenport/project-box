// Package authz decides who may do what.
//
// Identity is ordinary: principals authenticate through OIDC and carry grants,
// with no per-actor keypairs and no signature scheme. That is a deliberate
// narrowing. Per-actor signing only buys something when the control plane is
// outside the trust boundary, and for a factory whose operator is the party
// being audited it is not — the accepted controls there are access control,
// hash chaining, and WORM retention, none of which need signatures.
//
// The trade is a good one in both directions. Traditional permissioning
// *enforces* segregation of duties where key separation only *attributes* it:
// a builder that lacks the land grant cannot land, rather than landing and
// being identifiable afterwards.
//
// # Accepted risk
//
// A record only the control plane vouches for cannot survive a dishonest
// operator. This is the same position CloudTrail-style audit logging takes and
// is accepted practice, but it is accepted rather than absent. If it ever needs
// closing, periodic anchoring of evidence chain heads to something external is
// the cheap answer, and it needs no per-actor identity.
package authz

import (
	"context"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Kind distinguishes what sort of principal is acting.
type Kind string

const (
	// KindUser is a human, authenticated interactively.
	KindUser Kind = "user"
	// KindAgent is an agent runtime, authenticated as a workload.
	KindAgent Kind = "agent"
	// KindService is the control plane itself or one of its components.
	KindService Kind = "service"
)

// Principal is an authenticated identity.
type Principal struct {
	// Subject is the OIDC subject, stable across credential rotation.
	Subject string
	Kind    Kind
	Tenant  tenant.ID
	// Roles held by this principal, resolved at authentication time.
	Roles []string
}

// Actor is who is acting, and on whose authority.
//
// Agents act *on behalf of* a person rather than *as* one. Acting as the user
// would collapse two facts an audit needs kept apart — whether a change was
// made by a human or by an automation working for them — and there is no way to
// recover the distinction afterwards from a log that never drew it.
//
// Autonomous work still has an answer here: a campaign's opener or the Box's
// service principal stands behind work with no human in the moment. "Who
// authorized this 3am change" must never resolve to nobody.
type Actor struct {
	Principal Principal
	// OnBehalfOf is the principal whose authority is being exercised, nil when
	// the actor acts on its own.
	OnBehalfOf *Principal
}

// Action is a thing that can be done.
type Action string

const (
	ActionRead           Action = "read"    // materialize context
	ActionPropose        Action = "propose" // take a lease, produce a ChangeSet
	ActionGate           Action = "gate"    // issue a verdict
	ActionLand           Action = "land"    // move an integration branch
	ActionDeploy         Action = "deploy"  // write an environment repository
	ActionWriteKnowledge Action = "write:knowledge"
	ActionApprove        Action = "approve" // attest a human approval; see internal/approval
	ActionOpenIngress    Action = "open:ingress"
	ActionBreakGlass     Action = "break-glass"
	ActionAdminTenant    Action = "admin:tenant"
)

// Resource is what an action is taken against. Zero fields are wildcards, so a
// grant naming only a tenant covers every Box within it.
type Resource struct {
	Tenant tenant.ID
	Box    string
	Repo   string
	// RepoRole scopes a grant by what a repository is rather than by name, so
	// authority over services does not imply authority over environments.
	// Values are the forge role vocabulary; see forge.Role for the closed
	// set.
	RepoRole string
	Env      string
}

// Decision is an authorization outcome. The reason is carried on allow as well
// as deny, because an evidence record showing *why* an action was permitted is
// worth more at audit than one showing only that it was.
type Decision struct {
	Allow  bool
	Reason string
	// Grant identifies which grant permitted the action, empty on deny.
	Grant string
}

// Authorizer answers whether an actor may take an action.
//
// Implementations must apply delegation as an intersection and never a union:
// an agent's effective authority is the least of its own grants, the grants of
// the principal it acts for, and the Box's policy. Delegation that can escalate
// is not delegation.
//
// A missing grant denies. There is no implicit allow, and an empty grant set is
// an inert principal rather than an unconstrained one.
type Authorizer interface {
	Can(ctx context.Context, a Actor, action Action, r Resource) (Decision, error)
}
