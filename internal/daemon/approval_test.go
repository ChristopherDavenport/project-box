package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/approval"
	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// writeApprovalCfg writes a box.yaml with one repo. count > 0 declares
// approval.count on a gated, pull_request-landing repo — the shape
// boxcfg.validate requires an approval count to have; count == 0 says
// nothing about approval at all, today's behavior.
func writeApprovalCfg(t *testing.T, path, db, root, remote string, count int) {
	t.Helper()
	extra := ""
	if count > 0 {
		extra = fmt.Sprintf("\n        land: pull_request\n        land_token: gh\n        lane: gated\n        approval: {count: %d}", count)
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    agent: ['true']
    repos:
      - name: app
        remote: %s
        check: 'true'%s
`, db, root, remote, extra)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAssembleWiresApprovalForACountedRepo is issue #66's daemon wiring: a
// repo naming approval.count gets a Loop whose Approval seam is filled with
// a gate named "human-approval", and a repo that says nothing about
// approval gets nil — today's behavior, unchanged, because a count of zero
// must not turn into a gate that always passes.
func TestAssembleWiresApprovalForACountedRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("approval.count declared", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeApprovalCfg(t, cfgPath, db, root, bare(t, "app"), 1)

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		if lp.Approval == nil {
			t.Fatal("approval.count declared, but Loop.Approval is nil")
		}
		g := lp.Approval(queue.WorkItem{Tenant: tenantID})
		if g.Name() != "human-approval" {
			t.Fatalf("gate name = %q, want human-approval", g.Name())
		}
	})

	t.Run("unconfigured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeApprovalCfg(t, cfgPath, db, root, bare(t, "app"), 0)

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		if lp.Approval != nil {
			t.Fatal("approval unconfigured, but Loop.Approval is non-nil — a count of zero must change nothing")
		}
	})
}

// tenantID is the tenant [writeApprovalCfg] declares, named so the gate
// tests below can build a matching ChangeSet without re-typing the string.
const tenantID = "acme"

// TestHumanApprovalGateEvaluate exercises the daemon's forge.Gate against a
// fake ensure/reviews pair — no HTTP, no PRLander — covering the decisions
// the gate itself is responsible for: ensuring the request exists, and
// applying [approval.Distinct]'s head-binding and self-approval rules to
// whatever the reviews supplier reports.
func TestHumanApprovalGateEvaluate(t *testing.T) {
	ctx := context.Background()
	head := forge.Commit("head-sha")
	prior := forge.Commit("prior-sha")
	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tenantID, Run: "run-1", Intent: "test",
		Members: []forge.Member{{Repo: "app", Branch: "main", Head: head}},
	}

	newGate := func(requester authz.Actor, reviews []gitforge.Review) *humanApprovalGate {
		return &humanApprovalGate{
			requirement: approval.Requirement{Count: 1},
			requester:   requester,
			ensure: func(context.Context, forge.ChangeSet, forge.Member) (string, error) {
				return "https://github.com/acme/app/pull/1", nil
			},
			reviews: func(context.Context, forge.ChangeSet, forge.Member) ([]gitforge.Review, error) {
				return reviews, nil
			},
		}
	}
	directRequester := authz.Actor{Principal: authz.Principal{Subject: "alice", Kind: authz.KindUser, Tenant: tenantID}}
	delegatedRequester := authz.Actor{
		Principal:  authz.Principal{Subject: "boxd", Kind: authz.KindAgent, Tenant: tenantID},
		OnBehalfOf: &authz.Principal{Subject: "alice", Kind: authz.KindUser, Tenant: tenantID},
	}

	t.Run("no reviews goes pending", func(t *testing.T) {
		g := newGate(directRequester, nil)
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPending {
			t.Fatalf("decision = %s, want pending: %+v", v.Decision, v)
		}
	})

	t.Run("one approved review of the exact head passes", func(t *testing.T) {
		g := newGate(directRequester, []gitforge.Review{
			{Login: "bob", CommitID: string(head), State: "APPROVED"},
		})
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPass {
			t.Fatalf("decision = %s, want pass: %+v", v.Decision, v)
		}
	})

	t.Run("a review of the prior head does not pass", func(t *testing.T) {
		g := newGate(directRequester, []gitforge.Review{
			{Login: "bob", CommitID: string(prior), State: "APPROVED"},
		})
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPending {
			t.Fatalf("decision = %s, want pending — stale approvals do not count: %+v", v.Decision, v)
		}
	})

	t.Run("a review whose login is the item's requester does not pass", func(t *testing.T) {
		g := newGate(directRequester, []gitforge.Review{
			{Login: "alice", CommitID: string(head), State: "APPROVED"},
		})
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPending {
			t.Fatalf("decision = %s, want pending — self-approval does not count: %+v", v.Decision, v)
		}
	})

	t.Run("a review from an agent acting for that requester does not pass", func(t *testing.T) {
		g := newGate(delegatedRequester, []gitforge.Review{
			{Login: "alice", CommitID: string(head), State: "APPROVED"},
		})
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPending {
			t.Fatalf("decision = %s, want pending — the delegator's own approval does not count: %+v", v.Decision, v)
		}
	})

	t.Run("a non-approved review does not pass", func(t *testing.T) {
		g := newGate(directRequester, []gitforge.Review{
			{Login: "bob", CommitID: string(head), State: "DISMISSED"},
		})
		v, err := g.Evaluate(ctx, cs)
		if err != nil {
			t.Fatal(err)
		}
		if v.Decision != forge.DecisionPending {
			t.Fatalf("decision = %s, want pending — a dismissed review is not an approval: %+v", v.Decision, v)
		}
	})
}

// TestHumanApprovalGateEnsuresBeforeListing: the gated lane reaches gates
// before it ever opens a pull request (see gitforge.PRLander.Land), so a
// gate that only read reviews would go pending forever on a request that
// would never exist. Evaluate must ensure the request first, every call.
func TestHumanApprovalGateEnsuresBeforeListing(t *testing.T) {
	ctx := context.Background()
	head := forge.Commit("head-sha")
	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tenantID, Run: "run-1", Intent: "test",
		Members: []forge.Member{{Repo: "app", Branch: "main", Head: head}},
	}
	ensureCalls := 0
	g := &humanApprovalGate{
		requirement: approval.Requirement{Count: 1},
		requester:   authz.Actor{Principal: authz.Principal{Subject: "boxd", Kind: authz.KindAgent, Tenant: tenantID}},
		ensure: func(context.Context, forge.ChangeSet, forge.Member) (string, error) {
			ensureCalls++
			return "https://github.com/acme/app/pull/1", nil
		},
		reviews: func(context.Context, forge.ChangeSet, forge.Member) ([]gitforge.Review, error) {
			return nil, nil
		},
	}
	for i := 0; i < 3; i++ {
		if _, err := g.Evaluate(ctx, cs); err != nil {
			t.Fatalf("evaluate %d: %v", i, err)
		}
	}
	if ensureCalls != 3 {
		t.Fatalf("ensure calls = %d, want 3 — every Evaluate must ensure the request exists", ensureCalls)
	}
}
