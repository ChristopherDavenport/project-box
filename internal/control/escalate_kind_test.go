package control

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// parkUnder drives an item to the exception queue under a named kind.
func parkUnder(t *testing.T, w *clocked, id, k string, sev queue.Severity, reason string) {
	t.Helper()
	ctx := context.Background()
	item, err := w.queue.Submit(ctx, queue.WorkItem{
		ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Kind: k, Task: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := w.queue.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.queue.Park(ctx, item, lease, sev, reason); err != nil {
		t.Fatal(err)
	}
}

func declaredSets(t *testing.T) kind.Sets {
	t.Helper()
	pinned := agent.Source{Kind: agent.SourceGit, Repo: "r", Commit: "c", Path: "p", Digest: "d"}
	urgent := kind.Declaration{
		Name: "incident", Source: pinned,
		Escalation: kind.Escalation{
			Severity: queue.SeverityCritical, Objective: 15 * time.Minute,
			MaxReescalations: 1, Route: "pager-primary",
		},
		Completion: kind.Completion{Graph: true},
	}
	patient := kind.Declaration{
		Name: "dependency-bump", Source: pinned,
		Escalation: kind.Escalation{Objective: 48 * time.Hour, Route: "platform-rota"},
		Completion: kind.Completion{Graph: true},
	}
	set, err := kind.NewSet(kind.Capabilities{}, append(kind.Seeds(), urgent, patient)...)
	if err != nil {
		t.Fatal(err)
	}
	return kind.Sets{"shop": set}
}

// TestAnExceptionIsMeasuredAgainstItsOwnKindsPromise.
//
// #4's mechanics landed and had one number behind them: an incident's parked
// leaf and a routine dependency bump waited the same four hours, because the
// objective was the room's and there was nowhere for a kind to state its own.
// The declaration is that home.
func TestAnExceptionIsMeasuredAgainstItsOwnKindsPromise(t *testing.T) {
	w := clockedSetup(t, 4*time.Hour)
	ctx := context.Background()
	rec := &recorder{}
	e := &Escalator{Room: w.room, Notify: rec, Kinds: declaredSets(t)}

	parkUnder(t, w, "wi-inc", "incident", queue.SeverityMedium, "the runner is gone")
	parkUnder(t, w, "wi-bump", "dependency-bump", queue.SeverityMedium, "the bump does not build")

	// Half an hour: past the incident's fifteen minutes, nowhere near the
	// bump's forty-eight hours, and nowhere near the room's four.
	w.advance(30 * time.Minute)
	sent, err := e.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent %d notices, want 1: %+v", len(sent), sent)
	}
	n := sent[0]
	if n.Item != "wi-inc" {
		t.Fatalf("the wrong exception was told about: %s", n.Item)
	}
	if n.ObjectiveSec != int64((15 * time.Minute).Seconds()) {
		t.Fatalf("the room's objective was used, not the kind's: %d", n.ObjectiveSec)
	}
	// The FLOOR, applied where the parking stage was quieter than the
	// origination. A cascaded leaf inherits its cause's severity and knows
	// nothing about what opened the work upstream.
	if n.Severity != queue.SeverityCritical {
		t.Fatalf("an incident's exception reached the queue as %s", n.Severity)
	}
	if n.Kind != "incident" || n.Route != "pager-primary" {
		t.Fatalf("the notice cannot be routed: kind=%q route=%q", n.Kind, n.Route)
	}

	// The kind's bound, too: MaxReescalations 1 means two tellings and then
	// silence, whatever the room's default is.
	for i := 0; i < 5; i++ {
		w.advance(20 * time.Minute)
		if _, err := e.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	told := 0
	for _, s := range rec.all() {
		if s.Item == "wi-inc" {
			told++
		}
	}
	if told != 2 {
		t.Fatalf("the kind's bound was not applied: %d tellings", told)
	}

	// And the patient kind is still silent well past the room's objective,
	// because it asked for longer and got it.
	for _, s := range rec.all() {
		if s.Item == "wi-bump" {
			t.Fatal("a kind that asked for forty-eight hours was told at four")
		}
	}
}

// TestAKindThatSaysNothingGetsTheRoomsPolicy. A declaration that has not asked
// for a tighter promise has not asked for a looser one either.
func TestAKindThatSaysNothingGetsTheRoomsPolicy(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	rec := &recorder{}
	e := &Escalator{Room: w.room, Notify: rec, Kinds: declaredSets(t)}

	parkUnder(t, w, "wi-issue", kind.SeedIssue, queue.SeverityMedium, "a gate refused")
	w.advance(30 * time.Minute)
	if sent, err := e.Sweep(ctx); err != nil || len(sent) != 0 {
		t.Fatalf("told early: %d %v", len(sent), err)
	}
	w.advance(31 * time.Minute)
	sent, err := e.Sweep(ctx)
	if err != nil || len(sent) != 1 {
		t.Fatalf("the room's objective did not apply: %d %v", len(sent), err)
	}
	if sent[0].ObjectiveSec != int64(time.Hour.Seconds()) {
		t.Fatalf("objective: %d", sent[0].ObjectiveSec)
	}
	if sent[0].Severity != queue.SeverityMedium {
		t.Fatalf("a kind with no floor raised a severity: %s", sent[0].Severity)
	}
}

// TestAnItemWithNoKindStillEscalates. Items submitted before there was a
// decomposition stage carry no kind, and the sweep must not go quiet on
// exactly the queue that most needs reading.
func TestAnItemWithNoKindStillEscalates(t *testing.T) {
	w := clockedSetup(t, time.Hour)
	ctx := context.Background()
	rec := &recorder{}
	e := &Escalator{Room: w.room, Notify: rec, Kinds: declaredSets(t)}

	parkUnder(t, w, "wi-legacy", "", queue.SeverityHigh, "parked before the stage existed")
	// A kind nobody declared behaves the same way, and for the same reason.
	parkUnder(t, w, "wi-gone", "a-kind-that-was-removed", queue.SeverityHigh, "its declaration went missing")

	w.advance(2 * time.Hour)
	sent, err := e.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("the sweep went quiet: %d notices", len(sent))
	}
}
