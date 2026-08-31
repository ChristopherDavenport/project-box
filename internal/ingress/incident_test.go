package ingress

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn2 = tenant.ID("acme")

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func incident(open bool) Ingress {
	i := Ingress{ID: "ing-inc", Kind: KindIncident, Tenant: tn2, Box: "shop",
		Requester: "chris", Opened: time.Now(), Incident: &Incident{Severity: 1}}
	if !open {
		i.Closed = time.Now()
	}
	return i
}

func TestBreakGlassRequiresAnOpenIncident(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	// Refused with no open incident: a campaign, and a closed incident.
	camp := Ingress{ID: "ing-c", Kind: KindCampaign, Tenant: tn2, Box: "shop", Requester: "chris", Opened: time.Now()}
	if err := BreakGlass(ctx, camp, s.Log(), "chris", "bypass gate"); err == nil {
		t.Fatal("a campaign invoked break-glass")
	}
	if err := BreakGlass(ctx, incident(false), s.Log(), "chris", "bypass gate"); err == nil {
		t.Fatal("a closed incident invoked break-glass")
	}
	// Allowed under an open incident, and the invocation is evidence.
	if err := BreakGlass(ctx, incident(true), s.Log(), "chris", "bypass gate for hotfix"); err != nil {
		t.Fatal(err)
	}
	found := false
	for r, err := range s.Log().BySubject(ctx, tn2, "ing-inc") {
		if err == nil && r.Kind == "break-glass" {
			found = true
		}
	}
	if !found {
		t.Fatal("break-glass left no evidence")
	}
}

func TestIncidentPreemptsAndCampaignCannot(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	worker := s.Leaser("campaign-runner")
	held, err := worker.Acquire(ctx, forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/main", Run: "camp-run", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	// A campaign ingress cannot preempt, however it asks.
	camp := Ingress{ID: "ing-c", Kind: KindCampaign, Tenant: tn2, Box: "shop", Requester: "chris"}
	if _, err := Preempt(ctx, camp, s.Leaser("inc-runner"), s.Log(), "chris",
		forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/main", Run: "camp-2", TTL: time.Hour}); err == nil {
		t.Fatal("a campaign preempted a lease")
	}
	// An open incident can — and the displaced campaign runner is fenced.
	taken, err := Preempt(ctx, incident(true), s.Leaser("inc-runner"), s.Log(), "chris",
		forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/main", Run: "inc-run", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if taken.Token <= held.Token {
		t.Fatal("preemption did not supersede")
	}
	if _, err := worker.Renew(ctx, held, time.Hour); !errors.Is(err, forge.ErrStaleToken) {
		t.Fatalf("displaced runner not fenced: %v", err)
	}
	// A closed incident authorizes nothing.
	if _, err := Preempt(ctx, incident(false), s.Leaser("inc-runner"), s.Log(), "chris",
		forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/other", Run: "late", TTL: time.Hour}); err == nil {
		t.Fatal("a closed incident preempted")
	}
}

func TestPostmortemBeforeClose(t *testing.T) {
	i := incident(true)
	if ok, why := i.CanClose(); ok {
		t.Fatal("closed with nothing restored")
	} else if why != "service not restored" {
		t.Fatalf("why: %q", why)
	}
	i.Incident.Restored = time.Now()
	if ok, why := i.CanClose(); ok || why != "postmortem not landed" {
		t.Fatalf("restoration alone closed it: %v %q", ok, why)
	}
	i.Incident.Postmortem = "evidence-record-id"
	if ok, _ := i.CanClose(); !ok {
		t.Fatal("restored + postmortem should close")
	}
	// Non-incidents close freely.
	if ok, _ := (Ingress{Kind: KindIssue}).CanClose(); !ok {
		t.Fatal("an issue could not close")
	}
}
