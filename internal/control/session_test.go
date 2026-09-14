package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// ran writes the agent-run record a real builder writes, so these tests read
// the same shape the room reads in production. The field names are the
// contract between the two packages; acp's TestTheRoomReadsWhatTheBuilderWrote
// holds the other end.
func ran(t *testing.T, r *Room, id, seat, ref string, layers ...Layer) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"agentref": ref, "role": "builder", "mode": "acceptEdits",
		"seat": seat, "envelope": "(none declared)", "layers": layers,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: "workitem/" + id, Kind: "agent-run",
		Subject: ref, Actor: seat, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// agentRunOn writes an agent-run record on an arbitrary scope, with a role
// and an optional continues — the fields [ran] does not need but
// AgentSession's fold does. Used directly rather than through [ran] so
// judgement's ingress-scope chain can be built the same way a claimed
// item's workitem-scope chain is.
func agentRunOn(t *testing.T, r *Room, scope, ref, role, seat, continues string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"agentref": ref, "role": role, "mode": "acceptEdits",
		"seat": seat, "envelope": "(none declared)", "continues": continues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: "agent-run",
		Subject: ref, Actor: seat, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// agentSessionOn writes the agent-session record a real builder writes once
// its session opens: what the agent said it was, and the ACP session id a
// later attempt can continue.
func agentSessionOn(t *testing.T, r *Room, scope, ref, model, session string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"agentref": ref, "model": model, "session": session,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: "agent-session",
		Subject: ref, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// agentResultOn writes the agent-result record a real builder writes once an
// attempt settles: the subset resultBody's own fields this package reads.
func agentResultOn(t *testing.T, r *Room, scope, ref string, wallMS, tokens int64, costUSD float64) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"stop": "end_turn", "wall_ms": wallMS, "tokens": tokens, "cost_usd": costUSD,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: "agent-result",
		Subject: ref, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// builtOn writes the "built" record the loop writes right after a builder
// attempt's own agent-run/agent-session/agent-result (internal/loop/loop.go
// line 819), subject = the new head.
func builtOn(t *testing.T, r *Room, scope, sha string) {
	t.Helper()
	if _, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: "built", Subject: sha, Body: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}
}

// gateOn writes a "gate" record the way the loop's gateVerdictRecord
// marshals — forge.Verdict's own default JSON encoding, Go-capitalized field
// names (internal/loop/loop.go's gateVerdictRecord) — for a gate named
// `gate` (e.g. the repo's check label, "commit-message", or acp.ReviewerName
// "review") judging subject sha. Returns the record's chain id, the same id
// a review row's VerdictRecordID should carry.
func gateOn(t *testing.T, r *Room, scope, sha, gate, decision, reason string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"Gate": gate, "Decision": decision, "Reason": reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: scope, Kind: "gate", Subject: sha, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec.ID
}

// TestASessionSaysWhoWhereAndWhat is the issue's three bullets on one card.
//
// `state=claimed head=` was the whole answer for the length of a run — minutes
// to tens of minutes in which the daemon knew the seat, the composed layers,
// the stage, and every word the agent had said, and published none of it.
func TestASessionSaysWhoWhereAndWhat(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	r.Live = &Live{}

	w := submit(t, q, "wi-1")
	w, _, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	ran(t, r, w.ID, "mason", "sha256:deadbeef",
		Layer{Scope: "role", Name: "builder", Source: "inline", Digest: "d1"},
		Layer{Scope: "task", Name: "wake", Source: "inline", Digest: "d2"})
	r.Live.Append(w.ID, Frame{Kind: "started"})
	r.Live.Append(w.ID, Frame{Kind: "message", Text: "Reading the tree first."})
	r.Live.Append(w.ID, Frame{Kind: "tool", Tool: "Read"})
	r.Live.Append(w.ID, Frame{Kind: "denied", Tool: "DeleteProd"})

	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the one claimed unit", len(sessions))
	}
	s := sessions[0]

	// identity
	if s.ID != "wi-1" || s.Box != "shop" || s.Ingress != "ing-1" {
		t.Errorf("identity = %+v", s.QueueEntry)
	}
	if s.Seat != "mason" || s.AgentRef != "sha256:deadbeef" {
		t.Errorf("seat=%q agentref=%q — the record carried both and the row showed neither", s.Seat, s.AgentRef)
	}
	// The layers in APPLICATION ORDER: a narrower layer's contribution only
	// means something against what it came after, so the order is the fact.
	if len(s.Layers) != 2 || s.Layers[0].Scope != "role" || s.Layers[1].Scope != "task" {
		t.Errorf("layers = %+v, want role then task", s.Layers)
	}
	// progress — the agent-run record is precisely how a row learns it is
	// building, which is why the stage moves past "claimed" here.
	if s.State != queue.StateClaimed || s.Stage != "building" {
		t.Errorf("progress: state=%q stage=%q", s.State, s.Stage)
	}
	// communication
	if !s.Watching {
		t.Error("watching = false for a unit this bus is fanning in")
	}
	if len(s.Timeline) != 4 || s.Timeline[2].Tool != "Read" || s.Timeline[3].Kind != "denied" {
		t.Fatalf("timeline = %+v", s.Timeline)
	}
}

// TestSessionsFanOutRatherThanInterleave: several units in flight are several
// cards. The failure this rules out is the tempting one — a merged feed, in
// which the question "is THIS unit moving" is answered by scrolling.
func TestSessionsFanOutRatherThanInterleave(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	r.Live = &Live{}

	for _, id := range []string{"wi-a", "wi-b"} {
		w := submit(t, q, id)
		if _, _, err := q.Claim(ctx, w); err != nil {
			t.Fatal(err)
		}
		ran(t, r, id, "mason", "ref-"+id)
	}
	// Interleaved on the bus, exactly as two concurrent runs arrive.
	r.Live.Append("wi-a", Frame{Kind: "tool", Tool: "Read"})
	r.Live.Append("wi-b", Frame{Kind: "tool", Tool: "Bash"})
	r.Live.Append("wi-a", Frame{Kind: "tool", Tool: "Edit"})

	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	by := map[string]Session{}
	for _, s := range sessions {
		by[s.ID] = s
	}
	if got := by["wi-a"].Timeline; len(got) != 2 || got[0].Tool != "Read" || got[1].Tool != "Edit" {
		t.Errorf("wi-a timeline = %+v — one unit's card carries another unit's work", got)
	}
	if got := by["wi-b"].Timeline; len(got) != 1 || got[0].Tool != "Bash" {
		t.Errorf("wi-b timeline = %+v", got)
	}
	if by["wi-a"].AgentRef == by["wi-b"].AgentRef {
		t.Error("both cards report the same agentref; identity is per unit")
	}
}

// TestOnlyRunningUnitsAreSessions holds the word "running" to its meaning.
//
// The stale-stage bug in a bigger frame: a live view that quietly includes
// work nobody is doing tells an operator that work is in progress, which is
// the one reading they must never be given.
func TestOnlyRunningUnitsAreSessions(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	running := submit(t, q, "wi-running")
	if _, _, err := q.Claim(ctx, running); err != nil {
		t.Fatal(err)
	}
	submit(t, q, "wi-waiting-to-be-claimed")
	parked := submit(t, q, "wi-parked")
	pc, claim, err := q.Claim(ctx, parked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, pc, claim, queue.SeverityMedium, "needs a person"); err != nil {
		t.Fatal(err)
	}

	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "wi-running" {
		var ids []string
		for _, s := range sessions {
			ids = append(ids, s.ID)
		}
		t.Fatalf("sessions = %v, want only the claimed unit", ids)
	}
}

// TestAnEmptyTimelineIsNotAnIdleAgent: a claimed unit no bus is watching —
// another runner's work, or a window that rolled — must not read as an agent
// that has done nothing. One is worth interrupting; the other is not.
func TestAnEmptyTimelineIsNotAnIdleAgent(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	r.Live = &Live{}

	w := submit(t, q, "wi-elsewhere")
	if _, _, err := q.Claim(ctx, w); err != nil {
		t.Fatal(err)
	}
	s, err := r.Session(ctx, "wi-elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Timeline) != 0 {
		t.Fatalf("timeline = %+v", s.Timeline)
	}
	if s.Watching {
		t.Error("watching = true for a unit no bus has ever seen")
	}
}

// TestSessionsWithoutALiveBusStillCarryIdentity: the room degrades to what it
// could always say rather than to an error. A daemon with no agent builder
// reports units with no timeline, not no units.
func TestSessionsWithoutALiveBusStillCarryIdentity(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-shell")
	if _, _, err := q.Claim(ctx, w); err != nil {
		t.Fatal(err)
	}
	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "wi-shell" || sessions[0].Watching {
		t.Fatalf("sessions = %+v", sessions)
	}
}

// TestAJudgementIsASessionUntilItSettles is issue #47: a running judgement —
// the planner decomposing an origination — had frames on the live bus and the
// run log under "plan-<ingress-id>" since #44, and nowhere to see them. It
// must show up while the judgement runs and disappear the moment it settles,
// same as a claimed unit disappears once it is done.
func TestAJudgementIsASessionUntilItSettles(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	r.Live = &Live{}

	ing := ingress.Ingress{
		ID: "ing-judge", Tenant: tn, Box: "shop", Repo: "app",
		Requester: "chris", Note: "decompose this", Title: "the checkout flow is broken",
	}
	body, err := json.Marshal(ing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: ingress.Scope(ing.ID), Kind: ingress.RecordOpen,
		Subject: "delivery/" + ing.ID, Actor: ing.Requester, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	runBody, err := json.Marshal(map[string]any{
		"agentref": "sha256:cafef00d", "role": "planner", "mode": "acceptEdits",
		"seat": "planner", "envelope": "(none declared)",
		"layers": []Layer{{Scope: "role", Name: "planner", Source: "inline", Digest: "d1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: ingress.Scope(ing.ID), Kind: "agent-run",
		Subject: "sha256:cafef00d", Actor: "planner", Body: runBody,
	}); err != nil {
		t.Fatal(err)
	}
	r.Live.Append("plan-"+ing.ID, Frame{Kind: "started"})
	r.Live.Append("plan-"+ing.ID, Frame{Kind: "message", Text: "decomposing"})

	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the one judgement in flight", len(sessions))
	}
	s := sessions[0]
	if !s.Judgement {
		t.Error("judgement = false for a row that is one")
	}
	if s.ID != ing.ID || s.Ingress != ing.ID || s.Box != "shop" || s.Repo != "app" {
		t.Errorf("identity = %+v", s.QueueEntry)
	}
	if s.Task != "decompose this" {
		t.Errorf("task = %q", s.Task)
	}
	if s.Summary != ing.Title {
		t.Errorf("summary = %q, want the ingress title %q", s.Summary, ing.Title)
	}
	if s.Seat != "planner" || s.AgentRef != "sha256:cafef00d" {
		t.Errorf("seat=%q agentref=%q — the planner's own agent-run record carried both", s.Seat, s.AgentRef)
	}
	if len(s.Layers) != 1 || s.Layers[0].Scope != "role" {
		t.Errorf("layers = %+v", s.Layers)
	}
	if !s.Watching || len(s.Timeline) != 2 {
		t.Fatalf("timeline = %+v watching=%v", s.Timeline, s.Watching)
	}

	// The disappearing-once-planned case: once the planner settles, the
	// origination has a real decomposition and the judgement card must not
	// still be showing "in progress" beside it.
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: ingress.Scope(ing.ID), Kind: plan.RecordKind,
		Subject: "plan-ref", Actor: "planner", Body: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err = r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions after planning settled = %+v, want none", sessions)
	}
}

// TestSessionByIdOutlivesTheRun: an operator who followed a link to a unit
// that has since settled is told it settled. Reporting nothing would be the
// same silence the black box was.
func TestSessionByIdOutlivesTheRun(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-done")
	w, claim, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, w, claim, "abc123"); err != nil {
		t.Fatal(err)
	}
	s, err := r.Session(ctx, "wi-done")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != queue.StateDone || s.Head != "abc123" {
		t.Errorf("settled session = %+v", s.QueueEntry)
	}
}

// TestAgentSessionsFoldChainOrderRolesAttemptsAndCost is issue #107's core
// claim: the chain already recorded every attempt completely, and a listing
// over it must show that account rather than the newest agent-run alone.
//
// Three builder attempts interleaved with a reviewer session: chain order
// must survive the fold, attempt ordinals must count per role (a reviewer
// session between two builder ones must not perturb the builder count), a
// continues reference must ride through, and a row with no agent-result yet
// must read as unfinished rather than a free run.
func TestAgentSessionsFoldChainOrderRolesAttemptsAndCost(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-attempts")
	w, _, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	scope := "workitem/" + w.ID

	agentRunOn(t, r, scope, "ref-b1", "builder", "mason", "")
	agentSessionOn(t, r, scope, "ref-b1", "sonnet", "sess-b1")
	agentResultOn(t, r, scope, "ref-b1", 1000, 500, 0.5)

	agentRunOn(t, r, scope, "ref-rv1", "reviewer", "reviewer-seat", "")
	agentSessionOn(t, r, scope, "ref-rv1", "sonnet", "sess-rv1")
	agentResultOn(t, r, scope, "ref-rv1", 2000, 800, 0.25)

	// The second builder attempt continues the first's session, and has no
	// result yet: still running, or lost mid-flight.
	agentRunOn(t, r, scope, "ref-b2", "builder", "mason", "sess-b1")
	agentSessionOn(t, r, scope, "ref-b2", "sonnet", "sess-b2")

	s, err := r.Session(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.AgentSessions
	if len(rows) != 3 {
		t.Fatalf("agent sessions = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].AgentRef != "ref-b1" || rows[1].AgentRef != "ref-rv1" || rows[2].AgentRef != "ref-b2" {
		t.Fatalf("chain order = %+v, want b1, rv1, b2", rows)
	}
	if rows[0].Role != "builder" || rows[0].Attempt != 1 {
		t.Errorf("ref-b1 = role=%q attempt=%d, want builder attempt 1", rows[0].Role, rows[0].Attempt)
	}
	if rows[1].Role != "reviewer" || rows[1].Attempt != 1 {
		t.Errorf("ref-rv1 = role=%q attempt=%d, want reviewer attempt 1", rows[1].Role, rows[1].Attempt)
	}
	if rows[2].Role != "builder" || rows[2].Attempt != 2 {
		t.Errorf("ref-b2 = role=%q attempt=%d, want builder attempt 2 — a reviewer session must not perturb the builder count", rows[2].Role, rows[2].Attempt)
	}
	if rows[2].Continues != "sess-b1" {
		t.Errorf("ref-b2 continues = %q, want sess-b1", rows[2].Continues)
	}
	if !rows[0].Finished || !rows[1].Finished {
		t.Errorf("ref-b1/ref-rv1 both have results and must read finished: %+v", rows[:2])
	}
	if rows[2].Finished {
		t.Error("ref-b2 has no agent-result and must not read finished")
	}
	if rows[2].Tokens != 0 || rows[2].CostUSD != 0 || !rows[2].Ended.IsZero() {
		t.Errorf("ref-b2 = %+v, want zero tokens/cost/ended — unfinished is not a free run", rows[2])
	}
	// Totals summed from this exact slice: 500+800 tokens, 0.5+0.25 cost.
	// ref-b2 contributes nothing because it is unfinished.
	if s.AgentTokens != 1300 {
		t.Errorf("agent tokens = %d, want 1300 (sum of the listed rows)", s.AgentTokens)
	}
	if s.AgentCostUSD != 0.75 {
		t.Errorf("agent cost = %v, want 0.75 (sum of the listed rows)", s.AgentCostUSD)
	}
}

// TestAgentSessionsFoldHeadsRowsByRoleWithVerdicts is issue #189's chain: two
// gated attempts, each builder/built/check-gate/reviewer/review-gate in
// order, the first review passing and the second failing. It asserts every
// bullet the issue named at once: builder rows carry the sha their attempt
// produced, review rows carry the decision, reason and verdict record id
// once one lands, attempts are numbered independently per role, and the
// check gate's own verdict — same kind, same subject, different Gate name —
// attaches to no row at all.
func TestAgentSessionsFoldHeadsRowsByRoleWithVerdicts(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-verdicts")
	w, _, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	scope := "workitem/" + w.ID

	// Attempt 1: builds sha1, the check gate passes it, the reviewer passes
	// it too.
	agentRunOn(t, r, scope, "ref-b1", "builder", "mason", "")
	agentSessionOn(t, r, scope, "ref-b1", "sonnet", "sess-b1")
	agentResultOn(t, r, scope, "ref-b1", 1000, 500, 0.5)
	builtOn(t, r, scope, "sha1")
	gateOn(t, r, scope, "sha1", "check", "pass", "go test ./... clean")
	agentRunOn(t, r, scope, "ref-rv1", "gate", "reviewer-seat", "")
	agentSessionOn(t, r, scope, "ref-rv1", "sonnet", "sess-rv1")
	agentResultOn(t, r, scope, "ref-rv1", 2000, 800, 0.25)
	rv1ID := gateOn(t, r, scope, "sha1", "review", "pass", "honours the origination")

	// Attempt 2: builds sha2, the check gate passes it again, the reviewer
	// fails it this time.
	agentRunOn(t, r, scope, "ref-b2", "builder", "mason", "sess-b1")
	agentSessionOn(t, r, scope, "ref-b2", "sonnet", "sess-b2")
	agentResultOn(t, r, scope, "ref-b2", 1500, 600, 0.6)
	builtOn(t, r, scope, "sha2")
	gateOn(t, r, scope, "sha2", "check", "pass", "go test ./... clean")
	agentRunOn(t, r, scope, "ref-rv2", "gate", "reviewer-seat", "")
	agentSessionOn(t, r, scope, "ref-rv2", "sonnet", "sess-rv2")
	agentResultOn(t, r, scope, "ref-rv2", 2200, 900, 0.3)
	rv2ID := gateOn(t, r, scope, "sha2", "review", "fail", "drops the width column")

	s, err := r.Session(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.AgentSessions
	if len(rows) != 4 {
		t.Fatalf("agent sessions = %d, want 4: %+v", len(rows), rows)
	}
	b1, rv1, b2, rv2 := rows[0], rows[1], rows[2], rows[3]

	if b1.Role != "builder" || b1.Attempt != 1 || b1.Sha != "sha1" {
		t.Errorf("ref-b1 = role=%q attempt=%d sha=%q, want builder attempt 1 sha1", b1.Role, b1.Attempt, b1.Sha)
	}
	if b2.Role != "builder" || b2.Attempt != 2 || b2.Sha != "sha2" {
		t.Errorf("ref-b2 = role=%q attempt=%d sha=%q, want builder attempt 2 sha2", b2.Role, b2.Attempt, b2.Sha)
	}
	if b1.Decision != "" || b2.Decision != "" {
		t.Errorf("builder rows must never carry a decision: b1=%q b2=%q", b1.Decision, b2.Decision)
	}

	if rv1.Role != "gate" || rv1.Sha != "sha1" {
		t.Errorf("ref-rv1 = role=%q sha=%q, want gate sha1", rv1.Role, rv1.Sha)
	}
	if rv1.Decision != "pass" || rv1.Reason != "honours the origination" || rv1.VerdictRecordID != rv1ID {
		t.Errorf("ref-rv1 verdict = decision=%q reason=%q id=%q, want pass/honours the origination/%s",
			rv1.Decision, rv1.Reason, rv1.VerdictRecordID, rv1ID)
	}
	if rv2.Role != "gate" || rv2.Sha != "sha2" {
		t.Errorf("ref-rv2 = role=%q sha=%q, want gate sha2", rv2.Role, rv2.Sha)
	}
	if rv2.Decision != "fail" || rv2.Reason != "drops the width column" || rv2.VerdictRecordID != rv2ID {
		t.Errorf("ref-rv2 verdict = decision=%q reason=%q id=%q, want fail/drops the width column/%s",
			rv2.Decision, rv2.Reason, rv2.VerdictRecordID, rv2ID)
	}
}

// TestAgentSessionsFoldGateRowWithNoVerdictKeepsOnlyItsSha covers the gate
// infra error case the issue named: a reviewer's agent-run lands but no
// "review" gate record ever follows it (the gate errored rather than
// answering). The row must still say which sha it was reviewing and must
// carry no decision — unknown never reads as zero, so this must never
// default to a pass or a fail.
func TestAgentSessionsFoldGateRowWithNoVerdictKeepsOnlyItsSha(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-no-verdict")
	w, _, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	scope := "workitem/" + w.ID

	agentRunOn(t, r, scope, "ref-b1", "builder", "mason", "")
	agentResultOn(t, r, scope, "ref-b1", 1000, 500, 0.5)
	builtOn(t, r, scope, "sha1")
	gateOn(t, r, scope, "sha1", "check", "pass", "clean")
	agentRunOn(t, r, scope, "ref-rv1", "gate", "reviewer-seat", "")
	// The loop's synthetic "commit-message" refusal also lands as a
	// kind-"gate" record on this same subject, with no agent session behind
	// it — same shape as a real review verdict, different Gate name. This is
	// the case the name filter (body.Gate != reviewGateName), not just the
	// subject-keyed correlation, exists to keep off ref-rv1's row: at this
	// point ref-rv1 is already pendingReview["sha1"], so a filter keyed on
	// subject alone would wrongly attach this refusal to it.
	gateOn(t, r, scope, "sha1", "commit-message", "fail", "the adapter's placeholder")
	agentResultOn(t, r, scope, "ref-rv1", 2000, 800, 0.25)
	// No "review" gate record: the gate errored and recorded nothing.

	s, err := r.Session(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.AgentSessions
	if len(rows) != 2 {
		t.Fatalf("agent sessions = %d, want 2: %+v", len(rows), rows)
	}
	rv1 := rows[1]
	if rv1.Sha != "sha1" {
		t.Errorf("ref-rv1 sha = %q, want sha1 even with no verdict", rv1.Sha)
	}
	if rv1.Decision != "" || rv1.Reason != "" || rv1.VerdictRecordID != "" {
		t.Errorf("ref-rv1 = %+v, want no decision/reason/verdict id — its review never concluded", rv1)
	}

	// reviewGateName is data, not a shared symbol — acp.ReviewerName is the
	// only thing that keeps it truthful. escalate_kind_test.go already
	// imports internal/agent for the same contract-by-spelling reason; acp
	// does not import control, so this stays cycle-free.
	if reviewGateName != acp.ReviewerName {
		t.Fatalf("reviewGateName = %q, want acp.ReviewerName %q", reviewGateName, acp.ReviewerName)
	}
}

// TestAgentSessionOfFollowsTheItemsStatedBudgetOrOmits: the budget a row
// carries comes from the queue item's own Attempts field when it states one,
// and is omitted — never defaulted to the loop's own default — when the item
// names none, because control cannot see that default and must not guess it.
func TestAgentSessionOfFollowsTheItemsStatedBudgetOrOmits(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()

	budgeted, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-budgeted", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t", Attempts: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	budgeted, _, err = q.Claim(ctx, budgeted)
	if err != nil {
		t.Fatal(err)
	}
	agentRunOn(t, r, "workitem/"+budgeted.ID, "ref-budgeted", "builder", "mason", "")

	plain := submit(t, q, "wi-plain")
	plain, _, err = q.Claim(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	agentRunOn(t, r, "workitem/"+plain.ID, "ref-plain", "builder", "mason", "")

	bs, err := r.Session(ctx, budgeted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs.AgentSessions) != 1 || bs.AgentSessions[0].Of != 5 {
		t.Fatalf("budgeted row = %+v, want Of=5", bs.AgentSessions)
	}

	ps, err := r.Session(ctx, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.AgentSessions) != 1 || ps.AgentSessions[0].Of != 0 {
		t.Fatalf("plain row = %+v, want Of omitted (0) — the item named no budget", ps.AgentSessions)
	}
}

// TestAgentResultRecordCarriesTheSubsetControlReads pins the agent-result
// keys this package parses to acp's resultBody (builder.go) shape: wall_ms,
// tokens, cost_usd. A key renamed on either end must fail this test rather
// than surface, quietly, as a finished row that stopped carrying its cost.
func TestAgentResultRecordCarriesTheSubsetControlReads(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-result-shape")
	w, _, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	scope := "workitem/" + w.ID
	agentRunOn(t, r, scope, "ref-1", "builder", "mason", "")
	// The real writer's shape (acp's resultBody), including fields this
	// package does not read — proving those are ignored rather than
	// required.
	body := []byte(`{"stop":"end_turn","tool_calls":3,"wall_ms":4200,"tokens":900,"cost_usd":1.25}`)
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: scope, Kind: "agent-result", Subject: "ref-1", Body: body,
	}); err != nil {
		t.Fatal(err)
	}

	s, err := r.Session(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.AgentSessions) != 1 {
		t.Fatalf("agent sessions = %+v", s.AgentSessions)
	}
	got := s.AgentSessions[0]
	if !got.Finished || got.WallMS != 4200 || got.Tokens != 900 || got.CostUSD != 1.25 {
		t.Fatalf("result row = %+v, want finished wall_ms=4200 tokens=900 cost_usd=1.25", got)
	}
}

// TestAJudgementsPlannerSessionAppearsOnItsRow: the planner's own agent-run
// record lives on the ingress scope, not a workitem scope, and a judgement
// has no queue item to state an attempt budget — its rows must never guess
// one.
func TestAJudgementsPlannerSessionAppearsOnItsRow(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	r.Live = &Live{}

	ing := ingress.Ingress{
		ID: "ing-judge-sessions", Tenant: tn, Box: "shop", Repo: "app",
		Requester: "chris", Note: "decompose this", Title: "the checkout flow is broken",
	}
	body, err := json.Marshal(ing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: ingress.Scope(ing.ID), Kind: ingress.RecordOpen,
		Subject: "delivery/" + ing.ID, Actor: ing.Requester, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	agentRunOn(t, r, ingress.Scope(ing.ID), "ref-plan", "planner", "planner", "")
	agentSessionOn(t, r, ingress.Scope(ing.ID), "ref-plan", "sonnet", "sess-plan")
	r.Live.Append("plan-"+ing.ID, Frame{Kind: "started"})

	sessions, err := r.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the one judgement in flight", len(sessions))
	}
	s := sessions[0]
	rows := s.AgentSessions
	if len(rows) != 1 || rows[0].AgentRef != "ref-plan" || rows[0].Role != "planner" {
		t.Fatalf("judgement agent sessions = %+v, want the planner's own row", rows)
	}
	if rows[0].Model != "sonnet" || rows[0].Session != "sess-plan" {
		t.Errorf("planner row = %+v, want model=sonnet session=sess-plan", rows[0])
	}
	if rows[0].Of != 0 {
		t.Errorf("planner row Of = %d, want 0 — a judgement has no queue item to state a budget", rows[0].Of)
	}
}
