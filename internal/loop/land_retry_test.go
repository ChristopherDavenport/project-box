package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// transientLanderNTimes fails Land with a [git.Transient] error (a GitHub
// 5xx) for the first failFor calls, then delegates to inner untouched — the
// PRLander seam's stand-in for another service's bad minute that clears on
// its own.
type transientLanderNTimes struct {
	inner   *git.Lander
	n       int
	failFor int
}

func (s *transientLanderNTimes) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	s.n++
	if s.n <= s.failFor {
		return forge.Landing{}, &git.HTTPError{
			Method: "POST", Endpoint: "/repos/o/r/pulls",
			StatusCode: 502, Status: "502 Bad Gateway", Body: "bad gateway",
		}
	}
	return s.inner.Land(ctx, cs)
}

// nonTransientLander always fails Land with a [git.Transient]-false error (a
// GitHub 422) and never reaches inner — the shape of a validation failure or
// a fence refusal, nobody's infrastructure trouble to retry.
type nonTransientLander struct {
	n int
}

func (s *nonTransientLander) Land(context.Context, forge.ChangeSet) (forge.Landing, error) {
	s.n++
	return forge.Landing{}, &git.HTTPError{
		Method: "POST", Endpoint: "/repos/o/r/pulls",
		StatusCode: 422, Status: "422 Unprocessable Entity", Body: "validation failed",
	}
}

// tryRecordCount counts kind on the chain for subject across every scope —
// [git.Lander]'s success record and the loop's own land-try record land in
// different scopes, so BySubject (not the workitem scope alone) is what
// evaluateGate's own tests use for the same reason.
func tryRecordCount(t *testing.T, e *env, subject, kind string) int {
	t.Helper()
	n := 0
	for r, err := range e.store.Log().BySubject(context.Background(), tn, subject) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// TestLandTransientFailsTwiceThenLands is issue #161's core case: a landing
// that errors with the forge's own trouble is re-asked, not parked, up to
// the default bound — two failures then a third try that reaches the
// remote lands the item exactly as if the first try had.
func TestLandTransientFailsTwiceThenLands(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.LandBackoff = func(int) time.Duration { return 0 }
	stub := &transientLanderNTimes{failFor: 2}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander {
		stub.inner = inner
		return stub
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-land-transient"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-land-transient", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a landing that clears within the retry bound should land, got %s (%s)", item.State, item.Note)
	}
	if stub.n != 3 {
		t.Fatalf("Land should have been asked three times (two failures then a landing): %d", stub.n)
	}
	if got := tryRecordCount(t, e, item.Head, "land-try"); got != 2 {
		t.Fatalf("the two failing tries should each be on the chain as land-try, got %d", got)
	}
	if got := tryRecordCount(t, e, item.Head, "land"); got != 1 {
		t.Fatalf("the landing itself should be recorded exactly once, got %d", got)
	}
}

// TestLandNonTransientParksWithoutRetry is #161's other half: an error
// [git.Transient] does not positively place as the forge's own trouble
// parks on the first try, exactly as before it had a retry to reach for —
// a 422 says something about the request, not about GitHub's minute.
func TestLandNonTransientParksWithoutRetry(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.LandBackoff = func(int) time.Duration { return 0 }
	stub := &nonTransientLander{}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return stub }

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-land-nontransient"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-land-nontransient", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked || !strings.HasPrefix(item.Note, "land: ") {
		t.Fatalf("a non-transient landing error should park at the land step, got %s (%s)", item.State, item.Note)
	}
	if item.Built == "" {
		t.Fatal("a land-step park dropped the commit the remote refused")
	}
	if stub.n != 1 {
		t.Fatalf("a non-transient error must never be retried: Land called %d times", stub.n)
	}
	if got := tryRecordCount(t, e, item.Built, "land-try"); got != 1 {
		t.Fatalf("the one failing try should still be on the chain as land-try, got %d", got)
	}
}

// TestLandTransientOverBoundParksThenRetryResumesAtLanding is #161's bound
// case: a landing that keeps erroring transiently past every retry parks
// keeping the commit, naming the last error and the try count, and a
// person's Retry resumes at the landing — no rebuild, no re-gating, no
// verdict re-minted — exactly as a refused landing already did before this
// unit (issue #84).
func TestLandTransientOverBoundParksThenRetryResumesAtLanding(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	builder := &countingBuilder{}
	e.loop.Builder = builder
	gateRuns := 0
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{failNTimesGate{name: "build-and-test", n: &gateRuns, failFor: 0}}
	}
	e.loop.LandBackoff = func(int) time.Duration { return 0 }
	// LandRetries left at its default (2): three tries total. failFor=3
	// exhausts every try in the first Run, then the SAME stub's fourth call
	// — made by the retry's second Run — lands.
	stub := &transientLanderNTimes{failFor: 3}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander {
		stub.inner = inner
		return stub
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-land-overbound"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-land-overbound", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a landing that never clears the bound should park, got %s (%s)", item.State, item.Note)
	}
	if item.Built == "" {
		t.Fatal("parking on an exhausted land retry should keep its resume point")
	}
	head := item.Built
	if !strings.Contains(item.Note, "land: ") || !strings.Contains(item.Note, "502") {
		t.Fatalf("the park note should name the last error: %q", item.Note)
	}
	if !strings.Contains(item.Note, "3") {
		t.Fatalf("the park note should say how many tries it took: %q", item.Note)
	}
	if stub.n != 3 {
		t.Fatalf("Land should have been asked exactly 3 times before parking: %d", stub.n)
	}
	if builder.n != 1 || gateRuns != 1 {
		t.Fatalf("before the retry: %d builds, %d gate runs", builder.n, gateRuns)
	}
	if got := tryRecordCount(t, e, head, "land-try"); got != 3 {
		t.Fatalf("every one of the three failing tries should be on the chain as land-try, got %d", got)
	}
	if got := tryRecordCount(t, e, head, "gate"); got != 1 {
		t.Fatalf("a land retry must never re-mint the gate's verdict: %d gate records", got)
	}

	retried, err := q.Retry(ctx, tn, "wi-land-overbound", queue.Identity{Name: "chris"}, "the forge should be back by now")
	if err != nil {
		t.Fatal(err)
	}
	item, err = e.loop.Run(ctx, retried)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("the retry should land the kept commit, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("the retry rebuilt: %d builds", builder.n)
	}
	if gateRuns != 1 {
		t.Fatalf("the retry re-ran the gate on a commit it had already passed: %d gate runs", gateRuns)
	}
	if got := tryRecordCount(t, e, item.Head, "gate"); got != 1 {
		t.Fatalf("the retry re-minted the gate's verdict: %d gate records", got)
	}
	if got := tryRecordCount(t, e, item.Head, "land"); got != 1 {
		t.Fatalf("the landing itself should be recorded exactly once across both runs, got %d", got)
	}
}
