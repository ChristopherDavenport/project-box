package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// prompts returns every prompt one item's sessions were sent, in order, read
// off the item's own chain rather than off this test's idea of them.
func prompts(t *testing.T, s *sqlite.Store, id string) []promptRecord {
	t.Helper()
	var out []promptRecord
	for r, err := range s.Log().Since(context.Background(), btn, "workitem/"+id, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != "agent-prompt" {
			continue
		}
		var p promptRecord
		if err := json.Unmarshal(r.Body, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestHandoffResumesInsteadOfRestarting is issue #12's property, both halves.
//
// A session 80% through a change and out of room had two moves and both were
// bad: escalate, which parks work needing no person and throws away what was
// built, or push on, which produces the degraded commit the working contract
// exists to prevent. The third answer ends the SESSION without settling the
// ITEM — so what must hold is that the successor picks up where the
// predecessor stopped, in the tree it stopped in, knowing what it knew.
//
// The checkpoint surviving inside the one commit that lands is the strongest
// form of that: had the successor started from base, part.txt — "half the
// widget" — would not be in the tree of what landed at all. It does not land
// as a commit of its own, though (issue #155): a unit lands as one commit,
// so the tree the finishing session left is squashed onto base before the
// gates run, and what reaches the branch is the finishing session's single
// commit, carrying the checkpoint's content in its tree.
func TestHandoffResumesInsteadOfRestarting(t *testing.T) {
	s, lp, router, st, remote := rig(t, "halfway")
	ctx := context.Background()
	b := lp.Builder.(*Builder)
	base := strings.TrimSpace(mustSh(t, remote, "git", "rev-parse", "refs/heads/main"))

	item := runItem(t, s, lp, router, "issue-hop", "write the widget")
	if item.State != queue.StateHandedOff {
		t.Fatalf("a mid-run handoff settled as %s instead of handing off: %+v", item.State, item)
	}
	if item.Note != "out of room with the widget half written" {
		t.Fatalf("the agent's reason did not reach the item: %q", item.Note)
	}
	if item.Hops != 1 {
		t.Fatalf("hops = %d, want 1 — the count is what bounds the spend", item.Hops)
	}
	if item.Built == "" || !item.Unfinished {
		t.Fatalf("the session's work was not kept as unfinished: %+v", item)
	}
	// Not parked, and therefore not in the queue of things needing a person.
	// Conflating the two is what buries real exceptions, and a handed-off item
	// needs nobody at all.
	room := &control.Room{Store: s, Tenant: btn, Actor: "operator"}
	exceptions, err := room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exceptions) != 0 {
		t.Fatalf("a handed-off item reached the exception queue: %+v", exceptions)
	}
	// The hop is on the chain, attributed to the seat rather than the runner
	// that hosted it: "how many sessions did this change take" is answered
	// from here.
	hops := 0
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-hop", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "handoff-requested" {
			if r.Actor != "mason" {
				t.Fatalf("the hop is attributed to %q, not the seat", r.Actor)
			}
			hops++
		}
	}
	if hops != 1 {
		t.Fatalf("%d handoff-requested records, want 1", hops)
	}
	// Nothing landed: the change is half made, and half a change must not
	// reach the branch on the strength of the session ending.
	if out := mustSh(t, remote, "git", "rev-list", "--count", "refs/heads/main"); strings.TrimSpace(out) != "1" {
		t.Fatalf("a handed-off session moved the remote:\n%s", out)
	}

	// The fresh session, on the same item and the same seat.
	b.Env = []string{"ACP_FAKE=finisher"}
	item, err = lp.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("the successor did not land: %+v", item)
	}
	if item.Unfinished {
		t.Fatal("a landed item still reads as unfinished work")
	}

	// It did not redo the work — but it also did not land as two commits
	// stacked on top of each other (issue #155). One commit reaches the
	// branch, parented directly on base, carrying the finishing session's
	// own message; the checkpoint's content survives inside its tree rather
	// than as a commit of its own.
	if got := strings.TrimSpace(mustSh(t, remote, "git", "rev-list", "--count", base+"..refs/heads/main")); got != "1" {
		t.Fatalf("expected exactly one landed commit, got count=%s", got)
	}
	landed := strings.TrimSpace(mustSh(t, remote, "git", "rev-parse", "refs/heads/main"))
	if msg := strings.TrimSpace(mustSh(t, remote, "git", "log", "-1", "--format=%s", landed)); msg != "finish the widget" {
		t.Fatalf("landed message = %q, want the finishing session's own message %q", msg, "finish the widget")
	}
	log := mustSh(t, remote, "git", "log", "--format=%s", "refs/heads/main")
	if strings.Contains(log, "half the widget") {
		t.Fatalf("the checkpoint must not land as its own commit — the successor started over, or the rebuild is no longer a no-op:\n%s", log)
	}
	mustSh(t, remote, "git", "cat-file", "-e", landed+":part.txt")

	// And it woke knowing what its predecessor knew. The notes were written at
	// the hop rather than at settle, because this item never settled between
	// the two sessions.
	sent := prompts(t, s, "wi-issue-hop")
	if len(sent) != 2 {
		t.Fatalf("%d prompts on the chain, want one per session", len(sent))
	}
	if !strings.Contains(sent[1].Prompt, "widget.go is what remains") {
		t.Fatalf("the successor woke without its predecessor's notes:\n%s", sent[1].Prompt)
	}
	if strings.Contains(sent[0].Prompt, "widget.go is what remains") {
		t.Fatal("the first session was somehow woken with notes it had not written yet")
	}
	// A run woken with a different memory is a different agent, and the
	// agentref is where that difference has to be legible — the same rule seat
	// continuity and carried repair context are already held to.
	if sent[0].AgentRef == sent[1].AgentRef {
		t.Fatal("the two sessions share an agentref: the carried notes left no trace on the record")
	}

	// The successor's own parting note reached the seat by the ordinary path,
	// so a hop does not cost the seat its closure.
	wake, ok, err := st.WakeLayer(ctx, 8)
	if err != nil || !ok {
		t.Fatalf("wake: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(wake.Instructions, "the widget is whole now") {
		t.Fatalf("the successor's handoff was lost:\n%s", wake.Instructions)
	}
}

// TestHandoffThroughTheTextChannel: the fallback carries the same act. The
// agent this factory actually runs cannot call an unknown method, so a verb
// with no text channel is a verb nothing can reach — the same reason
// escalation has read both channels since it shipped.
func TestHandoffThroughTheTextChannel(t *testing.T) {
	s, lp, router, st, _ := rig(t, "halfwaytext")
	item := runItem(t, s, lp, router, "issue-hop-text", "write the widget")
	if item.State != queue.StateHandedOff {
		t.Fatalf("the text channel did not hand off: %+v", item)
	}
	if item.Note != "out of room with the widget half written" {
		t.Fatalf("reason = %q", item.Note)
	}
	if item.Built == "" || !item.Unfinished {
		t.Fatalf("the session's work was not kept: %+v", item)
	}
	// HANDOFF: lines are the successor's notes and reached the seat mid-run.
	// The HANDOFF-NOW: line that asked for the session is the request, not a
	// note: two spellings one word apart, and the difference between them is
	// whether a fresh session opens at all.
	wake, ok, err := st.WakeLayer(context.Background(), 8)
	if err != nil || !ok {
		t.Fatalf("wake: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(wake.Instructions, "widget.go is what remains") {
		t.Fatalf("the note did not reach the seat:\n%s", wake.Instructions)
	}
	if strings.Contains(wake.Instructions, "out of room") {
		t.Fatalf("the handoff REQUEST was filed as a note:\n%s", wake.Instructions)
	}
}

// TestHandoffHopsAreBounded: a session that can summon its own successor
// forever is an unbounded spend with no landing. At the limit the item parks
// — with the reason named, because "handed off too often" tells nobody what
// the sessions were trying to do — and only then does it want a person.
func TestHandoffHopsAreBounded(t *testing.T) {
	s, lp, router, _, _ := rig(t, "halfway")
	ctx := context.Background()
	lp.MaxHandoffs = 1

	item := runItem(t, s, lp, router, "issue-hops", "write the widget")
	if item.State != queue.StateHandedOff || item.Hops != 1 {
		t.Fatalf("first hop: %+v", item)
	}
	item, err := lp.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("the limit did not park: %+v", item)
	}
	for _, want := range []string{"handed off 1 times", "the limit is 1", "out of room with the widget half written"} {
		if !strings.Contains(item.Note, want) {
			t.Fatalf("the park note lost %q: %q", want, item.Note)
		}
	}
	// NOW it wants a person, and the exception queue is where it says so.
	exceptions, err := (&control.Room{Store: s, Tenant: btn, Actor: "operator"}).Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exceptions) != 1 || exceptions[0].ID != "wi-issue-hops" {
		t.Fatalf("the exhausted item is not in the exception queue: %+v", exceptions)
	}
}

// TestUnboundedHandoffsAreAChoice: negative means no bound, which an operator
// may decide and the factory will not decide for them — the same posture the
// build timeout and the repair depth take. The default would have parked this
// item well before the last hop.
func TestUnboundedHandoffsAreAChoice(t *testing.T) {
	s, lp, router, _, _ := rig(t, "halfway")
	lp.MaxHandoffs = -1
	item := runItem(t, s, lp, router, "issue-unbounded", "write the widget")
	for range loop.DefaultMaxHandoffs + 1 {
		if item.State != queue.StateHandedOff {
			t.Fatalf("an unbounded loop stopped hopping at %d: %+v", item.Hops, item)
		}
		var err error
		if item, err = lp.Run(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	if item.Hops != loop.DefaultMaxHandoffs+2 {
		t.Fatalf("hops = %d after %d sessions", item.Hops, loop.DefaultMaxHandoffs+2)
	}
}

// TestHandoffRematerializesTheContextSet covers what the two resumes do
// differently to a workspace, and which the older one never had to face.
//
// A waiting resume adopts a tree and does NOT run the builder — nothing
// re-materializes anything. An unfinished resume adopts a tree and DOES, into
// a context directory the previous session left behind read-only. Least
// privilege depends on the second session's set being laid fresh rather than
// extracted over the first's: what survived would be material the agent can
// read and was not granted this time.
func TestHandoffRematerializesTheContextSet(t *testing.T) {
	s, lp, router, _, _ := rig(t, "halfway")
	ctx := context.Background()
	b := lp.Builder.(*Builder)
	// The mirror the rig fetched is "app"; granting it to itself as read-only
	// reference material is enough to exercise the extraction twice.
	head, err := b.Mirror.Resolve(ctx, forge.Repo{Name: "app"}, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	b.Composer = &agent.Composer{Source: agent.StaticLayers{{
		Name: "builder", Scope: agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: "d-role"},
		Instructions: "You build changes for the shop box.",
		Context:      agent.ContextSet{Repos: []agent.ContextRepo{{Repo: "app", Commit: string(head)}}},
	}}}

	item := runItem(t, s, lp, router, "issue-hop-ctx", "write the widget")
	if item.State != queue.StateHandedOff {
		t.Fatalf("first session: %+v", item)
	}
	b.Env = []string{"ACP_FAKE=finisher"}
	if item, err = lp.Run(ctx, item); err != nil {
		t.Fatalf("the second session could not materialize into the adopted tree: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("second session: %+v", item)
	}
}
