// Package provision creates repositories and makes them safe to use.
//
// Creation is the one forge permission that cannot be scoped to repositories,
// because the repository does not exist yet — it is held at the organization
// level and is the broadest authority the control plane has. Everything else a
// run does can be narrowed to the repositories it touches; this cannot.
//
// That asymmetry shapes the package: creation is separated from ordinary work,
// defaults to requiring a human approval, and is followed immediately by
// integration rather than treated as finished.
package provision

import (
	"context"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// RepoSpec describes a repository to create.
type RepoSpec struct {
	Tenant tenant.ID
	Owner  string
	Name   string
	// Role is the forge role the repository will be enrolled as, which
	// determines what integration it needs and what authority agents can hold
	// over it.
	Role string
	Box  string
	// Template names the scaffold to render into the new repository.
	Template string
	Private  bool
}

// Integration is what must be true before a repository is usable.
//
// **Protection precedes use.** A repository that exists without required checks
// is a repository an agent can land in ungated, and creation-then-protection
// leaves exactly that window open. So a new repository starts closed — no agent
// holds a lease on it — and is opened only once [Integrator.Verify] confirms
// every item here. A partially integrated repository is not a fast start, it is
// a hole with a scaffold in it.
type Integration struct {
	// Protected is branch protection or rulesets on the integration branch.
	Protected bool
	// RequiredChecks are the gate checks that must pass before a merge.
	RequiredChecks []string
	// AppInstalled reports whether the control plane's app can reach the
	// repository at all.
	AppInstalled bool
	// Enrolled reports whether the estate records it against a Box and role.
	Enrolled bool
	// Rendered reports whether derived artifacts — chart values, environment
	// entries — have landed for a service.
	Rendered bool
}

// Complete reports whether every integration step is done. Anything short of
// complete keeps the repository closed.
func (i Integration) Complete() bool {
	return i.Protected && len(i.RequiredChecks) > 0 &&
		i.AppInstalled && i.Enrolled && i.Rendered
}

// Provisioner creates and integrates repositories.
type Provisioner interface {
	// Create makes the repository at the forge. It is not a git operation and
	// cannot be part of a ChangeSet, so it is recorded as its own evidence
	// event before any commit exists to attach to.
	Create(ctx context.Context, s RepoSpec) error
	// Integrate applies protection, checks, enrollment, and rendering. It is
	// idempotent: integration is re-run to converge a repository that drifted,
	// not only to set one up.
	Integrate(ctx context.Context, s RepoSpec) (Integration, error)
	// Verify reports current integration state without changing anything, so
	// the census can find repositories that were created and left half-done.
	Verify(ctx context.Context, s RepoSpec) (Integration, error)
}
