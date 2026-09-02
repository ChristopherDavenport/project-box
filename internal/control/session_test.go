package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
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
