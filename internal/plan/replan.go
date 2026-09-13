package plan

import (
	"context"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
)

// ReplanUnit is the work item a [Replanner] is asked to revise, named the
// way an operator would reference it rather than by its plan-internal node
// id.
type ReplanUnit struct {
	ID       string
	Task     string
	Sections []string
}

// ReplanEscalation is why a unit is being replanned, carried verbatim from
// the builder's structured [loop.Escalation]. It travels as JSON on the
// item's own chain (kind "replan-requested") between the loop that raised it
// and the pump that answers it, which is why it is a plain data shape here
// rather than a reference to loop's own type — plan does not import loop.
type ReplanEscalation struct {
	Reason        string   `json:"reason"`
	NeedsSections []string `json:"needs_sections,omitempty"`
	Paths         []string `json:"paths,omitempty"`
}

// ReplanNode is a [Replanner]'s answer: the unit's revised shape. It mirrors
// only the two [Node] fields a replan may change — WHERE the unit works and
// WHAT it asks for — never the graph around it.
type ReplanNode struct {
	// Sections replaces the unit's Sections outright, even when empty: a
	// planner naming no section is deciding the unit now needs the whole
	// repository, and that is a decision to apply, not an omission to
	// preserve the old value through.
	Sections []string
	// Task replaces the unit's Task when non-empty. Empty means the
	// planner left it alone — unlike Sections, there is no shape in which an
	// empty task is itself the answer, since [Node.validate] refuses one.
	Task string
}

// Replanner asks the planner to revise a unit whose builder escalated for a
// need [loop.WidenTests] could not grant in place: production paths, or
// needs_sections stated with no paths. It is where issue #182's pump hands
// such a unit to a decomposition judgement instead of a person's exception
// queue. The pump carries this as a nil-safe field (part 2, this unit): nil
// means no planner is configured, and the unit parks exactly as it would
// have before replanning existed. internal/agent/acp implements it by
// asking a model (part 3); internal/daemon wires that implementation into
// the pump (part 4).
//
// Origination is the ingress the unit's plan traces to, p is that plan as it
// last stood (loaded by the caller via [Store.ByRef] from the item's own
// Plan ref), unit is what the item asks for now, and esc is the builder's
// own escalation. An error — including one shaped like [loop.Escalation]
// itself, the planner declining to answer — means the caller parks the unit
// as an exception; see [Pump.Replan] in the pump package for that half.
type Replanner interface {
	Replan(ctx context.Context, origination ingress.Ingress, p Plan, unit ReplanUnit, esc ReplanEscalation) (ReplanNode, error)
}
