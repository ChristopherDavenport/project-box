package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/checks"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
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
}

func (s *prStub) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	s.cs = cs
	landing, err := s.inner.Land(ctx, cs)
	if err == nil && s.prURL != "" {
		for i := range landing.Landed {
			landing.Landed[i].PRURL = s.prURL
		}
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
