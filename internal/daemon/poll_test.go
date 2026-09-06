package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// countingLister is an [ingress.Lister] a test can inject directly into
// d.poller, per the design point that due-checking and polling live in a
// method a test can call without going through Run's select.
type countingLister struct {
	calls  int
	issues []ingress.Issue
}

func (c *countingLister) List(ctx context.Context, target, label string) ([]ingress.Issue, error) {
	c.calls++
	return c.issues, nil
}

// pollTestDaemon builds a Daemon with just enough wired to exercise
// PollRoutes directly — no box.yaml, no mirror, no real forge — since
// duePolls and Poll never touch either.
func pollTestDaemon(t *testing.T, poll time.Duration) (*Daemon, *countingLister) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	tn := tenant.ID("acme")
	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "boxd"}
	lister := &countingLister{}
	d := &Daemon{
		store: s, tenant: tn, router: router,
		poller:   &ingress.Poller{Lister: lister, Router: router, Log: s.Log(), Tenant: tn, Actor: "boxd"},
		lastPoll: map[string]time.Time{},
		routes: map[string]ingress.Route{
			"issues": {Name: "issues", Kind: ingress.RouteIssue, Box: "shop", Repo: "app",
				Requester: "boxd", Poll: poll, Label: "box"},
		},
		remotes: map[string]string{"issues": "https://github.com/acme/app.git"},
	}
	return d, lister
}

// TestPollRoutesRespectsTheRoutesOwnInterval: the daemon's tick and a
// route's own Poll interval are different clocks — PollRoutes must not
// re-read the forge on every tick, only once the route's interval elapses.
func TestPollRoutesRespectsTheRoutesOwnInterval(t *testing.T) {
	d, lister := pollTestDaemon(t, 5*time.Minute)
	ctx := context.Background()
	now := time.Now()

	d.PollRoutes(ctx, now)
	if lister.calls != 1 {
		t.Fatalf("calls after first poll = %d, want 1", lister.calls)
	}
	d.PollRoutes(ctx, now.Add(time.Minute))
	if lister.calls != 1 {
		t.Fatalf("calls after an early tick = %d, want still 1 (interval not elapsed)", lister.calls)
	}
	d.PollRoutes(ctx, now.Add(6*time.Minute))
	if lister.calls != 2 {
		t.Fatalf("calls after the interval elapsed = %d, want 2", lister.calls)
	}
}

// TestPollRoutesSkipsPollingOff: a route with Poll zero — the default,
// today's whole behavior — is never read by PollRoutes.
func TestPollRoutesSkipsPollingOff(t *testing.T) {
	d, lister := pollTestDaemon(t, 0)
	d.PollRoutes(context.Background(), time.Now())
	if lister.calls != 0 {
		t.Fatalf("a route with polling off was polled %d times", lister.calls)
	}
}

// TestPollRoutesOpensAnIngressForALabeledIssue exercises the seam end to
// end: a due route, a fake forge, and the ingress landing on the chain the
// same way a hand-run box submit's does.
func TestPollRoutesOpensAnIngressForALabeledIssue(t *testing.T) {
	d, lister := pollTestDaemon(t, time.Minute)
	lister.issues = []ingress.Issue{
		{Number: 5, Title: "found by the poll", Body: "body text", Labels: []string{"box"}},
	}
	ctx := context.Background()
	d.PollRoutes(ctx, time.Now())

	found := false
	for rec, err := range d.store.Log().Since(ctx, d.tenant, "index/ingress", 1) {
		if err != nil {
			t.Fatal(err)
		}
		var ing ingress.Ingress
		if err := json.Unmarshal(rec.Body, &ing); err != nil {
			continue
		}
		if ing.Key == "issue-5" {
			found = true
		}
	}
	if !found {
		t.Fatal("PollRoutes did not open an ingress for the labeled issue")
	}
}
