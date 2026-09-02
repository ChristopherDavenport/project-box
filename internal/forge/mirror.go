package forge

import "context"

// A Mirror maintains bare clones of member repositories on one runner, shared
// by every run on that runner. Sharing the mirror keeps fetches cheap; sharing
// a *working tree* is what this package exists to prevent, so each run gets
// its own [Workspace] materialized from the mirror.
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

// ContextDir names where a run's granted context set is materialized: a
// SIBLING of the workspace, never inside it, so reading material cannot
// become part of the change.
//
// The convention lives here because two packages need to agree on it and
// neither owns it — the agent runtime materializes into it and hands the path
// to a confining wrapper, and the mirror reclaims it with the workspace.
func ContextDir(workspaceDir string) string { return workspaceDir + ".context" }

// SkillsDir names where a run's pinned skills are materialized as a generated
// plugin. A sibling for the same reason [ContextDir] is one, and separate from
// it because the two are granted independently and a confining wrapper has to
// mount them as different things: context is reference material the agent was
// handed, skills are capability it was given.
func SkillsDir(workspaceDir string) string { return workspaceDir + ".skills" }
