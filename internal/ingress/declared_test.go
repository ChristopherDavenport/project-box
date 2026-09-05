package ingress

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/kind"
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

// governed is a declaration pinned the way a capability-bearing one has to be.
// The seeded incident grants nothing — see [kind.Seeds] for why shipping a
// preempting default would be the worst available choice — so a test of the
// authority path has to declare it, which is exactly the act an operator
// performs.
func governed(name string, caps kind.Capabilities) kind.Declaration {
	return kind.Declaration{
		Name: name, Capabilities: caps,
		Decompose:  kind.Decomposition{Decomposer: kind.DecomposeIdentity},
		Completion: kind.Completion{Graph: true},
		Source: agent.Source{Kind: agent.SourceGit, Repo: "https://example.test/policy.git",
			Commit: "5f2c8e7d1a3b4c6d8e0f1a2b3c4d5e6f70819293", Path: "kinds/" + name + ".yaml",
			Digest: "tree-" + name},
	}
}

func incidentDecl() kind.Declaration {
	d := governed(kind.SeedIncident, kind.Capabilities{Preempt: true, BreakGlass: true})
	d.Completion = kind.Completion{Facts: []string{kind.FactRestored, kind.FactPostmortem}}
	return d
}

func campaignDecl() kind.Declaration {
	return governed(kind.SeedCampaign, kind.Capabilities{})
}

func incident(open bool) Ingress {
	i := Ingress{ID: "ing-inc", Kind: kind.SeedIncident, Tenant: tn2, Box: "shop",
		Requester: "chris", Opened: time.Now()}
	if !open {
		i.Closed = time.Now()
	}
	return i
}

func TestBreakGlassRequiresAnOpenCarrier(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	// Refused where the declaration does not carry it, and where it does but
	// the origination is closed.
	camp := Ingress{ID: "ing-c", Kind: kind.SeedCampaign, Tenant: tn2, Box: "shop", Requester: "chris", Opened: time.Now()}
	err := BreakGlass(ctx, camp, campaignDecl(), s.Log(), "chris", "bypass gate")
	if err == nil {
		t.Fatal("a campaign invoked break-glass")
	}
	if !errors.Is(err, kind.ErrCapability) {
		t.Fatalf("refusal is not a capability refusal: %v", err)
	}
	if err := BreakGlass(ctx, incident(false), incidentDecl(), s.Log(), "chris", "bypass gate"); err == nil {
		t.Fatal("a closed incident invoked break-glass")
	}
	// Allowed under an open one whose declaration grants it, and the
	// invocation is evidence.
	if err := BreakGlass(ctx, incident(true), incidentDecl(), s.Log(), "chris", "bypass gate for hotfix"); err != nil {
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

// TestAuthorityFollowsTheDeclarationNotTheName is the collapse's own property.
// The name "incident" grants nothing; the DECLARATION does. A box whose
// incident declaration was never given break-glass has an incident that cannot
// break glass, and a box that grants the authority to a kind it invented has
// one that can.
func TestAuthorityFollowsTheDeclarationNotTheName(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	toothless := governed(kind.SeedIncident, kind.Capabilities{})
	if err := BreakGlass(ctx, incident(true), toothless, s.Log(), "chris", "bypass"); err == nil {
		t.Fatal("an incident declaration granting nothing still broke glass — the name is doing the work")
	}
	// An operator's own kind, granted the authority, holds it.
	escalation := Ingress{ID: "ing-esc", Kind: "customer-escalation", Tenant: tn2, Box: "shop",
		Requester: "chris", Opened: time.Now()}
	decl := governed("customer-escalation", kind.Capabilities{BreakGlass: true})
	if err := BreakGlass(ctx, escalation, decl, s.Log(), "chris", "bypass"); err != nil {
		t.Fatalf("a granted kind was refused: %v", err)
	}
}

// TestOnlyAGovernedDeclarationMayGrantAuthority is the first of the two
// conditions that make data-driven capabilities safe.
func TestOnlyAGovernedDeclarationMayGrantAuthority(t *testing.T) {
	inline := kind.Declaration{
		Name: "rogue", Capabilities: kind.Capabilities{BreakGlass: true, Preempt: true},
		Completion: kind.Completion{Graph: true},
		Source:     agent.Source{Kind: agent.SourceInline, Digest: "written-at-request-time"},
	}
	err := inline.Validate(kind.Capabilities{BreakGlass: true, Preempt: true})
	if err == nil {
		t.Fatal("an ungoverned declaration granted itself break-glass")
	}
	if !errors.Is(err, kind.ErrCapability) {
		t.Fatalf("wrong refusal: %v", err)
	}
	// The same declaration, pinned, is accepted.
	if err := governed("rogue", kind.Capabilities{BreakGlass: true}).Validate(kind.Capabilities{BreakGlass: true}); err != nil {
		t.Fatalf("a pinned declaration was refused: %v", err)
	}
}

// TestAKindCannotExceedItsBoxsCeiling is the second condition. Delegation
// intersects and never unions, so a maximal declaration is inert rather than
// dangerous — and it is refused at load rather than silently narrowed, because
// the only way to discover which side won would be to have an emergency.
func TestAKindCannotExceedItsBoxsCeiling(t *testing.T) {
	decl := governed(kind.SeedIncident, kind.Capabilities{BreakGlass: true, Preempt: true})
	err := decl.Validate(kind.Capabilities{BreakGlass: true})
	if err == nil {
		t.Fatal("a kind claimed preempt under a box that does not hold it")
	}
	if !errors.Is(err, kind.ErrCapability) {
		t.Fatalf("wrong refusal: %v", err)
	}
	if got := decl.Capabilities.Intersect(kind.Capabilities{BreakGlass: true}); got.Preempt {
		t.Fatal("intersection unioned")
	}
}

func TestPreemptFollowsTheDeclaration(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	worker := s.Leaser("campaign-runner")
	held, err := worker.Acquire(ctx, forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/main", Run: "camp-run", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	// A campaign's declaration does not carry preempt, however it asks.
	camp := Ingress{ID: "ing-c", Kind: kind.SeedCampaign, Tenant: tn2, Box: "shop", Requester: "chris"}
	if _, err := Preempt(ctx, camp, campaignDecl(), s.Leaser("inc-runner"), s.Log(), "chris",
		forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/main", Run: "camp-2", TTL: time.Hour}); err == nil {
		t.Fatal("a campaign preempted a lease")
	}
	// An open incident whose declaration carries it can — and the displaced
	// campaign runner is fenced.
	taken, err := Preempt(ctx, incident(true), incidentDecl(), s.Leaser("inc-runner"), s.Log(), "chris",
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
	// A closed origination authorizes nothing.
	if _, err := Preempt(ctx, incident(false), incidentDecl(), s.Leaser("inc-runner"), s.Log(), "chris",
		forge.LeaseRequest{Tenant: tn2, Repo: "app", Ref: "refs/heads/other", Run: "late", TTL: time.Hour}); err == nil {
		t.Fatal("a closed incident preempted")
	}
}

// TestCompletionIsDeclaredNotCompiled holds the sentence that used to live in
// a doc comment: restoration ends the outage, the postmortem ends the
// incident.
func TestCompletionIsDeclaredNotCompiled(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	decl := incidentDecl()
	i := incident(true)
	if ok, why := i.CanClose(decl, true); ok {
		t.Fatal("closed with nothing asserted")
	} else if why != "not asserted: restored, postmortem" {
		t.Fatalf("why: %q", why)
	}
	if err := openOnChain(ctx, s, i); err != nil {
		t.Fatal(err)
	}
	if err := Assert(ctx, s.Log(), i, "chris", kind.FactRestored, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// Facts are recorded acts, so they come back off the chain rather than out
	// of a struct somebody mutated.
	got, _, err := Load(ctx, s.Log(), tn2, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := got.CanClose(decl, true); ok || why != "not asserted: postmortem" {
		t.Fatalf("restoration alone closed it: %v %q", ok, why)
	}
	if err := Assert(ctx, s.Log(), i, "chris", kind.FactPostmortem, "evidence-record-id"); err != nil {
		t.Fatal(err)
	}
	got, _, err = Load(ctx, s.Log(), tn2, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := got.CanClose(decl, true); !ok {
		t.Fatalf("restored + postmortem should close: %q", why)
	}
	// A declaration whose completion is its graph closes on the graph alone.
	issue := Ingress{ID: "ing-i", Kind: kind.SeedIssue, Tenant: tn2, Box: "shop"}
	issueDecl := governed(kind.SeedIssue, kind.Capabilities{})
	if ok, _ := issue.CanClose(issueDecl, true); !ok {
		t.Fatal("an issue whose work is done could not close")
	}
	if ok, why := issue.CanClose(issueDecl, false); ok {
		t.Fatalf("an issue closed with work outstanding: %q", why)
	}
}

// openOnChain writes the origination itself, which is what the router does
// and what a fact needs underneath it: an assertion about something the chain
// has never seen opened is a fact with no subject.
func openOnChain(ctx context.Context, s *sqlite.Store, i Ingress) error {
	body, err := json.Marshal(i)
	if err != nil {
		return err
	}
	_, err = s.Log().Append(ctx, evidence.Record{
		Tenant: i.Tenant, Scope: Scope(i.ID), Kind: RecordOpen,
		Subject: "delivery/" + i.ID, Actor: i.Requester, Body: body,
	})
	return err
}
