package approval

import (
	"context"
	"fmt"

	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// GateAdapter is the human gate as a [forge.Gate]: it answers pending until
// the required approvals cover the head, which is what the three-state
// Decision exists for — "not yet" is not "no", and waiting is not parked.
//
// Under the compliance boundary this gate appears in exactly one place: the
// promotion to production. Everything below that boundary is autonomous, so
// an optimistic lane that finds this gate in its set is misconfigured and
// the loop refuses it outright.
type GateAdapter struct {
	Label       string
	Requirement Requirement
	// Requester is who asked for the change; the requester's approvals do
	// not count, directly or through an agent acting for them.
	Requester authz.Actor
	// Head names the commit approvals must cover. Approval binds to what the
	// reviewer saw, and for a promotion that is the CANDIDATE being promoted,
	// not the env commit that carries it. Nil defaults to the ChangeSet's
	// last member head.
	Head func(cs forge.ChangeSet) string
	// List returns the approvals currently on record for a head. Read-only:
	// Evaluate is called repeatedly while pending and must never solicit.
	List func(ctx context.Context, head string) ([]Approval, error)
}

// Name identifies the gate.
func (g GateAdapter) Name() string {
	if g.Label != "" {
		return g.Label
	}
	return "human-approval"
}

// Evaluate counts distinct, non-self, head-covering approvals against the
// requirement. Short: pending, with the tally in the reason. Never fail —
// a missing approval is a person who has not decided yet, and there is no
// "no" to record until policy adds one.
func (g GateAdapter) Evaluate(ctx context.Context, cs forge.ChangeSet) (forge.Verdict, error) {
	if g.Requirement.Count == 0 {
		return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPass,
			Reason: "machine-gated: no human approval required at this boundary"}, nil
	}
	head := ""
	if g.Head != nil {
		head = g.Head(cs)
	} else if n := len(cs.Members); n > 0 {
		head = string(cs.Members[n-1].Head)
	}
	if head == "" {
		return forge.Verdict{}, fmt.Errorf("approval: nothing to approve — no head resolved")
	}
	apps, err := g.List(ctx, head)
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("approval: listing approvals for %s: %w", head, err)
	}
	if len(g.Requirement.Sources) > 0 {
		var kept []Approval
		for _, a := range apps {
			for _, s := range g.Requirement.Sources {
				if a.Source == s {
					kept = append(kept, a)
					break
				}
			}
		}
		apps = kept
	}
	n := Distinct(apps, forge.Commit(head), g.Requester)
	if n >= g.Requirement.Count {
		return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPass,
			Reason: fmt.Sprintf("%d distinct approval(s) cover %s", n, head)}, nil
	}
	return forge.Verdict{Gate: g.Name(), Decision: forge.DecisionPending,
		Reason: fmt.Sprintf("waiting on approvals: %d of %d cover %s (stale and self-approvals do not count)", n, g.Requirement.Count, head)}, nil
}
