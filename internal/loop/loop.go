// Package loop wires the first closed loop: an opened issue becomes a work
// item, a builder produces a change in a leased workspace, gates judge it,
// and the lander pushes it — unattended, with evidence at every transition
// and the whole chain queryable by the landed commit.
//
// This is the phase that validates or kills the premise: everything before it
// is scaffolding, everything after is scale.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// Builder produces a change in a workspace and returns the commit to land.
// The ACP runtime satisfies this for real agents; tests satisfy it with a
// scripted change — the loop cannot tell, which is the point of the seam.
type Builder interface {
	Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error)
}

// Gates builds the gate set for a materialized workspace. A closure rather
// than a slice because gates run IN the workspace the builder wrote.
type Gates func(ws forge.Workspace) []forge.Gate

// Loop runs work items end to end for one repo of one Box.
type Loop struct {
	Store   store.Store
	Runner  string
	Mirror  *git.Mirror
	Repo    forge.Repo
	Branch  string
	Builder Builder
	Gates   Gates
	TTL     time.Duration
}

// ItemFor turns an admitted ingress into a work item. The ingress link is
// what keeps "why does this change exist" answerable from the other end.
func ItemFor(ing ingress.Ingress, id, task string) queue.WorkItem {
	return queue.WorkItem{ID: id, Tenant: ing.Tenant, Box: ing.Box, Ingress: ing.ID, Task: task}
}

// Run drives one item from claim to settled state. The returned item reports
// where it ended: done, parked, waiting, or failed — an error return means
// the loop itself broke, not that the change was judged and refused.
func (l *Loop) Run(ctx context.Context, item queue.WorkItem) (queue.WorkItem, error) {
	q := &queue.Queue{Store: l.Store, Runner: l.Runner, TTL: l.TTL}
	item, claim, err := q.Claim(ctx, item)
	if err != nil {
		return item, fmt.Errorf("loop: claim: %w", err)
	}

	leaser := l.Store.Leaser(l.Runner)
	ref := "refs/heads/" + l.Branch
	repoLease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: item.Tenant, Repo: l.Repo.Name, Ref: ref, Run: item.ID, TTL: l.TTL,
	})
	if err != nil {
		// The branch is busy: not an exception, just not our turn.
		return q.Wait(ctx, item, claim, fmt.Sprintf("branch %s is leased: %v", l.Branch, err))
	}
	defer leaser.Release(ctx, repoLease)

	if err := l.Mirror.Fetch(ctx, l.Repo); err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	base, err := l.Mirror.Resolve(ctx, l.Repo, ref)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}
	ws, err := l.Mirror.Checkout(ctx, repoLease, base)
	if err != nil {
		return q.Fail(ctx, item, claim, err.Error())
	}

	var head forge.Commit
	if serr := telemetry.Stage(ctx, "build", item, func(ctx context.Context) error {
		var err error
		head, err = l.Builder.Build(ctx, ws, item.Task)
		if err == nil {
			l.record(ctx, item, "built", string(head), "")
		}
		return err
	}); serr != nil {
		return q.Park(ctx, item, claim, "build: "+serr.Error())
	}

	cs := forge.ChangeSet{
		ID: "cs-" + item.ID, Tenant: item.Tenant, Box: item.Box, Run: item.ID,
		Intent: item.Task,
		Members: []forge.Member{{
			Repo: l.Repo.Name, Branch: l.Branch, Base: base, Head: head, Lease: repoLease,
		}},
	}

	var verdicts []forge.Verdict
	var gateInfra error
	gerr := telemetry.Stage(ctx, "gate", item, func(ctx context.Context) error {
		for _, g := range l.Gates(ws) {
			v, err := g.Evaluate(ctx, cs)
			if err != nil {
				// Infrastructure trouble is never recorded as a rejected change.
				gateInfra = fmt.Errorf("gate %s could not conclude: %v", g.Name(), err)
				return gateInfra
			}
			body, _ := json.Marshal(v)
			l.record(ctx, item, "gate", string(head), string(body))
			verdicts = append(verdicts, v)
		}
		if forge.Resolve(verdicts) == forge.DecisionFail {
			return fmt.Errorf("gates failed") // marks the stage span failed; the reasons go to evidence and the park note
		}
		return nil
	})
	if gateInfra != nil {
		return q.Fail(ctx, item, claim, gateInfra.Error())
	}
	_ = gerr
	switch forge.Resolve(verdicts) {
	case forge.DecisionFail:
		return q.Park(ctx, item, claim, reasons(verdicts))
	case forge.DecisionPending:
		return q.Wait(ctx, item, claim, reasons(verdicts))
	}

	if lerr := telemetry.Stage(ctx, "land", item, func(ctx context.Context) error {
		_, err := git.NewLander(l.Mirror, l.Store.Log()).Land(ctx, cs)
		if err == nil {
			telemetry.Outcome(ctx, queue.StateDone, string(head))
		}
		return err
	}); lerr != nil {
		return q.Park(ctx, item, claim, "land: "+lerr.Error())
	}
	return q.Done(ctx, item, claim, head)
}

func (l *Loop) record(ctx context.Context, item queue.WorkItem, kind, subject, body string) {
	if body == "" {
		b, _ := json.Marshal(item)
		body = string(b)
	}
	l.Store.Log().Append(ctx, evidence.Record{
		Tenant: item.Tenant, Scope: "workitem/" + item.ID,
		Kind: kind, Subject: subject, Actor: l.Runner, Body: []byte(body),
		Trace: telemetry.TraceID(ctx),
	})
}

func reasons(vs []forge.Verdict) string {
	out := ""
	for _, v := range vs {
		if v.Decision != forge.DecisionPass {
			if out != "" {
				out += "; "
			}
			line := v.Reason
			if i := indexByte(line, '\n'); i >= 0 {
				line = line[:i]
			}
			out += fmt.Sprintf("%s: %s (%s)", v.Gate, v.Decision, line)
		}
	}
	if out == "" {
		out = "no gate examined the change"
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
