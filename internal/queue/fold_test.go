package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// narrate appends a record of a NON-state kind to an item's chain, the way
// the ACP builder and the loop do: a body that is whatever the event needed
// to say, in the same scope as the item's transitions.
func narrate(t *testing.T, q *Queue, w WorkItem, kind, body string) {
	t.Helper()
	if _, err := q.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: w.Tenant, Scope: scope(w.ID), Kind: kind, Subject: w.ID,
		Actor: "mason", Body: []byte(body),
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFoldSkipsNarrativeRecords is the regression that cost a factory its
// morning. An agent escalated — correctly, the issue was already done — and
// the plain-prose reason it gave landed in the item's own chain. Every reader
// unmarshalled every record, so one honest escalation made Load fail forever:
// the drain aborted on it 31 ticks running, the queue listing and the
// exception queue both 500'd, and the parked item nobody could see was
// re-queued by intake on every tick.
//
// The fold selects by kind now. Prose in the chain is a record, not a fault.
func TestFoldSkipsNarrativeRecords(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}

	// The three shapes the real writers produce, in the order they occur.
	narrate(t, qu, w, "agent-run", `{"agentref":"deadbeef","role":"builder","mode":"acceptEdits","seat":"mason"}`)
	narrate(t, qu, w, "agent-result", `{"stop":"end_turn","tool_calls":20,"tokens":918520,"metered":true}`)
	narrate(t, qu, w, "escalated", "Issue #3 is already satisfied by the code as it stands — commit ab8ccfb names it.")

	if _, err := qu.Park(ctx, w, lease, "escalated by mason"); err != nil {
		t.Fatal(err)
	}

	got, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatalf("Load over a chain carrying narrative records: %v", err)
	}
	if got.ID != "wi-1" || got.State != StateParked {
		t.Fatalf("fold lost the item: %+v", got)
	}
	if got.Note != "escalated by mason" {
		t.Fatalf("note = %q, want the park reason", got.Note)
	}
}

// TestFoldKeepsTheRunningItemVisible is the other half of the same defect,
// and the more insidious one: mid-run the last record is an agent-run, whose
// body is valid JSON that is not a WorkItem. Unmarshalling it zeroed every
// field, so the one item actually executing was the one item the control room
// could not name — a blank row, and `?state=claimed` answering with nothing.
func TestFoldKeepsTheRunningItemVisible(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-2")
	w, _, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	narrate(t, qu, w, "agent-run", `{"agentref":"deadbeef","seat":"mason"}`)

	got, err := qu.Load(ctx, tn, "wi-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "wi-2" || got.State != StateClaimed || got.Task != "wi-2" {
		t.Fatalf("the running item folded to %+v — id, state and task must survive its own agent-run record", got)
	}
}

// TestStateWritersUseStateKinds holds the writers to the contract from the
// other side. Every transition writes the whole item, so every transition's
// kind must be one the fold reads; a new state kind that nobody adds to
// stateKinds would be skipped in silence, and the item would fold to the last
// state before it — stale, plausible, and wrong.
func TestStateWritersUseStateKinds(t *testing.T) {
	qu, ctx := q(t), context.Background()

	// Drive one item through every settle the queue can write.
	for _, tc := range []struct {
		id     string
		settle func(WorkItem, forge.Lease) error
	}{
		{"wi-done", func(w WorkItem, l forge.Lease) error {
			_, err := qu.Done(ctx, w, l, forge.Commit("abc123"))
			return err
		}},
		{"wi-parked", func(w WorkItem, l forge.Lease) error {
			_, err := qu.Park(ctx, w, l, "parked")
			return err
		}},
		{"wi-waiting", func(w WorkItem, l forge.Lease) error {
			_, err := qu.Wait(ctx, w, l, "waiting on a gate")
			return err
		}},
		{"wi-failed", func(w WorkItem, l forge.Lease) error {
			_, err := qu.Fail(ctx, w, l, "failed")
			return err
		}},
	} {
		w := submit(t, qu, tc.id)
		w, lease, err := qu.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if err := tc.settle(w, lease); err != nil {
			t.Fatal(err)
		}
		for rec, err := range qu.Store.Log().Since(ctx, tn, scope(tc.id), 1) {
			if err != nil {
				t.Fatal(err)
			}
			var probe WorkItem
			whole := json.Unmarshal(rec.Body, &probe) == nil && probe.ID == tc.id
			if whole != StateRecord(rec.Kind) {
				t.Errorf("%s: kind %q writes a whole item = %v, but StateRecord says %v",
					tc.id, rec.Kind, whole, StateRecord(rec.Kind))
			}
		}
	}
}

// TestLoadDistinguishesAbsenceFromFailure is what intake keys on. "No item
// yet" is the only answer that may create work; anything else must propagate,
// because reading a failure as absence is how one unreadable chain became a
// new queued record on every tick.
func TestLoadDistinguishesAbsenceFromFailure(t *testing.T) {
	qu, ctx := q(t), context.Background()

	_, err := qu.Load(ctx, tn, "wi-never-existed")
	if !errors.Is(err, ErrNoItem) {
		t.Fatalf("missing item: err = %v, want ErrNoItem", err)
	}

	// A state record that will not parse is corruption, and must NOT read as
	// absence — that is the difference between "create one" and "stop".
	w := submit(t, qu, "wi-corrupt")
	narrate(t, qu, w, "claimed", "this is not a work item")
	_, err = qu.Load(ctx, tn, "wi-corrupt")
	if err == nil {
		t.Fatal("a corrupt state record loaded cleanly")
	}
	if errors.Is(err, ErrNoItem) {
		t.Fatalf("corruption reported as absence: %v", err)
	}
}
