// Package estate is what the control plane knows about the repositories that
// actually exist, as opposed to the ones it was told about.
//
// A Box enrols repositories, which is a declaration. This package observes
// them, which is a fact. The distinction matters most for campaigns: a campaign
// is a predicate over repositories, so a hand-maintained repository list makes
// "every service is on Go 1.25" quietly mean "every service someone remembered
// to enrol". The gap does not announce itself, which is why [Registry.Unenrolled]
// exists — an unenrolled repository is a reportable state rather than silence.
package estate

import (
	"context"
	"iter"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Repo is a repository as observed at its host.
type Repo struct {
	Tenant tenant.ID
	// Host, Owner and Name together address the repository. Host is carried
	// because the estate is not assumed to be one forge forever.
	Host  string
	Owner string
	Name  string

	DefaultBranch string
	Archived      bool
	Private       bool
	// Observed is when this record was last confirmed against the host. An
	// estate is only as true as its last sweep.
	Observed time.Time
}

// Enrollment binds an observed repository to a Box and a role.
//
// Enrollment is separate from observation so that the two can disagree
// visibly. A repository can exist and not be enrolled, or be enrolled and have
// been deleted at the host; both are conditions worth surfacing, and neither is
// expressible if the two are the same record.
type Enrollment struct {
	// Repo is the repository's canonical key: host/owner/name, built from
	// the observed [Repo]'s address fields. Every package that names a
	// repository by a single string uses this form; a forge.Repo.Name, by
	// contrast, is local to its Box.
	Repo string
	Box  string
	// Role is the forge role; see [forge.Role] for the closed set.
	Role forge.Role
}

// Fact is something the census learned about a repository.
//
// Facts are what campaign predicates read: "every repository whose go.mod is
// below 1.25", "every service without required status checks". Keys are
// namespaced by collector so two collectors cannot disagree silently about what
// a key means.
type Fact struct {
	// Repo is the repository's canonical key (host/owner/name); see
	// [Enrollment.Repo].
	Repo string
	// Key is dotted and namespaced, e.g. "go.version",
	// "branch-protection.required-checks", "container.base-image".
	Key   string
	Value string
	// Collector names what produced this fact, so a wrong fact leads back to
	// the thing that got it wrong.
	Collector string
	Observed  time.Time
}

// Stale reports whether a fact is older than the given age.
//
// Predicates must consider staleness rather than trusting the last value seen.
// A campaign that acts on a stale fact does work that was already done, or
// skips work that is newly needed — and at fleet scale it does so silently
// across many repositories at once.
func (f Fact) Stale(now time.Time, max time.Duration) bool {
	return now.Sub(f.Observed) > max
}

// Census discovers repositories and describes them.
//
// Implementations talk to a forge. They are read-only by construction: nothing
// here may change the estate it is measuring.
type Census interface {
	// Discover enumerates the repositories visible to this tenant at the host.
	Discover(ctx context.Context, t tenant.ID) iter.Seq2[Repo, error]
	// Collect gathers facts about one repository.
	Collect(ctx context.Context, r Repo) ([]Fact, error)
}

// Registry is the recorded estate.
type Registry interface {
	// Repos returns every observed repository for a tenant.
	Repos(ctx context.Context, t tenant.ID) iter.Seq2[Repo, error]
	// Facts returns what is known about one repository within a tenant.
	Facts(ctx context.Context, t tenant.ID, repo string) ([]Fact, error)
	// Unenrolled returns observed repositories belonging to no Box.
	//
	// This is the method that keeps the estate honest. Work is only ever
	// generated for enrolled repositories, so anything here is outside every
	// campaign, every gate, and every policy — invisible to the factory while
	// being entirely visible to an attacker.
	Unenrolled(ctx context.Context, t tenant.ID) ([]Repo, error)
	// Missing returns enrolled repositories no longer observed at the host,
	// the opposite failure: policy that believes it covers something gone.
	Missing(ctx context.Context, t tenant.ID) ([]Enrollment, error)
}
