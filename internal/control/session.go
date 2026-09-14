package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
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
	Role     string  `json:"role,omitempty"`
	Seat     string  `json:"seat"`
	Layers   []Layer `json:"layers,omitempty"`
	// Continues is the prior run's ACP session id when this run continued
	// it, empty for a cold run — see acp's own runRecord.Continues.
	Continues string `json:"continues,omitempty"`
}

// sessionRecord is the agent-session record as this package reads it — a
// subset of what [acp.Builder] writes, spelled locally for the same reason
// runRecord is: what the agent said it was once its session opened.
type sessionRecord struct {
	Model   string `json:"model,omitempty"`
	Session string `json:"session,omitempty"`
}

// resultRecord is the agent-result record as this package reads it — a
// subset of resultBody (acp/builder.go): what one attempt cost and how long
// it ran. Hand-formatted JSON on the writer's side, so the key names are the
// contract; TestAgentResultRecordCarriesTheSubsetControlReads holds both
// ends to it.
type resultRecord struct {
	WallMS  int64   `json:"wall_ms"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// gateVerdictBody is declared in origins.go — this package's one read of a
// "gate" record's body, shared rather than redeclared here. The loop
// marshals forge.Verdict's own default JSON encoding (Go-capitalized field
// names) inside its gateVerdictRecord envelope (internal/loop/loop.go); this
// mirrors the same read queue.gateVerdict and loop.findGateVerdict's local
// verdictBody already do, rather than importing internal/forge for a shape
// that is already the record's own encoding. json.Unmarshal matches field
// names case-insensitively, so the capitalized wire keys land here without a
// tag.

const (
	// agentRoleBuilder and agentRoleGate mirror agent.RoleBuilder and
	// agent.RoleGate (internal/agent/agent.go), spelled locally rather than
	// imported: this package reads a record's "role" as data the way
	// runRecord's other fields are read, not as the runtime type that wrote
	// it.
	agentRoleBuilder = "builder"
	agentRoleGate    = "gate"
	// reviewGateName mirrors acp.ReviewerName (internal/agent/acp/reviewer.go)
	// — the "gate" record this fold must correlate to a gate-role row is the
	// one whose body names this gate, and only that one. A command gate (the
	// repo's own check, run before the reviewer on the same head) and the
	// loop's synthetic "commit-message" refusal are also kind-"gate" records
	// with no agent session behind them; matching on the name, not the
	// subject alone, is what keeps both off of every session row.
	reviewGateName = "review"
)

// AgentSession is one attempt's own account, folded from the agent-run,
// agent-session and agent-result records its agentref wrote on a unit's
// chain — see [Room.agentSessions]. The chain already recorded every attempt
// completely; this is the listing that had no surface until issue #107.
type AgentSession struct {
	// AgentRef is the composed agentref this attempt ran — the agent-run
	// record's Subject, and the key every other agent-* record on this
	// attempt shares.
	AgentRef string `json:"agentref"`
	// Role, Seat and Continues come off the agent-run record: which role
	// wrote this attempt, which seat drove it, and — when set — the ACP
	// session id this attempt continued rather than opened cold.
	Role      string `json:"role,omitempty"`
	Seat      string `json:"seat,omitempty"`
	Continues string `json:"continues,omitempty"`
	// Attempt is this row's ordinal among same-role agent-run records on the
	// chain, oldest first: the first builder attempt is 1, the second is 2,
	// independent of any reviewer or planner attempts interleaved with them.
	Attempt int `json:"attempt"`
	// Of is the attempt budget the queue item stated when it stated one —
	// see [queue.WorkItem.Attempts]. Omitted, never defaulted, when the item
	// named none: control cannot see the loop's own default and must not
	// guess it, and a judgement row — no queue item yet — never has one.
	Of int `json:"of,omitempty"`
	// Model and Session come off the agent-session record: what the agent
	// said it was once its session opened, and the ACP session id a later
	// attempt can continue. Empty when this agentref never opened a
	// session — a run that refused or failed before one did.
	Model   string `json:"model,omitempty"`
	Session string `json:"session,omitempty"`
	// Started is the agent-run record's own timestamp: when this attempt
	// began, whether or not it ever finished.
	Started time.Time `json:"started"`
	// Finished reports whether this agentref has an agent-result record yet.
	// False means still running, or lost mid-flight — never zero cost: a row
	// without a result carries no Ended, WallMS, Tokens or CostUSD, because
	// unknown must never read as zero.
	Finished bool      `json:"finished"`
	Ended    time.Time `json:"ended,omitempty"`
	WallMS   int64     `json:"wall_ms,omitempty"`
	Tokens   int64     `json:"tokens,omitempty"`
	CostUSD  float64   `json:"cost_usd,omitempty"`
	// Sha is the head this row concerns, and means a different thing by
	// role. On a builder row it is the commit that attempt produced —
	// folded from the "built" record the loop writes right after this same
	// row's own agent-run/agent-session/agent-result (internal/loop/loop.go).
	// On a gate-role row it is the commit under judgement: a reviewer's own
	// agent-run record carries no sha (internal/agent/acp/reviewer.go), so
	// this is the latest built sha the chain had recorded when that row's
	// agent-run appeared. Empty on every other role, and on a builder or
	// gate row whose correlated record this fold never saw.
	Sha string `json:"sha,omitempty"`
	// Decision, Reason and VerdictRecordID come off the "review" gate's own
	// verdict record, correlated to a gate-role row by Sha — set only once
	// that record lands on the chain. A gate-role row whose review never
	// concluded (a gate infra error records nothing) keeps Sha and nothing
	// else: unknown never reads as zero, and defaulting a decision here
	// would be inventing a judgement nobody made. Never set on a builder
	// row, and never set from the check gate's own verdict or the loop's
	// synthetic "commit-message" refusal — both are kind-"gate" records too,
	// on the same subject, but neither names this gate and neither belongs
	// to a session row.
	Decision        string `json:"decision,omitempty"`
	Reason          string `json:"reason,omitempty"`
	VerdictRecordID string `json:"verdict_record_id,omitempty"`
}

// agentSessionFold accumulates agent-run, agent-session and agent-result
// records into rows grouped by Subject (the agentref), in chain order. Its
// own type so [Room.judgement] — already mid-walk of a scope for its own
// reasons — can fold the same records into it in that one pass, rather than
// [Room.agentSessions] opening a second walk of a chain judgement already
// has open.
type agentSessionFold struct {
	order    []string
	byRef    map[string]*AgentSession
	attempts map[string]int
	// lastBuilder is the most recently observed builder-role row. A "built"
	// record, written right after a builder attempt's own agent-run/session/
	// result, stamps the sha it produced onto this row directly — the row
	// that produced it — rather than needing a second index keyed by sha.
	lastBuilder *AgentSession
	// lastBuiltSha is the most recent "built" record's subject: the head a
	// gate-role row appearing after it is judging. Carried across attempts
	// because a gate role's own agent-run record names no sha (see
	// AgentSession.Sha) — this is the only place on the chain that says
	// which commit it was reviewing.
	lastBuiltSha string
	// pendingReview holds gate-role rows still waiting on a "review" gate's
	// verdict, keyed by the sha they are judging. A row leaves this map the
	// moment its verdict lands; a row whose verdict never arrives (a gate
	// infra error records nothing) simply stays out of it forever, keeping
	// only the Sha it was stamped with.
	pendingReview map[string]*AgentSession
}

func newAgentSessionFold() *agentSessionFold {
	return &agentSessionFold{
		byRef: map[string]*AgentSession{}, attempts: map[string]int{},
		pendingReview: map[string]*AgentSession{},
	}
}

// observe folds one record in. Best effort, the same rule [Room.judgement]
// already holds itself to: a record this reader cannot parse is a thinner
// row, never a failed listing.
func (f *agentSessionFold) observe(rec evidence.Record) {
	switch rec.Kind {
	case "agent-run":
		var got runRecord
		if json.Unmarshal(rec.Body, &got) != nil {
			return
		}
		f.attempts[got.Role]++
		as := &AgentSession{
			AgentRef: rec.Subject, Role: got.Role, Seat: got.Seat,
			Continues: got.Continues, Attempt: f.attempts[got.Role], Started: rec.At,
		}
		switch got.Role {
		case agentRoleBuilder:
			f.lastBuilder = as
		case agentRoleGate:
			// The reviewer's own agent-run record carries no sha (verified
			// in internal/agent/acp/reviewer.go): the latest built sha the
			// chain has seen is the only way to say what this row reviews.
			// A chain with no built sha yet leaves this row uncorrelated —
			// nothing to attach a verdict to later, rather than a guess.
			if f.lastBuiltSha != "" {
				as.Sha = f.lastBuiltSha
				f.pendingReview[f.lastBuiltSha] = as
			}
		}
		f.byRef[rec.Subject] = as
		f.order = append(f.order, rec.Subject)
	case "built":
		// Written by the loop right after the builder's own agent-run/
		// agent-session/agent-result (internal/loop/loop.go), so the most
		// recently observed builder row is the attempt that produced it.
		f.lastBuiltSha = rec.Subject
		if f.lastBuilder != nil {
			f.lastBuilder.Sha = rec.Subject
		}
	case "gate":
		// Best effort, same rule as every other case: a body this reader
		// cannot parse correlates to nothing rather than failing the fold.
		var body gateVerdictBody
		if json.Unmarshal(rec.Body, &body) != nil || body.Gate != reviewGateName {
			// Not a review verdict — the check gate's own pass/fail on this
			// same subject, the loop's synthetic "commit-message" refusal,
			// or a body this reader could not parse. None of these belong
			// to a session row.
			return
		}
		if row, ok := f.pendingReview[rec.Subject]; ok {
			row.Decision, row.Reason, row.VerdictRecordID = body.Decision, body.Reason, rec.ID
			delete(f.pendingReview, rec.Subject)
		}
	case "agent-session":
		var got sessionRecord
		if json.Unmarshal(rec.Body, &got) != nil {
			return
		}
		if as, ok := f.byRef[rec.Subject]; ok {
			as.Model, as.Session = got.Model, got.Session
		}
	case "agent-result":
		var got resultRecord
		if json.Unmarshal(rec.Body, &got) != nil {
			return
		}
		if as, ok := f.byRef[rec.Subject]; ok {
			as.Finished, as.Ended = true, rec.At
			as.WallMS, as.Tokens, as.CostUSD = got.WallMS, got.Tokens, got.CostUSD
		}
	}
}

// rows returns the folded sessions in chain order. of, when positive, is
// stamped onto every row as the attempt budget the scope's own queue item
// stated — see AgentSession.Of; a judgement has no queue item and always
// passes zero, which leaves Of omitted exactly as an item that named none
// does.
func (f *agentSessionFold) rows(of int) []AgentSession {
	out := make([]AgentSession, 0, len(f.order))
	for _, ref := range f.order {
		as := *f.byRef[ref]
		if of > 0 {
			as.Of = of
		}
		out = append(out, as)
	}
	return out
}

// agentTotals sums AgentSessions' tokens and cost — the same slice a
// response already carries, never a second chain read, per PLAN.md §6 "A
// count is a projection of the listing, never a second reading of it".
// Rows without an agent-result yet contribute nothing, which is correct: an
// unfinished attempt's cost is unknown, not zero, and adding zero for it
// gives the identical sum a caller would get by skipping it.
func agentTotals(sessions []AgentSession) (tokens int64, costUSD float64) {
	for _, s := range sessions {
		tokens += s.Tokens
		costUSD += s.CostUSD
	}
	return tokens, costUSD
}

// agentSessions folds one scope's chain into its agent sessions — one row
// per attempt, oldest first. A dedicated walk of scope, for a caller ([Room.
// Session], [Room.Sessions]) that is not already mid-walk of it; see
// agentSessionFold for the one already mid-walk.
func (r *Room) agentSessions(ctx context.Context, scope string) ([]AgentSession, error) {
	fold := newAgentSessionFold()
	var of int
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, scope, 1) {
		if err != nil {
			return nil, err
		}
		fold.observe(rec)
		if queue.StateRecord(rec.Kind) {
			var w queue.WorkItem
			if json.Unmarshal(rec.Body, &w) == nil {
				of = w.Attempts
			}
		}
	}
	return fold.rows(of), nil
}

// AgentSessions lists one unit's agent sessions from its chain. The chain
// outlives the claim, so a settled item still answers with its history; an
// id with no agent-run records answers an empty list, matching runFrames'
// doctrine (internal/api/runs.go) rather than an error.
func (r *Room) AgentSessions(ctx context.Context, id string) ([]AgentSession, error) {
	return r.agentSessions(ctx, "workitem/"+id)
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
	// AgentSessions is every attempt this unit's chain has recorded, oldest
	// first — see AgentSession. Additive: issue #107 gave the room this
	// account without touching any field above it.
	AgentSessions []AgentSession `json:"agent_sessions,omitempty"`
	// AgentTokens and AgentCostUSD are this unit's total across every row in
	// AgentSessions — see agentTotals for why they are summed from that same
	// slice rather than read a second time.
	AgentTokens  int64   `json:"agent_tokens,omitempty"`
	AgentCostUSD float64 `json:"agent_cost_usd,omitempty"`
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
		as, err := r.agentSessions(ctx, "workitem/"+e.ID)
		if err != nil {
			return nil, err
		}
		tokens, cost := agentTotals(as)
		out = append(out, Session{
			QueueEntry:    e,
			Layers:        f.layers[e.ID],
			Timeline:      r.Live.Item(e.ID),
			Watching:      r.Live.Watching(e.ID),
			AgentSessions: as, AgentTokens: tokens, AgentCostUSD: cost,
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
	// Folded in the same pass rather than a second walk of ingress/<id>: see
	// agentSessionFold. A judgement has no queue item, so its rows never
	// carry an attempt budget — fold.rows(0) leaves Of omitted on every one.
	fold := newAgentSessionFold()
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, ingress.Scope(id), 1) {
		if err != nil {
			return Session{}, false, err
		}
		fold.observe(rec)
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
		Task: clip(firstLine(plan.TaskOf(ing)), 200), Summary: ing.Title,
		Seat: ran.Seat, AgentRef: ran.AgentRef,
		Opened: ing.Opened,
	}
	as := fold.rows(0)
	tokens, cost := agentTotals(as)
	return Session{
		QueueEntry: e, Layers: ran.Layers, Judgement: true,
		Timeline: r.Live.Item("plan-" + id), Watching: r.Live.Watching("plan-" + id),
		AgentSessions: as, AgentTokens: tokens, AgentCostUSD: cost,
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
	as, err := r.agentSessions(ctx, "workitem/"+id)
	if err != nil {
		return Session{}, err
	}
	tokens, cost := agentTotals(as)
	return Session{
		QueueEntry: e, Layers: f.layers[id],
		Timeline: r.Live.Item(id), Watching: r.Live.Watching(id),
		AgentSessions: as, AgentTokens: tokens, AgentCostUSD: cost,
	}, nil
}
