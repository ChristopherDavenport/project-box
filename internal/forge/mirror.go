package forge

import "context"

// Mirror is a bare clone of a member repository kept on one runner and shared
// by every run on that runner. Sharing the mirror keeps fetches cheap; sharing
// a *working tree* is what this package exists to prevent, so each run gets its
// own [Workspace] materialized from the mirror.
type Mirror interface {
	// Fetch updates the mirror from the repo's remote.
	Fetch(ctx context.Context, repo Repo) error
	// Resolve turns a ref into the commit it currently names. Callers resolve
	// once, at lease time, and refer to the [Commit] thereafter.
	Resolve(ctx context.Context, repo Repo, ref string) (Commit, error)
	// Checkout materializes a workspace at a commit. The lease must be live;
	// implementations present its [Token] so that a runner which stalled past
	// its deadline cannot quietly acquire a second tree over the same ref.
	Checkout(ctx context.Context, l Lease, at Commit) (Workspace, error)
	// Prune removes workspaces whose leases are gone, reclaiming the disk of
	// runs that died without releasing.
	Prune(ctx context.Context) error
}

// Workspace is an isolated working tree belonging to exactly one (run, repo).
// Nothing else writes to it for the lifetime of the run, which is the property
// that makes concurrent agents safe to operate on one Box.
type Workspace struct {
	Repo string
	Run  string
	// Dir is the absolute path of the working tree.
	Dir string
	// Base is the commit the tree was materialized at, and the value a landing
	// presents as the expected old value of the branch.
	Base Commit
	// Lease is the claim that authorizes this tree to exist. When it lapses
	// the tree must be abandoned, not pushed.
	Lease Lease
}
