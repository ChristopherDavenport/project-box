package agent

import (
	"context"
	"fmt"
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

// TestVerdictsRenderOldestFirstWithReasonsWhole. A rebuild is handed the
// gate's own words for every refused attempt, not a park note's first line
// of the latest one — so both attempts' full, multi-line reasons must appear
// in order.
func TestVerdictsRenderOldestFirstWithReasonsWhole(t *testing.T) {
	p := Prior{
		Run: "wi-1",
		Verdicts: []PriorVerdict{
			{Gate: "review", Decision: "fail", Reason: "the regex cannot match\nthe list-item form", Attempt: 1},
			{Gate: "review", Decision: "fail", Reason: "still cannot match\nthe nested list case", Attempt: 2},
		},
	}
	l, ok := p.Layer()
	if !ok {
		t.Fatal("nothing carried")
	}
	first := strings.Index(l.Instructions, "the regex cannot match")
	second := strings.Index(l.Instructions, "still cannot match")
	if first < 0 || second < 0 {
		t.Fatalf("both attempts' reasons must appear whole:\n%s", l.Instructions)
	}
	if first > second {
		t.Fatalf("attempts must render oldest first:\n%s", l.Instructions)
	}
	for _, want := range []string{
		"the regex cannot match\nthe list-item form",
		"still cannot match\nthe nested list case",
		"attempt 1", "attempt 2",
		"attempt 3 of 3",
		"the review gate refused",
	} {
		if !strings.Contains(l.Instructions, want) {
			t.Fatalf("lost %q:\n%s", want, l.Instructions)
		}
	}
}

// TestVerdictReasonOverBoundIsCutAndSaidSo mirrors the reviewer's own
// diff-bound pattern: a reason too large to hand over whole is cut at a line
// boundary, and the layer itself says it was cut and at what size — a
// builder that thinks it read the whole reason when it did not is worse off
// than one that knows it was shown only part.
func TestVerdictReasonOverBoundIsCutAndSaidSo(t *testing.T) {
	long := strings.Repeat("line of gate output\n", MaxVerdictReasonBytes/len("line of gate output\n")+10)
	p := Prior{
		Run:      "wi-1",
		Verdicts: []PriorVerdict{{Gate: "build-and-test", Decision: "fail", Reason: long, Attempt: 1}},
	}
	l, ok := p.Layer()
	if !ok {
		t.Fatal("nothing carried")
	}
	if strings.Contains(l.Instructions, long) {
		t.Fatal("an over-bound reason was handed over whole instead of cut")
	}
	if !strings.Contains(l.Instructions, fmt.Sprintf("%d bytes", MaxVerdictReasonBytes)) {
		t.Fatalf("the layer must state the bound it cut at:\n%s", l.Instructions)
	}
	if !strings.Contains(l.Instructions, "cut") {
		t.Fatalf("the layer must say the reason was cut:\n%s", l.Instructions)
	}
}

// TestOperatorNoteRendersSeparatelyFromVerdicts. A rebuild reopened over a
// judgement the operator disputes must not have that dispute read back as
// part of the judgement's own words — the note is attributed and stands on
// its own line.
func TestOperatorNoteRendersSeparatelyFromVerdicts(t *testing.T) {
	p := Prior{
		Run: "wi-1",
		Verdicts: []PriorVerdict{
			{Gate: "review", Decision: "fail", Reason: "the regex cannot match the list-item form", Attempt: 1},
		},
		RetriedBy: "chris",
		RetryNote: "rebuild: admit the list-item form",
	}
	l, ok := p.Layer()
	if !ok {
		t.Fatal("nothing carried")
	}
	if !strings.Contains(l.Instructions, "retried by chris: rebuild: admit the list-item form") {
		t.Fatalf("the operator's note must render as its own attributed line:\n%s", l.Instructions)
	}
	for _, line := range strings.Split(l.Instructions, "\n") {
		if strings.Contains(line, "the regex cannot match") && strings.Contains(line, "rebuild: admit") {
			t.Fatalf("the operator's note leaked into the verdict's own words: %q", line)
		}
	}
}

// TestAttemptOfAloneIsStillEmpty: a number alone wakes nobody knowing
// anything, so an otherwise-empty Prior with only Attempt/Of set must still
// count as empty and render no layer — the in-claim retry budget is not
// itself carried context.
func TestAttemptOfAloneIsStillEmpty(t *testing.T) {
	p := Prior{Attempt: 2, Of: 3}
	if !p.Empty() {
		t.Fatal("a prior with only Attempt/Of set reported non-empty")
	}
	if _, ok := p.Layer(); ok {
		t.Fatal("a prior with only Attempt/Of set rendered a layer")
	}
}

// TestVerdictsOrRetryAloneAreSomethingToCarry: neither field needs the other
// present to be worth a layer.
func TestVerdictsOrRetryAloneAreSomethingToCarry(t *testing.T) {
	if (Prior{}).Empty() == false {
		t.Fatal("a truly empty prior reported non-empty")
	}
	if _, ok := (Prior{Run: "wi-1", Verdicts: []PriorVerdict{{Gate: "g", Decision: "fail", Reason: "r", Attempt: 1}}}).Layer(); !ok {
		t.Fatal("a prior with verdicts is something to carry")
	}
	if _, ok := (Prior{Run: "wi-1", RetryNote: "rebuild"}).Layer(); !ok {
		t.Fatal("a prior with a retry note is something to carry")
	}
}
