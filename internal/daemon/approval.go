package daemon

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ChristopherDavenport/project-box/internal/approval"
	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
)

// requestEnsurer makes sure the pull request a human is being asked to
// review exists before the gate ever asks — production wires this to
// [gitforge.PRLander.EnsureRequest].
type requestEnsurer func(ctx context.Context, cs forge.ChangeSet, mem forge.Member) (string, error)

// reviewsSupplier reads back the reviews recorded against a ChangeSet
// member's pull request — production wires this to
// [gitforge.PRLander.Reviews]. A named type rather than the bare function
// signature so tests can stand a fake in without importing gitforge.
type reviewsSupplier func(ctx context.Context, cs forge.ChangeSet, mem forge.Member) ([]gitforge.Review, error)

// humanApprovalGate is the daemon's [forge.Gate] for a repo naming
// approval.count > 0.
//
// Evaluate does two things approval.GateAdapter alone cannot: it ensures the
// pull request the reviews are collected on actually exists (the gated
// lane reaches gates BEFORE it ever opens one — see [gitforge.PRLander.Land]
// — so a gate that only read reviews would go pending on a request that
// would never exist), and it maps the request's reviews onto
// approval.Approval before delegating the pass/pending decision to a
// GateAdapter. The soliciting lives here, in Evaluate, and not in the
// adapter's List — gate.go's own doc comment states List is read-only, and
// internal/approval sits in no declared section, so changing it there would
// cost whole-repository scheduling.
type humanApprovalGate struct {
	requirement approval.Requirement
	requester   authz.Actor
	ensure      requestEnsurer
	reviews     reviewsSupplier
}

// Name identifies the gate in a [forge.Verdict] and the audit trail.
func (g *humanApprovalGate) Name() string { return "human-approval" }

// Evaluate ensures the request exists for cs's head, then delegates to a
// approval.GateAdapter whose List maps that request's APPROVED reviews to
// approval.Approval and nothing else — a COMMENTED, CHANGES_REQUESTED or
// DISMISSED review is not an approval, and silence never reads as one.
func (g *humanApprovalGate) Evaluate(ctx context.Context, cs forge.ChangeSet) (forge.Verdict, error) {
	if len(cs.Members) == 0 {
		return forge.Verdict{}, fmt.Errorf("approval: empty ChangeSet")
	}
	mem := cs.Members[len(cs.Members)-1]
	if _, err := g.ensure(ctx, cs, mem); err != nil {
		return forge.Verdict{}, fmt.Errorf("approval: ensure request: %w", err)
	}
	adapter := approval.GateAdapter{
		Label: g.Name(), Requirement: g.requirement, Requester: g.requester,
		List: func(ctx context.Context, head string) ([]approval.Approval, error) {
			reviews, err := g.reviews(ctx, cs, mem)
			if err != nil {
				return nil, err
			}
			out := make([]approval.Approval, 0, len(reviews))
			for _, r := range reviews {
				if r.State != "APPROVED" {
					continue
				}
				out = append(out, approval.Approval{
					Tenant: cs.Tenant, Subject: r.Login, Commit: forge.Commit(r.CommitID),
					Repo: mem.Repo, Source: approval.SourceGitHub,
					ExternalID: strconv.FormatInt(r.ID, 10), At: r.SubmittedAt,
				})
			}
			return out, nil
		},
	}
	return adapter.Evaluate(ctx, cs)
}
