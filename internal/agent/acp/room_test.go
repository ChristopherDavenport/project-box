package acp

import (
	"context"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestTheRoomReadsWhatTheBuilderWrote runs a real item through the real loop
// with a (protocol-real) agent, and then asks the control room what it can say
// about it — which is the only way to test a contract held by two packages
// through a JSON record and an in-memory bus.
//
// It is deliberately the whole path. The record's field names, the layer
// order, the live bus's fan-in and the room's fold are four places that must
// agree about one run, and each of them is independently plausible while
// wrong: a room reading `agentref` from a record that writes `agent_ref` shows
// a blank where it should show identity, and nothing anywhere fails.
func TestTheRoomReadsWhatTheBuilderWrote(t *testing.T) {
	s, lp, router, _, _ := rig(t, "toolsy")
	ctx := context.Background()

	live := &control.Live{}
	b := lp.Builder.(*Builder)
	b.Progress = func(_ context.Context, run string, u Update) {
		live.Append(run, control.Frame{
			Kind: string(u.Kind), Tool: u.Tool, Text: u.Text, Reason: u.Reason,
		})
	}

	item := runItem(t, s, lp, router, "issue-live", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("item: %+v", item)
	}

	room := &control.Room{Store: s, Tenant: btn, Actor: "chris", Live: live}
	sn, err := room.Session(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Identity, off the agent-run record the builder wrote.
	if sn.Seat != "mason" {
		t.Errorf("seat = %q, want mason — the room cannot read the record the builder writes", sn.Seat)
	}
	if len(sn.AgentRef) != 64 {
		t.Errorf("agentref = %q, want the resolved digest", sn.AgentRef)
	}
	// The composed layers, in application order. The digest proves WHICH agent
	// ran and can never say what that agent was; re-resolving to find out
	// needs the same material at the same commits, which is exactly what a
	// record exists so nobody has to have.
	if len(sn.Layers) == 0 {
		t.Fatal("no layers on the session: the agentref is a hash, and alone it answers nothing")
	}
	last := sn.Layers[len(sn.Layers)-1]
	if last.Scope != "task" {
		t.Errorf("last layer scope = %q, want task — layers are recorded in APPLICATION order, general to specific", last.Scope)
	}
	var role *control.Layer
	for i, l := range sn.Layers {
		if l.Name == "builder" {
			role = &sn.Layers[i]
		}
	}
	if role == nil || role.Digest != "d-role" || role.Source != "inline" {
		t.Errorf("role layer = %+v, want the inline builder layer at d-role", role)
	}

	// Communication: the run's stream, fanned in as it happened.
	if !sn.Watching {
		t.Fatal("watching = false for a run this bus fed")
	}
	if len(sn.Timeline) == 0 {
		t.Fatal("empty timeline for a run that made tool calls")
	}
	if sn.Timeline[0].Kind != string(UpdateStarted) {
		t.Errorf("first frame = %q, want %q — a unit must reach a live view at its first instant, not its first tool call",
			sn.Timeline[0].Kind, UpdateStarted)
	}
	tools, denied := map[string]bool{}, map[string]bool{}
	for _, f := range sn.Timeline {
		switch f.Kind {
		case string(UpdateTool):
			tools[f.Tool] = true
		case string(UpdateDenied):
			denied[f.Tool] = true
		}
	}
	if !tools["Bash"] {
		t.Errorf("timeline tools = %v, want the agent's Bash call", tools)
	}
	// A refusal is the most important thing the stream ever carries, and it
	// was the least visible: policy killed or refused it in a runtime nobody
	// could see into, and the room learned about it when the item settled.
	if !denied["DeleteProd"] {
		t.Errorf("timeline denials = %v, want the refused DeleteProd", denied)
	}

	// And the settled record says the same thing, which is what makes the
	// live view a window on the run rather than a second account of it.
	var settled string
	for r, err := range s.Log().Since(ctx, btn, "workitem/"+item.ID, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-result" {
			settled = string(r.Body)
		}
	}
	for _, want := range []string{`"Bash"`, `"DeleteProd"`} {
		if !strings.Contains(settled, want) {
			t.Errorf("agent-result = %s, want %s — the room showed it live and the chain must carry it", settled, want)
		}
	}
}
