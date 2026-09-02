package acp

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// collect runs a scenario with Progress wired and returns both accounts: the
// frames as they arrived, and the Result the run settled with.
func collect(t *testing.T, scenario string) ([]Update, Result) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	var got []Update
	rt := runtimeFor(t, scenario)
	rt.Progress = func(u Update) {
		mu.Lock()
		got = append(got, u)
		mu.Unlock()
	}
	res, err := rt.Run(ctx, bundle(), "add the widget package")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(got), res
}

func kinds(us []Update) []UpdateKind {
	out := make([]UpdateKind, 0, len(us))
	for _, u := range us {
		out = append(out, u.Kind)
	}
	return out
}

func pick(us []Update, k UpdateKind, f func(Update) string) []string {
	var out []string
	for _, u := range us {
		if u.Kind == k {
			out = append(out, f(u))
		}
	}
	return out
}

// TestTheRunIsVisibleWhileItIsStillRunning is the black box opened.
//
// A claimed item said `state=claimed head=` for the whole length of a run, and
// the reason was not that nobody had thought to show more: the runtime knew
// everything and reported none of it until the session ended. This replays the
// PINNED dialect — the same frames the conformance test asserts the parser
// against — and asserts the live account of it, so a dialect that moves breaks
// the live view here rather than quietly emptying it in production.
func TestTheRunIsVisibleWhileItIsStillRunning(t *testing.T) {
	got, _ := collect(t, "replay:"+pinPath(t))

	// What it said, then what it did. The rhythm is the whole design: a frame
	// per message chunk would be a log dump nobody can read, and one message
	// at the end would be no live view at all.
	if want := []UpdateKind{
		UpdateMessage, UpdateTool, // "Reading the tree first." then Read
		UpdateTool,    // Bash, announced update-only
		UpdateDenied,  // DeleteProd, refused at the permission ask
		UpdateTool,    // Edit
		UpdateMessage, // the closing account, flushed at the end
	}; !slices.Equal(kinds(got), want) {
		t.Fatalf("frames = %v, want %v", kinds(got), want)
	}
	if want := []string{"Read", "Bash", "Edit"}; !slices.Equal(pick(got, UpdateTool, func(u Update) string { return u.Tool }), want) {
		t.Errorf("tool frames = %v, want %v", pick(got, UpdateTool, func(u Update) string { return u.Tool }), want)
	}
	if first := pick(got, UpdateMessage, func(u Update) string { return u.Text })[0]; !strings.Contains(first, "Reading the tree first.") {
		t.Errorf("first message frame = %q, want the text that preceded the first tool call", first)
	}
}

// TestTheLiveAccountAndTheSettledOneAgree is the property that makes the live
// view worth trusting.
//
// A room fed by a second, parallel reading of the stream would be a second
// account of the run, free to differ from the one the chain records — and the
// difference would surface as an operator who watched forty tool calls reading
// a settled record that says thirty. Both accounts come off the same dedup
// here, so the frames a watcher saw ARE the tally the run is charged with.
func TestTheLiveAccountAndTheSettledOneAgree(t *testing.T) {
	got, res := collect(t, "replay:"+pinPath(t))

	live := pick(got, UpdateTool, func(u Update) string { return u.Tool })
	if !slices.Equal(live, res.ToolCalls) {
		t.Errorf("live tool calls %v, settled %v — the room and the chain disagree about one run", live, res.ToolCalls)
	}
	denied := pick(got, UpdateDenied, func(u Update) string { return u.Tool })
	if !slices.Equal(denied, res.Denied) {
		t.Errorf("live denials %v, settled %v", denied, res.Denied)
	}
	// The message frames are the agent's text, split at its actions and
	// nothing else: concatenated they must be exactly what the run reports.
	if joined := strings.Join(pick(got, UpdateMessage, func(u Update) string { return u.Text }), ""); joined != res.Text {
		t.Errorf("message frames concatenate to %q, run text %q", joined, res.Text)
	}
}

// TestProgressCarriesTheBoxVerbs: an escalation or a handoff is the most
// important thing a run ever says, and until the session ended it was the one
// thing nobody outside could hear. Both verbs, both with the agent's reason.
func TestProgressCarriesTheBoxVerbs(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		kind     UpdateKind
		reason   string
	}{
		{"escalate", UpdateEscalate, "task needs credentials I do not hold"},
		{"handoff", UpdateHandoff, "context is nearly full and the migration is half applied"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			got, _ := collect(t, tc.scenario)
			var found *Update
			for i, u := range got {
				if u.Kind == tc.kind {
					found = &got[i]
				}
			}
			if found == nil {
				t.Fatalf("no %s frame in %v — the verb reached the Result and nobody watching heard it", tc.kind, kinds(got))
			}
			if found.Reason != tc.reason {
				t.Errorf("%s reason = %q, want %q", tc.kind, found.Reason, tc.reason)
			}
		})
	}
}

// TestNoProgressHookIsNoWork: the hook is optional, and a runtime without one
// must behave exactly as it did before there was one — including the message
// accumulation, which now runs through a cursor.
func TestNoProgressHookIsNoWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "replay:"+pinPath(t)).Run(ctx, bundle(), "add the widget package")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Added the widget package") {
		t.Errorf("text without a Progress hook = %q", res.Text)
	}
	if want := []string{"Read", "Bash", "Edit"}; !slices.Equal(res.ToolCalls, want) {
		t.Errorf("tool calls = %v, want %v", res.ToolCalls, want)
	}
}
