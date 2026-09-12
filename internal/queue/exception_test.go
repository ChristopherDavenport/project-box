package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// park drives an item to the exception queue the way a run does.
func park(t *testing.T, qu *Queue, id, reason string) WorkItem {
	t.Helper()
	w := submit(t, qu, id)
	w, lease, err := qu.Claim(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	w, err = qu.Park(context.Background(), w, lease, SeverityMedium, reason)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestResolveClosesAnExceptionWithoutALanding. StateParked used to be written
// in one place and read in five, with nothing anywhere moving an item off it:
// every exception was permanent and the queue only grew. The agent's own
// verdict — "this issue is already implemented, close it" — was a
// recommendation the system had no verb to act on.
func TestResolveClosesAnExceptionWithoutALanding(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "escalated by mason: already implemented by ab8ccfb")

	got, err := qu.Resolve(ctx, tn, "wi-1", "chris", "agreed — closing the issue instead")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateResolved {
		t.Fatalf("state = %s, want resolved", got.State)
	}

	// It survives the fold, and it leaves the exception queue by doing so.
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != StateResolved || reloaded.Note != "agreed — closing the issue instead" {
		t.Fatalf("resolve did not survive the fold: %+v", reloaded)
	}

	// Resolved is NOT done: review sampling draws on the landings index, and
	// an auditor pulling a sampled item expects a sha to exist.
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/landings", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == "wi-1" {
			t.Fatal("a resolved item reached the landings index; sampling would offer an auditor a change that never landed")
		}
	}
}

// TestResolveIsAttributedToTheOperator. Every other transition on an item is
// the runner's; this is the one a person makes by hand, and an audit that
// could not tell them apart would be missing the only human act in the loop.
func TestResolveIsAttributedToTheOperator(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "gate flake")
	if _, err := qu.Resolve(ctx, tn, "wi-1", "chris", "not worth a retry"); err != nil {
		t.Fatal(err)
	}
	var actor string
	for rec, err := range qu.Store.Log().Since(ctx, tn, scope("wi-1"), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "resolved" {
			actor = rec.Actor
		}
	}
	if actor != "chris" {
		t.Fatalf("resolved record actor = %q, want the operator, not %q", actor, "runner-1")
	}
}

// TestRetryReturnsParkedWorkToTheQueue. Without this an item that parked for
// a transient reason — a flaky gate, a timeout — was work the factory could
// never attempt again.
func TestRetryReturnsParkedWorkToTheQueue(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "gate timed out")

	got, err := qu.Retry(ctx, tn, "wi-1", "chris", "gate was flaky; try again")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StatePending {
		t.Fatalf("state = %s, want pending", got.State)
	}
	// Claimable again is the whole point.
	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := qu.Claim(ctx, reloaded); err != nil {
		t.Fatalf("a retried item could not be claimed: %v", err)
	}
}

// parkKeeping parks an item the way a refused gate does now: with a resume
// point set on it first and ParkKeeping used instead of Park, so the park
// itself does not drop it. See [Queue.ParkKeeping].
func parkKeeping(t *testing.T, qu *Queue, id, reason string) WorkItem {
	t.Helper()
	w := submit(t, qu, id)
	w, lease, err := qu.Claim(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepCandidate("sha-built", "sha-base", []string{"build-and-test"})
	w, err = qu.ParkKeeping(context.Background(), w, lease, SeverityMedium, reason)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestRetryKeepsTheResumePointUnlessToldToRebuild. A gate-refusal park keeps
// its resume point precisely so a retry can ask the gate again instead of
// buying the same commit from an agent a second time — a plain retry note
// must not throw that away, or ParkKeeping's whole reason for existing goes
// with it.
func TestRetryKeepsTheResumePointUnlessToldToRebuild(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := parkKeeping(t, qu, "wi-1", "gate refused")
	if w.Built == "" {
		t.Fatal("setup: ParkKeeping dropped the resume point")
	}

	got, err := qu.Retry(ctx, tn, "wi-1", "chris", "let's ask the gate again")
	if err != nil {
		t.Fatal(err)
	}
	if got.Built != "sha-built" || got.BuiltBase != "sha-base" || len(got.PassedGates) != 1 {
		t.Fatalf("a plain retry dropped the resume point: %+v", got)
	}

	reloaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Built != "sha-built" || reloaded.BuiltBase != "sha-base" {
		t.Fatalf("the kept resume point did not survive the fold: %+v", reloaded)
	}
}

// TestRetryWithRebuildInTheNoteDropsTheResumePoint. The one case a retry
// still builds afresh: the operator's note itself says the commit, not just
// the gate, is the problem. Matched case-insensitively, per the issue's own
// wording.
func TestRetryWithRebuildInTheNoteDropsTheResumePoint(t *testing.T) {
	notes := []string{"rebuild from scratch", "Please REBUILD it", "the diff was bad — ReBuild"}
	for i, note := range notes {
		qu, ctx := q(t), context.Background()
		id := fmt.Sprintf("wi-rebuild-%d", i)
		parkKeeping(t, qu, id, "gate refused")

		got, err := qu.Retry(ctx, tn, id, "chris", note)
		if err != nil {
			t.Fatal(err)
		}
		if got.Built != "" || got.BuiltBase != "" || got.PassedGates != nil {
			t.Fatalf("note %q should have dropped the resume point: %+v", note, got)
		}
	}
}

// TestResolveAlwaysDropsTheResumePoint. Closing an exception is final: there
// is no retry left to hand a kept commit to, so unlike Retry, Resolve drops
// it regardless of what the note says.
func TestResolveAlwaysDropsTheResumePoint(t *testing.T) {
	qu, ctx := q(t), context.Background()
	parkKeeping(t, qu, "wi-1", "gate refused")

	got, err := qu.Resolve(ctx, tn, "wi-1", "chris", "already implemented, nothing to rebuild")
	if err != nil {
		t.Fatal(err)
	}
	if got.Built != "" || got.BuiltBase != "" || got.PassedGates != nil {
		t.Fatalf("resolve kept a resume point: %+v", got)
	}
}

// TestOnlyAnExceptionCanBeReopened. An operator resolving something still
// running would race the runner holding its lease.
func TestOnlyAnExceptionCanBeReopened(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	if _, _, err := qu.Claim(ctx, w); err != nil {
		t.Fatal(err)
	}

	for _, verb := range []string{"resolve", "retry"} {
		var err error
		if verb == "resolve" {
			_, err = qu.Resolve(ctx, tn, "wi-1", "chris", "closing")
		} else {
			_, err = qu.Retry(ctx, tn, "wi-1", "chris", "again")
		}
		if err == nil {
			t.Fatalf("%s took a claimed item off a queue it was never on", verb)
		}
		if !strings.Contains(err.Error(), "not parked") {
			t.Fatalf("%s: err = %v, want it to name the state", verb, err)
		}
	}
}

// TestResolveDemandsAReason. Closing an exception is a judgement, and an
// audit needs whose and why — the same bar stop and resume are held to.
func TestResolveDemandsAReason(t *testing.T) {
	qu, ctx := q(t), context.Background()
	park(t, qu, "wi-1", "escalated")
	if _, err := qu.Resolve(ctx, tn, "wi-1", "chris", ""); err == nil {
		t.Fatal("an exception was closed with no reason on the record")
	}
}

// TestHandoffKeepsTheWorkAndTheItem is the queue's half of issue #12: a
// session ended, the item did not.
//
// The two things it must not do are the two ways this state could have been
// spelled with an existing one. Parking it would file work needing nobody in
// the queue that exists to name work needing somebody — the argument the
// gate's pending decision already made about waiting, on a third axis. And
// dropping the resume point would throw away the change the session got
// partway through, which is the escalate-shaped outcome the verb was added to
// avoid.
func TestHandoffKeepsTheWorkAndTheItem(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-hop")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepUnfinished("abc123", "base456")

	w, err = qu.HandOff(ctx, w, lease, "out of room with the migration half applied")
	if err != nil {
		t.Fatal(err)
	}
	if w.State != StateHandedOff || w.Hops != 1 {
		t.Fatalf("handoff: %+v", w)
	}

	got, err := qu.Load(ctx, tn, "wi-hop")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateHandedOff || got.Hops != 1 {
		t.Fatalf("the hop did not survive the fold: %+v", got)
	}
	if got.Built != "abc123" || got.BuiltBase != "base456" || !got.Unfinished {
		t.Fatalf("the session's work was dropped by the transition: %+v", got)
	}
	if got.Note != "out of room with the migration half applied" {
		t.Fatalf("note = %q", got.Note)
	}

	// It is claimable again, by the same runner, with no operator in between.
	if _, _, err := qu.Claim(ctx, got); err != nil {
		t.Fatalf("a handed-off item could not be claimed for its fresh session: %v", err)
	}

	// And it is not on the exception queue's index, which is what the control
	// room lists as needing a person.
	for rec, err := range qu.Store.Log().Since(ctx, tn, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == "wi-hop" {
			t.Fatal("a handed-off item was filed as an exception; it needs no person")
		}
	}
}

// TestSettlingDropsTheUnfinishedMark: every settle drops the resume point, and
// the mark that says what KIND of resume point it was has to go with it. A
// stale "unfinished" outliving its commit would have a later attempt adopt a
// tree that is not there and, worse, run the agent over a change already
// judged.
func TestSettlingDropsTheUnfinishedMark(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-hop-settled")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.KeepUnfinished("abc123", "base456")
	if _, err := qu.Park(ctx, w, lease, SeverityMedium, "gave up"); err != nil {
		t.Fatal(err)
	}
	got, err := qu.Load(ctx, tn, "wi-hop-settled")
	if err != nil {
		t.Fatal(err)
	}
	if got.Unfinished || got.Built != "" {
		t.Fatalf("a settled item kept its unfinished resume point: %+v", got)
	}
}

// gateRecord appends a "gate" record to w's chain the way the loop does: a
// forge.Verdict, marshalled with its own default field names, subject to the
// head sha it judged.
func gateRecord(t *testing.T, qu *Queue, w WorkItem, sha string, v forge.Verdict) {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qu.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: w.Tenant, Scope: scope(w.ID), Kind: "gate", Subject: sha,
		Actor: "runner-1", Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestARebuildIsWokenKnowingWhatRefusedTheLastAttempt. The reviewer's first
// refusal on the dogfood named the line and the fix; the rebuild that
// followed ran on the same task, blind, and made the same mistake. A retry
// that drops the commit now carries the attempt it abandons — the commit,
// the gate's words, the operator's — as the prior the builder resolves into
// a layer at wake time, so the rebuilt run's agentref says what it was told.
func TestARebuildIsWokenKnowingWhatRefusedTheLastAttempt(t *testing.T) {
	qu, ctx := q(t), context.Background()
	w := submit(t, qu, "wi-1")
	w, lease, err := qu.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	// Two attempts: the first attempt's commit, then the current one — with
	// a free recheck on the current commit that failed again, to prove a
	// same-sha second refusal dedupes to its latest rather than doubling up.
	gateRecord(t, qu, w, "sha-attempt-1", forge.Verdict{
		Gate: "review", Decision: forge.DecisionFail,
		Reason: "the regex cannot match\nthe list-item form",
	})
	gateRecord(t, qu, w, "sha-built", forge.Verdict{
		Gate: "review", Decision: forge.DecisionFail,
		Reason: "still cannot match\nthe nested list case",
	})
	gateRecord(t, qu, w, "sha-built", forge.Verdict{
		Gate: "review", Decision: forge.DecisionFail,
		Reason: "recheck: still cannot match\nthe nested list case",
	})
	w.KeepCandidate("sha-built", "sha-base", []string{})
	if _, err := qu.ParkKeeping(ctx, w, lease, SeverityMedium, "review: fail (the regex cannot match the list-item form)"); err != nil {
		t.Fatal(err)
	}

	got, err := qu.Retry(ctx, tn, "wi-1", "chris", "rebuild: admit the list-item form")
	if err != nil {
		t.Fatal(err)
	}
	if got.Prior == nil {
		t.Fatal("a rebuild carried nothing of the attempt it abandoned")
	}
	if got.Prior.Run != "wi-1" || got.Prior.Sha != "sha-built" {
		t.Fatalf("prior = %+v, want the abandoned attempt's run and commit", *got.Prior)
	}
	if got.Prior.RetriedBy != "chris" || got.Prior.RetryNote != "rebuild: admit the list-item form" {
		t.Fatalf("the operator's note must be held on its own attributed fields: %+v", *got.Prior)
	}
	if got.Prior.Attempt != 0 || got.Prior.Of != 0 {
		t.Fatalf("a reopen is a fresh claim with a fresh budget — it should wake cold, not mid-count: %+v", *got.Prior)
	}
	if len(got.Prior.Verdicts) != 2 {
		t.Fatalf("want one entry per refused attempt (a same-sha recheck should dedupe), got %+v", got.Prior.Verdicts)
	}
	first, second := got.Prior.Verdicts[0], got.Prior.Verdicts[1]
	if first.Attempt != 1 || first.Reason != "the regex cannot match\nthe list-item form" {
		t.Fatalf("first verdict = %+v, want attempt 1 with its reason intact", first)
	}
	if second.Attempt != 2 || second.Reason != "recheck: still cannot match\nthe nested list case" {
		t.Fatalf("second verdict = %+v, want attempt 2 deduped to its LATEST refusal", second)
	}
	if _, ok := got.Prior.Layer(); !ok {
		t.Fatal("the carried attempt does not resolve into a layer, so the builder would be woken knowing nothing")
	}
	// And the record of the retry carries it: what the next run is told is
	// on the chain before that run exists.
	var recorded WorkItem
	for rec, err := range qu.Store.Log().Since(ctx, tn, scope("wi-1"), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "retried" {
			if err := json.Unmarshal(rec.Body, &recorded); err != nil {
				t.Fatal(err)
			}
		}
	}
	if recorded.Prior == nil || recorded.Prior.Sha != "sha-built" {
		t.Fatalf("the retried record does not carry the prior: %+v", recorded.Prior)
	}
	// The builder reads the item back from the store when it wakes, so
	// what the retry set has to be what Load folds.
	loaded, err := qu.Load(ctx, tn, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Prior == nil || loaded.Prior.Sha != "sha-built" {
		t.Fatalf("Load after the retry does not carry the prior: %+v", loaded.Prior)
	}
}

// TestARetryThatKeepsTheCommitCarriesNoPrior. Resuming at the gate runs no
// builder, and a prior that nobody reads would still change nothing — but
// it would be a claim on the record that a run was told something.
func TestARetryThatKeepsTheCommitCarriesNoPrior(t *testing.T) {
	qu, ctx := q(t), context.Background()
	parkKeeping(t, qu, "wi-1", "gate refused")
	got, err := qu.Retry(ctx, tn, "wi-1", "chris", "the gate was flaky; try again")
	if err != nil {
		t.Fatal(err)
	}
	if got.Prior != nil {
		t.Fatalf("a resumed retry carried a prior: %+v", *got.Prior)
	}
}
