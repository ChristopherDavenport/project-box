package agent

import (
	"fmt"
	"strings"
)

// Prior is the attempt that came before this run.
//
// Work the factory opens for itself — repair for a red landing most of all —
// enters through a route as NEW work, so the item that wakes an agent is an
// ordinary one: a task string and nothing else. Whatever the run before it
// tried, doubted, or stopped halfway through was not on that item, so the
// successor starts cold and re-derives the situation from the diff. On a red
// branch that is waste; on a parked run it is worse, because the handoff had
// already captured the answer and nothing carried it.
//
// Carried as a LAYER rather than concatenated into the task text. The merge
// semantics stay as explicit as every other layer's, and the bundle digest
// moves when the carried context does — so "this run was woken knowing X" is
// answerable from the agentref afterwards, exactly the way seat continuity
// already is.
type Prior struct {
	// Run is the work item that made the attempt. Its handoff notes are
	// looked up under it at wake time rather than copied in here, because a
	// predecessor writes its notes as it SETTLES — which on the optimistic
	// lane happens after the repair it summoned has already been opened.
	Run string `json:"run,omitempty"`
	// Repo, Branch and Sha are where the attempt got to. On the optimistic
	// lane the change LANDED and nothing rolled it back, so the branch still
	// carries it: the successor is resuming a change rather than making one.
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Sha    string `json:"sha,omitempty"`
	// Checks names what reported the trouble and Reason is what it said.
	// Which check went red is the difference between "main is broken" and a
	// place to start.
	Checks []string `json:"checks,omitempty"`
	Reason string   `json:"reason,omitempty"`
	// Notes are the predecessor's handoff, resolved at wake time from the
	// seat that wrote them and never authored here.
	Notes []string `json:"notes,omitempty"`
}

// Empty reports whether there is nothing here worth carrying. A repo and a
// branch alone say only where work happens, which the agent knows already.
func (p Prior) Empty() bool {
	return p.Run == "" && p.Sha == "" && p.Reason == "" && len(p.Checks) == 0 && len(p.Notes) == 0
}

// Layer renders the carried attempt as a resolved inline layer, reporting
// false when there is nothing to carry: an empty prior must not become a
// layer that says nothing, because a layer changes the agentref and a run
// woken knowing nothing is not a different agent.
//
// Task scope, beside the task text. What the last attempt did is about this
// one unit of work and no other, which is per-request in exactly the way
// [ScopeTask] is for — and inline for the same reason the task layer is, so
// a Box requiring governed sources of its broader scopes is not surprised
// here.
func (p Prior) Layer() (Layer, bool) {
	if p.Empty() {
		return Layer{}, false
	}
	var b strings.Builder
	b.WriteString("## The attempt before this one\n\nYou are not the first run at this work. What the last one did:\n\n")
	if p.Run != "" {
		fmt.Fprintf(&b, "- the run was %s\n", p.Run)
	}
	switch {
	case p.Sha != "" && p.Branch != "":
		fmt.Fprintf(&b, "- it landed %s on %s, and nothing rolled that back — the change is still on the branch, so carry it forward rather than make it again\n",
			p.where(), p.Branch)
	case p.Sha != "":
		fmt.Fprintf(&b, "- it produced %s\n", p.where())
	}
	if len(p.Checks) > 0 {
		fmt.Fprintf(&b, "- %s reported it red\n", strings.Join(p.Checks, ", "))
	}
	if p.Reason != "" {
		fmt.Fprintf(&b, "- what the checks said: %s\n", p.Reason)
	}
	if len(p.Notes) > 0 {
		b.WriteString("\nIts handoff, oldest first:\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	b.WriteString("\nStart from that. Re-deriving it from the diff is work this factory has already paid for once.\n")
	text := b.String()
	return Layer{
		Name:         "prior-attempt",
		Scope:        ScopeTask,
		Source:       Source{Kind: SourceInline, Digest: hashText(text)},
		Instructions: text,
	}, true
}

func (p Prior) where() string {
	if p.Repo != "" {
		return p.Repo + "@" + p.Sha
	}
	return p.Sha
}
