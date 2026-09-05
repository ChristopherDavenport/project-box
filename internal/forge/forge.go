// Package forge manages the git substrate of a Box: mirrors of every member
// repository, exclusive leases over the refs a run intends to move, isolated
// workspaces to move them in, and the gate that decides whether a change
// spanning several repositories may land.
//
// A Box is a system rather than a repository, so the unit of change here is the
// [ChangeSet]: a set of branches, one per affected repository, reviewed and
// landed together. Deployment is not a separate concept — a ChangeSet whose
// member has [RoleEnv] is a deployment, reconciled into a cluster by Argo CD or
// Flux rather than applied by this process.
package forge

import (
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Role classifies what a repository contributes to a Box. Policy is written
// against roles, not names: an agent granted authority over [RoleService] may
// not thereby touch the environment repositories that decide what runs in
// production.
type Role string

const (
	// RoleService is a deployable unit that produces an image.
	RoleService Role = "service"
	// RoleLibrary is consumed by other members at build time.
	RoleLibrary Role = "library"
	// RoleContract is an interface definition others generate from.
	RoleContract Role = "contract"
	// RoleChart holds Kubernetes manifests or Helm charts, unpinned to any
	// particular environment.
	RoleChart Role = "chart"
	// RoleInfra holds infrastructure definitions: clusters, networks, managed
	// services.
	RoleInfra Role = "infra"
	// RoleEnv is the desired state of one environment, reconciled into a
	// cluster by an external GitOps controller. Landing here is deploying.
	RoleEnv Role = "env"
)

// Repo is the forge layer's working view of a repository enrolled in a Box —
// a projection of the estate's enrollment record, derived from it rather than
// edited independently.
type Repo struct {
	// Name identifies the repo within its Box and is stable across remote
	// moves.
	Name string
	// Remote is the fetch and push URL.
	Remote string
	Role   Role
	// DefaultBranch is the integration branch changes land on.
	DefaultBranch string
}

// Commit is a resolved 40-character object ID. Refs are resolved to commits at
// the moment a lease is granted so that everything downstream — gates,
// landings, provenance — refers to content that cannot move underneath it.
type Commit string

// ChangeSet is the unit of change in a Box: branches across one or more
// repositories that must be gated and landed as a whole.
//
// Members land in slice order, which must be a topological order of their
// dependencies. There is no atomic commit across git remotes, so a ChangeSet is
// additionally required to be safe when landed partially — each member additive
// on its own, with removals deferred to a later ChangeSet. See [Lander] for
// what happens when a landing stops halfway.
type ChangeSet struct {
	ID string
	// Tenant owns this change. It is carried explicitly rather than derived
	// from Box: an identifier that must be resolved before it can be trusted
	// is one an attacker can supply.
	Tenant tenant.ID
	// Box and Run tie the ChangeSet to the system it belongs to and the run
	// that produced it, forming the middle of the provenance chain.
	Box string
	Run string
	// Intent is the human-readable reason this change exists, carried through
	// to the commit messages and the evidence records.
	Intent string
	// DrivingIssue is the GitHub issue number this change addresses (e.g.
	// "42"), empty when the change did not originate from a GitHub issue. A
	// caller wiring an issue-driven route populates it; a PR opened for the
	// change (see git.PRLander) links it with "Fixes #<n>" in the PR body.
	DrivingIssue string
	Members      []Member
	Created      time.Time
}

// Member is one repository's participation in a [ChangeSet].
type Member struct {
	Repo string
	// Branch is the ref this member proposes to move.
	Branch string
	// Base is the commit Head was built on, and the value presented as the
	// expected old value when landing.
	Base Commit
	// Head is the commit to land. Empty until the run has produced work.
	Head Commit
	// Lease is the claim held over Branch. A member without a live lease
	// cannot land.
	Lease Lease
}
