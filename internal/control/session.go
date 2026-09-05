package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// runRecord is the agent-run record as this package reads it — a subset of
// what [acp.Builder] writes, spelled here rather than imported so the room
// does not depend on the runtime that drives agents. The field names are the
// contract between them, and TestAgentRunRecordCarriesTheComposition holds
// both ends to it.
type runRecord struct {
	AgentRef string  `json:"agentref"`
	Seat     string  `json:"seat"`
	Layers   []Layer `json:"layers,omitempty"`
}

// Layer is one layer the run's agent was composed from, in application order.
// Named the way Explain names it, so a reader can check the room's account
// against the bundle rather than only against itself.
type Layer struct {
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Digest string `json:"digest"`
}

// Session is one running unit as the control room shows it: what it is, how
// far it has got, and what its agent is doing right now.
//
// Three things in one card because they are one question. A claimed item said
// `state=claimed head=` and nothing else, for the whole length of a run — the
// room knew the unit existed and the daemon knew everything else. The three
// halves come from three places and only one of them is new: identity and
// progress are folds over the item's own chain, the layers come off the
// agent-run record, and the timeline is the live bus.
type Session struct {
	QueueEntry
	// Layers is the composed agentref's stack — which layers, in which order,
	// on which seat. The materialized context set is the other half of what
	// the agent knows and is not here: it is on the agent-prompt record, whole,
	// and a card is not where a reader should meet a rendered bundle.
	Layers []Layer `json:"layers,omitempty"`
	// Timeline is what the agent has said and done so far, oldest first:
	// messages, tool calls, refusals, and the box verbs.
	//
	// Empty means one of two things and the card must not conflate them —
	// either nothing has happened yet, or this daemon never saw the run. Watch
	// tells them apart.
	Timeline []Frame `json:"timeline,omitempty"`
	// Watching reports that the live bus is fanning this unit in. False on a
	// unit claimed by another runner, or one whose run predates the daemon
	// now answering — in both cases an empty timeline is silence about the
	// run, not evidence the agent has done nothing.
	Watching bool `json:"watching"`
	// Judgement marks a row as an origination still being decomposed rather
	// than a claimed work item. queue.State has no value for "in judgement" —
	// this is not a queue item at all, it is the planner's own act — so a
	// reader must not be left to infer the distinction from an empty State.
	Judgement bool `json:"judgement,omitempty"`
}

// Sessions is the control room's per-unit live view: one entry per running
// unit, each carrying its own progress and its own communication, so several
// units in flight are read side by side rather than as one interleaved feed.
//
// Claimed and nothing else. A waiting item released its claim and is somebody
// else's decision; a handed-off item is between sessions and has no agent to
// watch; a parked one is the exception queue's. "Running" is a narrow word
// here on purpose — the room already has a view for each of the others, and a
// live view that quietly includes work nobody is doing is the stale-stage bug
// again in a bigger frame.
func (r *Room) Sessions(ctx context.Context) ([]Session, error) {
	rows, f, err := r.fold(ctx, queue.StateClaimed)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(rows))
	for _, e := range rows {
		out = append(out, Session{
			QueueEntry: e,
			Layers:     f.layers[e.ID],
			Timeline:   r.Live.Item(e.ID),
			Watching:   r.Live.Watching(e.ID),
		})
	}
	// A judgement — an origination the planner is still decomposing — has no
	// queue entry to fold; before #44 it had no surface at all, only frames
	// reaching the bus and the run log under "plan-<ingress-id>" with nothing
	// to read them back. Live.Prefixed is the only index into that: the queue
	// does not know about work that does not exist yet.
	for _, item := range r.Live.Prefixed("plan-") {
		id := strings.TrimPrefix(item, "plan-")
		s, ok, err := r.judgement(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// judgement folds one origination's chain into a session row for a judgement
// still in flight, or reports that it settled and must not appear: planned or
// failed both mean the planner is done, one way or another, and only the
// two states short of that — never judged, or sent back for another attempt
// — belong on this list.
//
// One walk of ingress/<id> rather than three. plan.Store.Standing and
// plan.Store.Origination would each open their own walk of this same chain,
// and the agent-run record — the planner's seat, agentref and layers, in the
// same shape [runRecord] already reads off a unit's chain — would be a third;
// [Room.entry] and [Room.parked] fold their chains once for exactly this
// reason, so this does too.
func (r *Room) judgement(ctx context.Context, id string) (Session, bool, error) {
	var ing ingress.Ingress
	var ran runRecord
	state := ""
	found := false
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, ingress.Scope(id), 1) {
		if err != nil {
			return Session{}, false, err
		}
		switch rec.Kind {
		case ingress.RecordOpen:
			if err := json.Unmarshal(rec.Body, &ing); err != nil {
				return Session{}, false, fmt.Errorf("control: judgement %s: %w", id, err)
			}
			if ing.Opened.IsZero() {
				ing.Opened = rec.At
			}
			found = true
		case "agent-run":
			// Best effort, same as entry: a record this reader cannot parse is
			// a thinner row, never a listing that fails.
			var got runRecord
			if json.Unmarshal(rec.Body, &got) == nil {
				ran = got
			}
		case plan.RecordKind:
			state = "planned"
		case plan.FailedKind:
			state = "failed"
		case plan.RetriedKind:
			state = "retried"
		case plan.ResolvedKind:
			state = "resolved"
		}
	}
	if !found || (state != "" && state != "retried") {
		return Session{}, false, nil
	}
	e := QueueEntry{
		ID: id, Box: ing.Box, Repo: ing.Repo, Ingress: id,
		Task: firstLine(plan.TaskOf(ing)), Seat: ran.Seat, AgentRef: ran.AgentRef,
		Opened: ing.Opened,
	}
	return Session{
		QueueEntry: e, Layers: ran.Layers, Judgement: true,
		Timeline: r.Live.Item("plan-" + id), Watching: r.Live.Watching("plan-" + id),
	}, true, nil
}

// Session is one running unit by id, for a room following a single card.
// Reported whatever its state, because an operator who followed a link wants
// to be told the run finished rather than shown nothing.
func (r *Room) Session(ctx context.Context, id string) (Session, error) {
	f := &frontier{
		r: r, now: r.now(), items: map[string]queue.WorkItem{}, errs: map[string]error{},
		q: &queue.Queue{Store: r.Store, Runner: r.Actor}, layers: map[string][]Layer{},
	}
	e, err := r.entry(ctx, id, f)
	if err != nil {
		return Session{}, err
	}
	return Session{
		QueueEntry: e, Layers: f.layers[id],
		Timeline: r.Live.Item(id), Watching: r.Live.Watching(id),
	}, nil
}
