package forge

import (
	"context"
	"time"
)

// Lander pushes a gated [ChangeSet] to its members' remotes.
//
// Git offers no atomic commit across remotes, so a landing can stop halfway and
// the design has to survive that rather than assume it away. Two things make it
// survivable. As policy, members are required to be independently safe —
// additive on their own, with removals deferred to a later ChangeSet — so a
// partial landing degrades rather than breaks. As mechanism, members land in
// the ChangeSet's dependency order and a failure is compensated by reverting
// forward: the commits that already landed are undone by new commits, never by
// rewriting published history.
type Lander interface {
	Land(ctx context.Context, cs ChangeSet) (Landing, error)
}

// Landing records what actually happened, including the half-completed cases.
// It is the tail of the provenance chain and is recorded as evidence whether
// the landing succeeded or not.
type Landing struct {
	ChangeSet string
	// Landed lists the members that reached their remote, in the order they
	// did so.
	Landed []Placement
	// Failed names the member that stopped the landing, if any.
	Failed string
	// Compensated lists the revert-forward commits issued for members in
	// Landed after Failed stopped the run. A landing with a non-empty Failed
	// and an empty Compensated is an incident, not a rollback.
	Compensated []Placement
	Finished    time.Time
}

// Complete reports whether every member landed and nothing needed compensating.
func (l Landing) Complete() bool {
	return l.Failed == "" && len(l.Compensated) == 0
}

// Placement is one member's commit arriving on one branch: the link that ties a
// run to the content it published.
type Placement struct {
	Repo   string
	Branch string
	// Was is the commit the branch pointed at beforehand, presented as the
	// expected old value so a concurrent mover is detected rather than
	// overwritten.
	Was Commit
	Now Commit
	At  time.Time
	// PRURL is the GitHub pull request opened for this placement, empty for
	// a plain git.Lander landing and populated when a PR-opening wrapper
	// (git.PRLander) handled the land — so a caller reads the PR URL
	// straight off the returned Landing without a second query.
	PRURL string
	// IssueClosed reports whether a Lander that talks to a code host
	// (git.PRLander) confirmed GitHub actually answered a request to close
	// this ChangeSet's DrivingIssue for this placement. It is never set
	// merely because a close was attempted: an unanswered request, an
	// erroring one, or a plain git.Lander with no such capability at all all
	// leave it false, so a caller deciding whether to record the close never
	// reads unknown as done.
	IssueClosed bool
	// IssueClosedBy names who closed the issue, set only alongside
	// IssueClosed. It is the box's own actor when this landing's PATCH
	// performed the close, and GitHub's reported closed_by.login when the
	// GET found the issue already closed — the case issue #181 is about,
	// where GitHub beat this landing to the close. Unknown never reads as
	// zero: if GitHub reports closed with no closed_by, this stays empty
	// and the emptiness is itself the finding.
	IssueClosedBy string
}
