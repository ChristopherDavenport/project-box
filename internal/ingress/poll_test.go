package ingress

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// fakeLister is a [Lister] a test drives directly: it returns exactly the
// issues and error it is given, regardless of the label a caller passes —
// which is the point of TestPollFiltersOnTheLabelsReturned, below.
type fakeLister struct {
	issues     []Issue
	err        error
	lastTarget string
	lastLabel  string
}

func (f *fakeLister) List(ctx context.Context, target, label string) ([]Issue, error) {
	f.lastTarget, f.lastLabel = target, label
	if f.err != nil {
		return nil, f.err
	}
	return f.issues, nil
}

func issueRoute(requester string) Route {
	return Route{Tenant: tn2, Name: "issues", Kind: RouteIssue, Opens: kind.SeedIssue,
		Box: "shop", Repo: "app", Requester: requester, Label: "box"}
}

// TestPollOpensOneIngressPerLabeledIssueAndRedeliversIdempotently is the
// property issue #63 asks for: a poll is a Router.Open under the hood, so a
// second poll of the same open issue is redelivery, not a duplicate.
func TestPollOpensOneIngressPerLabeledIssueAndRedeliversIdempotently(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	router := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	lister := &fakeLister{issues: []Issue{
		{Number: 42, Title: "the box cannot see its own PRs", Body: "  body text  \n", Labels: []string{"box"}},
	}}
	p := &Poller{Lister: lister, Router: router, Log: s.Log(), Tenant: tn2, Actor: "runner"}
	route := issueRoute("chris")
	route.Contract = "read CLAUDE.md first"
	now := time.Now().UTC()

	if err := p.Poll(ctx, route, "https://github.com/acme/app", now); err != nil {
		t.Fatal(err)
	}
	ing, found, err := Load(ctx, s.Log(), tn2, "ing-"+hashKey("issues", "issue-42"))
	if err != nil || !found {
		t.Fatalf("first poll did not open an ingress: found=%v err=%v", found, err)
	}
	if ing.Key != "issue-42" {
		t.Fatalf("key = %q, want issue-42", ing.Key)
	}
	wantNote := "GitHub issue #42: the box cannot see its own PRs\n\nbody text\n\n---\n\nread CLAUDE.md first"
	if ing.Note != wantNote {
		t.Fatalf("note = %q, want %q", ing.Note, wantNote)
	}

	// A second poll of the same still-open, still-labeled issue redelivers:
	// same ingress, no error, and the router's own idempotency answered it —
	// this is not a second dedup layer on top.
	if err := p.Poll(ctx, route, "https://github.com/acme/app", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, found, err := Load(ctx, s.Log(), tn2, "ing-"+hashKey("issues", "issue-42"))
	if err != nil || !found {
		t.Fatal("second poll lost the ingress")
	}
	if again.Opened != ing.Opened {
		t.Fatalf("second poll re-opened the ingress: %v -> %v", ing.Opened, again.Opened)
	}
}

// TestPollDropsAnIssueMissingTheRoutesLabel holds the "unlabeled issues do
// not open work" rule even against a Lister that ignores the label
// parameter entirely — the filter has to be the Poller's, not merely
// something a well-behaved Lister happens to do server-side.
func TestPollDropsAnIssueMissingTheRoutesLabel(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	router := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	lister := &fakeLister{issues: []Issue{
		{Number: 7, Title: "unrelated", Body: "no label here", Labels: []string{"wontfix"}},
	}}
	p := &Poller{Lister: lister, Router: router, Log: s.Log(), Tenant: tn2, Actor: "runner"}

	if err := p.Poll(ctx, issueRoute("chris"), "https://github.com/acme/app", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load(ctx, s.Log(), tn2, "ing-"+hashKey("issues", "issue-7")); err != nil || found {
		t.Fatalf("an unlabeled issue opened work: found=%v err=%v", found, err)
	}
}

// TestPollFailureIsRecordedAndOpensNothing is the "unknown never reads as
// zero" rule for a poll: an unreachable forge must be a recorded failure,
// not silence that looks like an empty issue list.
func TestPollFailureIsRecordedAndOpensNothing(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	router := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	lister := &fakeLister{err: errors.New("github: 503")}
	p := &Poller{Lister: lister, Router: router, Log: s.Log(), Tenant: tn2, Actor: "runner"}
	route := issueRoute("chris")

	err := p.Poll(ctx, route, "https://github.com/acme/app", time.Now())
	if err == nil {
		t.Fatal("a listing error was swallowed")
	}
	if !strings.Contains(err.Error(), "github: 503") {
		t.Fatalf("error lost the cause: %v", err)
	}
	found := false
	for rec, err := range s.Log().Since(ctx, tn2, pollScope(route.Name), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == RecordPollFailed {
			found = true
		}
	}
	if !found {
		t.Fatal("the listing failure left no evidence")
	}
}

// TestPollWithNoRequesterOpensNothing: StoreRouter.Open already refuses a
// route with no requester, so a poll of one must surface that refusal
// rather than swallow it — and open nothing.
func TestPollWithNoRequesterOpensNothing(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	router := &StoreRouter{Store: s, Tenant: tn2, Runner: "runner"}
	lister := &fakeLister{issues: []Issue{
		{Number: 9, Title: "no principal behind this", Body: "", Labels: []string{"box"}},
	}}
	p := &Poller{Lister: lister, Router: router, Log: s.Log(), Tenant: tn2, Actor: "runner"}
	route := issueRoute("") // no requester

	if err := p.Poll(ctx, route, "https://github.com/acme/app", time.Now()); err == nil {
		t.Fatal("a route with no requester opened work")
	}
	if _, found, err := Load(ctx, s.Log(), tn2, "ing-"+hashKey("issues", "issue-9")); err != nil || found {
		t.Fatalf("a route with no requester opened an ingress: found=%v err=%v", found, err)
	}
}
