package authz

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func res(box string) Resource {
	return Resource{Tenant: tn, Box: box, Repo: "svc", RepoRole: "service"}
}

func actor(subject string) Actor {
	return Actor{Principal: Principal{Subject: subject, Kind: KindAgent, Tenant: tn}}
}

func onBehalf(agent, user string) Actor {
	a := actor(agent)
	p := Principal{Subject: user, Kind: KindUser, Tenant: tn}
	a.OnBehalfOf = &p
	return a
}

func eval(grants ...Grant) *Evaluator {
	src := Static{}
	for _, g := range grants {
		src[g.Subject] = append(src[g.Subject], g)
	}
	return &Evaluator{Grants: src}
}

func TestNoImplicitAllow(t *testing.T) {
	e := eval() // nobody holds anything
	d, err := e.Can(context.Background(), actor("builder-1"), ActionPropose, res("shop"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Allow {
		t.Fatal("an empty grant set must be an inert principal, not an unconstrained one")
	}
	if d.Reason == "" {
		t.Fatal("a denial must say why")
	}
}

func TestGrantCoversAndIsNamed(t *testing.T) {
	e := eval(Grant{ID: "g1", Subject: "builder-1", Tenant: string(tn), Actions: []Action{ActionPropose}})
	d, err := e.Can(context.Background(), actor("builder-1"), ActionPropose, res("shop"))
	if err != nil || !d.Allow {
		t.Fatalf("want allow, got %+v, %v", d, err)
	}
	if d.Grant != "g1" {
		t.Fatalf("decision must name the permitting grant, got %q", d.Grant)
	}
	// Same subject, different action: still nothing.
	if d, _ := e.Can(context.Background(), actor("builder-1"), ActionLand, res("shop")); d.Allow {
		t.Fatal("a propose grant must not cover land — that absence IS segregation of duties")
	}
}

func TestDelegationIntersectsNeverUnions(t *testing.T) {
	ctx := context.Background()
	e := eval(
		Grant{ID: "agent-wide", Subject: "agent-1", Tenant: string(tn), Actions: []Action{ActionPropose, ActionDeploy}},
		Grant{ID: "user-narrow", Subject: "chris", Tenant: string(tn), Actions: []Action{ActionPropose}},
	)
	// Agent for user, action both hold: allowed.
	if d, _ := e.Can(ctx, onBehalf("agent-1", "chris"), ActionPropose, res("shop")); !d.Allow {
		t.Fatalf("intersection should allow propose: %+v", d)
	}
	// Agent holds deploy, user does not: the agent acting FOR the user is
	// denied what the user could not do — delegation cannot escalate.
	if d, _ := e.Can(ctx, onBehalf("agent-1", "chris"), ActionDeploy, res("shop")); d.Allow {
		t.Fatal("delegation escalated: agent lent its own authority to a requester who lacks it")
	}
	// And the reverse: a user's grant lends nothing to an agent without its own.
	e2 := eval(Grant{ID: "user-deploy", Subject: "chris", Tenant: string(tn), Actions: []Action{ActionDeploy}})
	if d, _ := e2.Can(ctx, onBehalf("agent-1", "chris"), ActionDeploy, res("shop")); d.Allow {
		t.Fatal("delegation escalated: requester's authority flowed to an agent holding no grant")
	}
}

func TestTenantIsNeverAWildcard(t *testing.T) {
	ctx := context.Background()
	// A grant with an empty tenant covers nothing — unscoped is a bug, not "all".
	e := eval(Grant{ID: "broken", Subject: "builder-1", Tenant: "", Actions: []Action{ActionPropose}})
	if d, _ := e.Can(ctx, actor("builder-1"), ActionPropose, res("shop")); d.Allow {
		t.Fatal("an unscoped grant must cover nothing")
	}
	// A grant in one tenant says nothing in another.
	e2 := eval(Grant{ID: "other", Subject: "builder-1", Tenant: "globex", Actions: []Action{ActionPropose}})
	if d, _ := e2.Can(ctx, actor("builder-1"), ActionPropose, res("shop")); d.Allow {
		t.Fatal("a grant crossed a tenant boundary")
	}
}

func TestNarrowingFieldsMatch(t *testing.T) {
	ctx := context.Background()
	e := eval(Grant{ID: "shop-only", Subject: "builder-1", Tenant: string(tn), Box: "shop", Actions: []Action{ActionPropose}})
	if d, _ := e.Can(ctx, actor("builder-1"), ActionPropose, res("shop")); !d.Allow {
		t.Fatalf("box-scoped grant should cover its box: %+v", d)
	}
	if d, _ := e.Can(ctx, actor("builder-1"), ActionPropose, res("warehouse")); d.Allow {
		t.Fatal("box-scoped grant leaked into another box")
	}
	// Empty grant fields widen; empty is on the grant side only.
	e2 := eval(Grant{ID: "tenant-wide", Subject: "builder-1", Tenant: string(tn), Actions: []Action{ActionPropose}})
	if d, _ := e2.Can(ctx, actor("builder-1"), ActionPropose, res("warehouse")); !d.Allow {
		t.Fatalf("tenant-wide grant should cover every box: %+v", d)
	}
}
