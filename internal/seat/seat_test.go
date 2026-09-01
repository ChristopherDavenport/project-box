package seat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func open(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestHandoffOfOneNamedRun: repair work is not the seat's next run, it is a
// successor to one PARTICULAR run — so the notes it needs are keyed by that
// run rather than by the seat's recency window, which the wake layer already
// answers and which would hand it whatever the seat did last instead.
func TestHandoffOfOneNamedRun(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	st := &Seat{Name: "mason", Base: &knowledge.Base{Store: s, Tenant: tn}}

	red := queue.WorkItem{ID: "wi-red", Tenant: tn, Box: "shop", Ingress: "ing-1",
		State: queue.StateDone, Head: "abc123"}
	if err := st.RecordHandoff(ctx, red, "", []string{"the migration has to run before the deploy"}); err != nil {
		t.Fatal(err)
	}
	later := queue.WorkItem{ID: "wi-other", Tenant: tn, Box: "shop", Ingress: "ing-2",
		State: queue.StateDone, Head: "def456"}
	if err := st.RecordHandoff(ctx, later, "", []string{"unrelated: the linter wants gofmt -s"}); err != nil {
		t.Fatal(err)
	}

	notes, err := st.HandoffFrom(ctx, "wi-red")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "migration") {
		t.Fatalf("the predecessor's own notes, and only those: %v", notes)
	}
	// A run that left nothing is silence, not an error: not every predecessor
	// had anything to say.
	if notes, err := st.HandoffFrom(ctx, "wi-nobody"); err != nil || len(notes) != 0 {
		t.Fatalf("unknown run: %v %v", notes, err)
	}
	// A seat's memory stays its own. A successor sitting elsewhere carries
	// the facts of the attempt and not the notes, because reading another
	// seat's scope is the cross-scope read the knowledge plane refuses.
	elsewhere := &Seat{Name: "hammer", Base: st.Base}
	if notes, err := elsewhere.HandoffFrom(ctx, "wi-red"); err != nil || len(notes) != 0 {
		t.Fatalf("one seat read another's handoff: %v %v", notes, err)
	}
}

// TestSecondRunReceivesTheHandoff is the Phase 12 exit criterion: run one
// hands off; run two wakes with the note in its resolved bundle — and the
// continuity is part of the agentref, so different memories are different
// agents on the record.
func TestSecondRunReceivesTheHandoff(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	st := &Seat{Name: "mason", Base: &knowledge.Base{Store: s, Tenant: tn}}

	// Run one settles and hands off.
	done := queue.WorkItem{ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1",
		State: queue.StateDone, Head: "abc123"}
	if err := st.RecordHandoff(ctx, done, "", []string{
		"the flaky test is TestRetry — it needs the fake clock, not sleeps",
		"   ", // blank notes are dropped
	}); err != nil {
		t.Fatal(err)
	}

	wake, ok, err := st.WakeLayer(ctx, 5)
	if err != nil || !ok {
		t.Fatalf("wake: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(wake.Instructions, "flaky test is TestRetry") {
		t.Fatalf("handoff not in wake layer:\n%s", wake.Instructions)
	}

	// Run two resolves WITH the wake layer: the bundle carries the memory.
	c := &agent.Composer{Source: agent.StaticLayers{{
		Name: "builder", Scope: agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: "d-role"},
		Instructions: "Build the change.",
	}}}
	req := agent.Request{Tenant: tn, Role: agent.RoleBuilder, Box: "shop", Task: "fix the flaky test"}
	req.Extra = []agent.Layer{wake}
	with, err := c.Resolve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with.Instructions, "TestRetry") {
		t.Fatal("second run's bundle lacks its predecessor's note")
	}
	// Different memory, different agentref — the evidence can tell.
	without, _ := c.Resolve(ctx, agent.Request{Tenant: tn, Role: agent.RoleBuilder, Box: "shop", Task: "fix the flaky test"})
	if with.Ref == without.Ref {
		t.Fatal("continuity is invisible to the agentref")
	}
	// A fresh seat has no history: a first day, not an error.
	if _, ok, err := (&Seat{Name: "newhire", Base: st.Base}).WakeLayer(ctx, 5); err != nil || ok {
		t.Fatalf("fresh seat: ok=%v err=%v", ok, err)
	}
	// And the seat's memory is scope-private: another seat's grant sees none of it.
	other, _ := st.Base.Retrieve(ctx, knowledge.Query{Granted: []string{"seat/newhire"}})
	if len(other) != 0 {
		t.Fatal("a seat read another seat's memory")
	}
}
