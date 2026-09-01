package agent

import (
	"context"
	"strings"
	"testing"
)

func prior() Prior {
	return Prior{
		Run: "wi-red", Repo: "app", Branch: "main", Sha: "3f2a1b",
		Checks: []string{"build-and-test"}, Reason: "build-and-test: fail (TestWidget)",
		Notes: []string{"the widget package expects blue as the default"},
	}
}

// TestNothingToCarryIsNoLayer: an empty prior must not become a layer that
// says nothing. A layer moves the agentref, and a run woken knowing nothing
// is not a different agent from a run woken knowing nothing.
func TestNothingToCarryIsNoLayer(t *testing.T) {
	if _, ok := (Prior{}).Layer(); ok {
		t.Fatal("an empty prior rendered a layer")
	}
	// Where work happens is not something the last attempt taught anyone.
	if _, ok := (Prior{Repo: "app", Branch: "main"}).Layer(); ok {
		t.Fatal("a repo and a branch alone rendered a layer")
	}
	if _, ok := (Prior{Run: "wi-red"}).Layer(); !ok {
		t.Fatal("a named predecessor is something to carry")
	}
}

// TestCarriedAttemptSaysWhatThePredecessorKnew: every fact the successor
// would otherwise re-derive from the diff, in the layer rather than in the
// task string.
func TestCarriedAttemptSaysWhatThePredecessorKnew(t *testing.T) {
	l, ok := prior().Layer()
	if !ok {
		t.Fatal("nothing carried")
	}
	if l.Scope != ScopeTask || l.Source.Kind != SourceInline || l.Source.Digest == "" {
		t.Fatalf("the carried layer must be an addressable task-scope layer: %+v", l)
	}
	for _, want := range []string{
		"wi-red",                   // whose attempt it was
		"app@3f2a1b",               // what it produced
		"main",                     // where that still stands
		"build-and-test",           // which check reported it
		"TestWidget",               // and what the check said
		"expects blue",             // what the predecessor handed off
		"nothing rolled that back", // the branch is intact: resume, do not redo
	} {
		if !strings.Contains(l.Instructions, want) {
			t.Fatalf("the carried attempt lost %q:\n%s", want, l.Instructions)
		}
	}
}

// TestCarriedContextIsInTheAgentref: continuity is part of the bundle digest,
// and so is this. Two runs of the same repair with different accounts of what
// preceded them are different agents, and the record has to say so — that is
// the whole reason this is a layer rather than a paragraph glued to the task.
func TestCarriedContextIsInTheAgentref(t *testing.T) {
	ctx := context.Background()
	c := &Composer{Source: layers()}
	resolve := func(extra ...Layer) Bundle {
		t.Helper()
		req := request()
		req.Extra = extra
		b, err := c.Resolve(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	cold := resolve()
	carried, ok := prior().Layer()
	if !ok {
		t.Fatal("nothing carried")
	}
	warm := resolve(carried)
	if warm.Ref == cold.Ref {
		t.Fatal("a run woken with its predecessor's attempt shares a ref with one woken cold")
	}
	if !strings.Contains(warm.Instructions, "expects blue") {
		t.Fatalf("the bundle does not carry the predecessor's handoff:\n%s", warm.Instructions)
	}

	// And the digest tracks the CONTENT, not the presence of a layer: a
	// predecessor that learned something else is a different agent again.
	other := prior()
	other.Notes = []string{"the widget package expects red as the default"}
	l, _ := other.Layer()
	if again := resolve(l); again.Ref == warm.Ref {
		t.Fatal("different carried notes resolved to the same agentref")
	}
}
