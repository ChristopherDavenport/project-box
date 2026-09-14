package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/checks"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// prStub is the loop's PRLander seam filled with a stub standing in for
// git.PRLander: it delegates every git operation to the real inner
// git.Lander — the same fencing, the same fenced push, the same
// compensation — and only stamps a PR URL onto what landed, the way a real
// GitHub round trip would without making one. It also remembers the last
// ChangeSet it was handed, which is how these tests read DrivingIssue back:
// the loop builds that ChangeSet itself, and a wrapped Lander is the seam
// that sees it.
type prStub struct {
	inner *git.Lander
	prURL string
	cs    forge.ChangeSet
	// flakes, when set, is stamped onto the first landed member the way
	// git.PRLander stamps [forge.Placement.Flakes] after observing an
	// independent CI red-then-green for the same commit.
	flakes []forge.FlakedTest
}

func (s *prStub) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	s.cs = cs
	landing, err := s.inner.Land(ctx, cs)
	if err == nil && s.prURL != "" {
		for i := range landing.Landed {
			landing.Landed[i].PRURL = s.prURL
		}
	}
	if err == nil && len(s.flakes) > 0 && len(landing.Landed) > 0 {
		landing.Landed[0].Flakes = s.flakes
	}
	return landing, err
}

// wirePRStub sets e.loop.PRLander to wrap the real inner git.Lander with a
// prStub, the same way a Box would wire git.NewPRLander in production.
func wirePRStub(e *env, prURL string) *prStub {
	stub := &prStub{prURL: prURL}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander {
		stub.inner = inner
		return stub
	}
	return stub
}

// TestDrivingIssueFromOriginKey is issue #22's ChangeSet.DrivingIssue wiring:
// an issue-route ingress's delivery key trims to the issue number, and any
// other shape — including no key at all — leaves it empty rather than
// guessing at a mapping that is not this exact convention.
func TestDrivingIssueFromOriginKey(t *testing.T) {
	t.Run("issue-originated", func(t *testing.T) {
		e := setup(t, []string{"true"})
		ctx := context.Background()
		stub := wirePRStub(e, "")

		ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-77"})
		if err != nil {
			t.Fatal(err)
		}
		q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
		item, _ := q.Submit(ctx, ItemFor(ing, "wi-di-issue", "close issue 77"))
		item, err = e.loop.Run(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		if item.State != queue.StateDone {
			t.Fatalf("did not land: %+v", item)
		}
		if item.OriginKey != "issue-77" {
			t.Fatalf("OriginKey = %q, want issue-77", item.OriginKey)
		}
		if stub.cs.DrivingIssue != "77" {
			t.Fatalf("DrivingIssue = %q, want 77", stub.cs.DrivingIssue)
		}
	})

	t.Run("non-issue-shaped key", func(t *testing.T) {
		e := setup(t, []string{"true"})
		ctx := context.Background()
		stub := wirePRStub(e, "")

		// Shaped like a repair's delivery key (loop.OpenRepair), not an
		// issue's — the other real convention this must not be confused
		// with.
		ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "red-deadbeef"})
		if err != nil {
			t.Fatal(err)
		}
		q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
		item, _ := q.Submit(ctx, ItemFor(ing, "wi-di-other", "not an issue"))
		item, err = e.loop.Run(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		if item.State != queue.StateDone {
			t.Fatalf("did not land: %+v", item)
		}
		if stub.cs.DrivingIssue != "" {
			t.Fatalf("DrivingIssue = %q, want empty for a non-issue-shaped key", stub.cs.DrivingIssue)
		}
	})

	t.Run("no key at all", func(t *testing.T) {
		e := setup(t, []string{"true"})
		ctx := context.Background()
		stub := wirePRStub(e, "")

		// Built directly rather than through the router: the router refuses
		// an empty idempotency key, but an ingress opened before Key was
		// carried still has to resolve to an empty DrivingIssue rather than
		// an error.
		ing := ingress.Ingress{ID: "ing-manual", Tenant: tn, Box: "shop", Requester: "chris"}
		q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
		item, _ := q.Submit(ctx, ItemFor(ing, "wi-di-nokey", "no origination key"))
		item, err := e.loop.Run(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		if item.State != queue.StateDone {
			t.Fatalf("did not land: %+v", item)
		}
		if stub.cs.DrivingIssue != "" {
			t.Fatalf("DrivingIssue = %q, want empty with no origination key", stub.cs.DrivingIssue)
		}
	})
}

// TestGatedLaneLandsSameCommitWithPRLander confirms wiring in a PRLander
// changes nothing about which commit reaches the branch or how it is
// fenced: the wrapped Lander still does the real push, and the item settles
// exactly as TestIssueToLandedCommitUnattended's unwrapped run does.
func TestGatedLaneLandsSameCommitWithPRLander(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()
	stub := wirePRStub(e, "https://github.com/acme/app/pull/9")

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-90"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-gated-pr", "close issue 90"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone || item.Head == "" {
		t.Fatalf("item did not finish: %+v", item)
	}
	got := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if got != item.Head {
		t.Fatalf("remote main=%s want %s", got, item.Head)
	}
	if stub.cs.DrivingIssue != "90" {
		t.Fatalf("DrivingIssue = %q, want 90", stub.cs.DrivingIssue)
	}
}

// TestGatedLaneCarriesVerdictsToLand is issue #58: the gated lane's resolved
// gate decision has to reach Land on the ChangeSet, or a Lander that
// publishes it somewhere a reader can see (git.PRLander's GitHub check) has
// nothing to publish. The record id on the verdict has to be real — a
// record the evidence log actually holds — not merely non-empty, since a
// fabricated id would send an auditor chasing a chain entry that isn't
// there.
func TestGatedLaneCarriesVerdictsToLand(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := wirePRStub(e, "https://github.com/acme/app/pull/11")

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-92"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-gated-verdicts", "close issue 92"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("did not land: %+v", item)
	}

	if len(stub.cs.Verdicts) == 0 {
		t.Fatal("ChangeSet handed to Land carried no verdicts")
	}
	v := stub.cs.Verdicts[0]
	if v.Decision != forge.DecisionPass {
		t.Fatalf("verdict decision = %q, want pass", v.Decision)
	}
	recID := ""
	for _, e := range v.Evidence {
		if e.Kind == forge.EvidenceKindRecord {
			recID = e.URI
		}
	}
	if recID == "" {
		t.Fatal("verdict carried no evidence-record reference")
	}

	found := false
	for r, err := range e.store.Log().BySubject(ctx, tn, item.Head) {
		if err != nil {
			t.Fatalf("bysubject: %v", err)
		}
		if r.ID == recID && r.Kind == "gate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verdict evidence id %q does not name a real gate record on subject %s", recID, item.Head)
	}
}

// TestChangeSetCarriesWorkItemSummary is issue #59's plumbing half: a
// WorkItem's Summary (issue #51) has to reach Members[0].Summary on the
// ChangeSet the loop builds, since that is the only thing PRLander (see
// internal/forge/git/pr.go) can title a PR from without truncating the
// task's whole paragraph. Title truncation itself is exercised in
// internal/forge/git/pr_test.go, not here.
func TestChangeSetCarriesWorkItemSummary(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := wirePRStub(e, "")

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-93"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	wi := ItemFor(ing, "wi-summary", "close issue 93, a much longer task paragraph than the summary")
	wi.Summary = "close issue 93"
	item, _ := q.Submit(ctx, wi)
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("did not land: %+v", item)
	}
	if len(stub.cs.Members) == 0 || stub.cs.Members[0].Summary != "close issue 93" {
		t.Fatalf("ChangeSet Members[0].Summary = %q, want %q", stub.cs.Members[0].Summary, "close issue 93")
	}
}

// TestOptimisticLaneLandsSameCommitWithPRLander is the same guarantee for
// the land-then-check lane: a green run lands and settles exactly as it does
// unwrapped, PRLander configured or not.
func TestOptimisticLaneLandsSameCommitWithPRLander(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	optimistic(e)
	stub := wirePRStub(e, "https://github.com/acme/app/pull/10")

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-91"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-opt-pr", "close issue 91"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone || item.Head == "" {
		t.Fatalf("item did not finish: %+v", item)
	}
	got := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if got != item.Head {
		t.Fatalf("remote main=%s want %s", got, item.Head)
	}
	green, err := checks.IsGreen(ctx, e.store, tn, "app", item.Head)
	if err != nil || !green {
		t.Fatalf("a green landing should read green: %v %v", green, err)
	}
	if string(stub.cs.Members[0].Head) != item.Head {
		t.Fatalf("the wrapped lander saw head %s, item landed %s", stub.cs.Members[0].Head, item.Head)
	}
}

// TestRepairReasonCarriesThePRURL is the other half of issue #22: a
// confirmed red on a PR-landed commit hands its repair a reason that names
// the PR it repairs, folded into the existing free-text reason rather than
// a new field on agent.Prior or checks.Status.
func TestRepairReasonCarriesThePRURL(t *testing.T) {
	e := setup(t, []string{"false"})
	ctx := context.Background()
	_, priors := optimistic(e)
	stub := wirePRStub(e, "https://github.com/acme/app/pull/11")

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-red-pr"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-red-pr", "break the build"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone || !strings.Contains(item.Note, "repair opened") {
		t.Fatalf("confirmed red should finish with repair on record: %+v", item)
	}
	if len(*priors) != 1 {
		t.Fatalf("repair summoned %d times, want once", len(*priors))
	}
	if !strings.Contains((*priors)[0].Reason, stub.prURL) {
		t.Fatalf("repair reason should carry the PR it repairs: %q", (*priors)[0].Reason)
	}
}

// prRefuseOnce is a PRLander whose first landing the remote refuses — the
// shape of a 403 on the check run, or a scope the token lacks — and whose
// second goes through the real inner Lander untouched.
type prRefuseOnce struct {
	inner   *git.Lander
	refused int
}

func (s *prRefuseOnce) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if s.refused == 0 {
		s.refused++
		return forge.Landing{}, errors.New("github: POST /repos/o/r/check-runs: 403 Forbidden: You must authenticate via a GitHub App.")
	}
	return s.inner.Land(ctx, cs)
}

// TestALandingTheRemoteRefusedKeepsTheCommitForTheRetry. The first landing
// through the reviewer onto main was refused at the push twice — a token
// scope, then a check run only an App may post — and each retry built,
// checked and reviewed the same change again, because the land step parked
// without its resume point. A landing the remote refused says nothing about
// the change: the park keeps the commit and its verdicts, and the retry
// resumes at the landing with no builder and no gate run.
func TestALandingTheRemoteRefusedKeepsTheCommitForTheRetry(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	builder := &countingBuilder{}
	e.loop.Builder = builder
	gateRuns := 0
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{failNTimesGate{name: "build-and-test", n: &gateRuns, failFor: 0}}
	}
	stub := &prRefuseOnce{}
	e.loop.PRLander = func(inner *git.Lander) forge.Lander {
		stub.inner = inner
		return stub
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-84"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-land-refused", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked || !strings.HasPrefix(item.Note, "land: ") {
		t.Fatalf("a refused landing should park at the land step, got %s (%s)", item.State, item.Note)
	}
	if item.Built == "" {
		t.Fatal("a land-step park dropped the commit the remote refused")
	}
	if builder.n != 1 || gateRuns != 1 {
		t.Fatalf("before the retry: %d builds, %d gate runs", builder.n, gateRuns)
	}

	retried, err := q.Retry(ctx, tn, "wi-land-refused", queue.Identity{Name: "chris"}, "the token has the scope now; land it")
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
}

// flakeRoute is the ingress route flake remediation opens against — a
// standing test fixture the way issueRoute is, not a new declared kind.
func flakeRoute() ingress.Route {
	return ingress.Route{Name: "flaky-tests", Kind: ingress.RouteRedMain, Opens: kind.SeedIssue, Box: "shop", Requester: "gargoyle"}
}

// TestFlakedLandingOpensRemediation is issue #275's loop half: a gated
// landing that comes back carrying a proven flake ([forge.Placement.Flakes],
// populated only by a Lander like PRLander that talked to an independent
// CI) must turn into remediation work — a side effect of a landing that
// actually settled, not a record nobody reads.
func TestFlakedLandingOpensRemediation(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := wirePRStub(e, "")
	f := forge.FlakedTest{
		Test: "TestWidget", Package: "internal/widget", Sha: "flake-sha-1",
		RedRunID: 10, GreenRunID: 11, URL: "https://ci.example/run/11",
	}
	stub.flakes = []forge.FlakedTest{f}
	e.loop.Flaky = OpenFlake(e.router, flakeRoute(), 0)

	route := issueRoute()
	route.Repo = "app"
	ing, _, err := e.router.Open(ctx, route, ingress.Delivery{Key: "issue-flake-1"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-flake-1", "close issue flake-1"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a flaked landing should still settle done: %+v", item)
	}

	var remediations []string
	for r, err := range e.store.Log().BySubject(ctx, tn, f.Sha) {
		if err != nil {
			t.Fatalf("bysubject: %v", err)
		}
		if r.Kind == "flake-remediation" {
			remediations = append(remediations, string(r.Body))
		}
	}
	if len(remediations) != 1 {
		t.Fatalf("want exactly one flake-remediation record, got %d: %v", len(remediations), remediations)
	}
	body := remediations[0]
	if !strings.Contains(body, f.Test) || !strings.Contains(body, f.Package) {
		t.Fatalf("remediation record does not name the flaked test: %s", body)
	}
	_, rest, found := strings.Cut(body, `"ingress":"`)
	if !found {
		t.Fatalf("remediation record does not name the opened ingress: %s", body)
	}
	ingID, _, _ := strings.Cut(rest, `"`)

	head, err := e.store.Log().Head(ctx, tn, "ingress/"+ingID)
	if err != nil {
		t.Fatal(err)
	}
	var opened ingress.Ingress
	if err := json.Unmarshal(head.Body, &opened); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app", f.Package, f.Test, f.Sha, "10", "11", f.URL, "do not skip, tag, or quarantine"} {
		if !strings.Contains(opened.Note, want) {
			t.Fatalf("opened ingress does not name %q: %s", want, opened.Note)
		}
	}
}

// TestNilFlakyOpensNothing: [Loop.Flaky] unset means a proven flake is
// recorded but nobody is summoned — the same nil-safe posture [Loop.Red]
// takes — and the landing still settles done.
func TestNilFlakyOpensNothing(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := wirePRStub(e, "")
	stub.flakes = []forge.FlakedTest{{
		Test: "TestWidget", Package: "internal/widget", Sha: "flake-sha-nil",
		RedRunID: 1, GreenRunID: 2, URL: "https://ci.example/run/2",
	}}
	// e.loop.Flaky is left nil, the default.

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-flake-nil"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-flake-nil", "close issue flake-nil"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a flaked landing should still settle done with Flaky unset: %+v", item)
	}
	for r, err := range e.store.Log().BySubject(ctx, tn, "flake-sha-nil") {
		if err != nil {
			t.Fatalf("bysubject: %v", err)
		}
		if strings.HasPrefix(r.Kind, "flake-remediation") {
			t.Fatalf("nil Flaky should summon nobody, found record: %+v", r)
		}
	}
}

// TestFlakyHookErrorLeavesLandingSettled: a hook error is recorded and never
// unsettles the landing — the same posture [Loop.settle] takes with a
// failing Handoff.
func TestFlakyHookErrorLeavesLandingSettled(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	stub := wirePRStub(e, "")
	f := forge.FlakedTest{
		Test: "TestWidget", Package: "internal/widget", Sha: "flake-sha-err",
		RedRunID: 3, GreenRunID: 4, URL: "https://ci.example/run/4",
	}
	stub.flakes = []forge.FlakedTest{f}
	e.loop.Flaky = func(ctx context.Context, item queue.WorkItem, got forge.FlakedTest) (string, error) {
		return "", errors.New("router is down")
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-flake-err"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-flake-err", "close issue flake-err"))
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a Flaky hook error must not unsettle a landed item: %+v", item)
	}

	found := false
	for r, err := range e.store.Log().BySubject(ctx, tn, f.Sha) {
		if err != nil {
			t.Fatalf("bysubject: %v", err)
		}
		if r.Kind == "flake-remediation-failed" {
			found = true
			if !strings.Contains(string(r.Body), "router is down") {
				t.Fatalf("failure record does not carry the error: %s", r.Body)
			}
		}
	}
	if !found {
		t.Fatal("a Flaky hook error should still be recorded")
	}
}

// TestSameFlakeObservedTwiceFoldsToOneIngress: the delivery key OpenFlake
// derives folds test, package AND sha together, so a second observation of
// the SAME flake at the SAME commit resolves to the one ingress already
// open for it — sha256(route+key) dedup is permanent, structural, not a
// second reading of anything.
func TestSameFlakeObservedTwiceFoldsToOneIngress(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	route := flakeRoute()
	flaky := OpenFlake(e.router, route, 0)
	f := forge.FlakedTest{
		Test: "TestWidget", Package: "internal/widget", Sha: "flake-sha-dedup",
		RedRunID: 5, GreenRunID: 6, URL: "https://ci.example/run/6",
	}

	id1, err := flaky(ctx, queue.WorkItem{ID: "wi-dedup-1", Repo: "app"}, f)
	if err != nil || id1 == "" {
		t.Fatalf("the first observation should open: %q %v", id1, err)
	}
	id2, err := flaky(ctx, queue.WorkItem{ID: "wi-dedup-2", Repo: "app"}, f)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id1 {
		t.Fatalf("the same flake at the same commit opened a second ingress: %q vs %q", id2, id1)
	}

	// The fold is structural, not a coincidence of equal ids: the router
	// itself reports the occurrence as not fresh.
	ing, fresh, err := e.router.Open(ctx, route, ingress.Delivery{
		Key: fmt.Sprintf("flake-%s/%s-%s", f.Package, f.Test, f.Sha),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Fatal("the second observation of the same flake should not be fresh")
	}
	if ing.ID != id1 {
		t.Fatalf("router.Open resolved a different ingress: %q, want %q", ing.ID, id1)
	}
}
