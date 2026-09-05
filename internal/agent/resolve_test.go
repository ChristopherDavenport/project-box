package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

func layers() StaticLayers {
	return StaticLayers{
		{
			Name: "org", Scope: ScopeBase,
			Source:       Source{Kind: SourceGit, Digest: "d-base", Repo: "policies", Commit: "abc", Path: "base.md"},
			Instructions: "Follow the org conventions.",
			Tools:        ToolPolicy{Deny: []string{"DeleteProd"}},
			Skills:       []SkillRef{{Name: "go-style", Source: Source{Kind: SourceGit, Digest: "d-skill-1"}}},
		},
		{
			Name: "builder", Scope: ScopeRole,
			Source:       Source{Kind: SourceGit, Digest: "d-role", Repo: "policies", Commit: "abc", Path: "builder.md"},
			Instructions: "Build the change and keep the tests green.",
			Tools:        ToolPolicy{Allow: []string{"Bash", "Edit", "Read"}},
			Context:      ContextSet{Knowledge: []string{"global"}},
		},
		{
			Name: "shop", Scope: ScopeBox,
			Source: Source{Kind: SourceGit, Digest: "d-box", Repo: "policies", Commit: "abc", Path: "shop.md"},
			Tools:  ToolPolicy{Allow: []string{"Bash", "Edit"}}, // narrows: no Read
			Context: ContextSet{
				Repos:     []ContextRepo{{Repo: "contracts", Commit: "c1"}},
				Knowledge: []string{"box/shop"},
			},
			// The box also pins a newer skill under the same name: more
			// specific replaces broader.
			Skills: []SkillRef{{Name: "go-style", Source: Source{Kind: SourceGit, Digest: "d-skill-2"}}},
		},
	}
}

func request() Request {
	return Request{Tenant: tenant.ID("acme"), Role: RoleBuilder, Box: "shop", Repo: "app", Run: "run-1", Task: "Fix the flaky test."}
}

func TestResolveIsDeterministic(t *testing.T) {
	c := &Composer{Source: layers()}
	a, err := c.Resolve(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Resolve(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if a.Ref == "" || a.Ref != b.Ref {
		t.Fatalf("same request, same material, different refs: %q vs %q", a.Ref, b.Ref)
	}
	// Any behavioral change moves the ref.
	altered := layers()
	altered[1].Instructions += " Also rewrite history." // a changed role layer
	c2 := &Composer{Source: altered}
	d, err := c2.Resolve(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if d.Ref == a.Ref {
		t.Fatal("changed instructions did not change the agentref")
	}
}

func TestMergeRules(t *testing.T) {
	c := &Composer{Source: layers()}
	b, err := c.Resolve(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	// Denies union and survive narrowing; allows intersect.
	if got := b.Tools.Deny; len(got) != 1 || got[0] != "DeleteProd" {
		t.Fatalf("deny: %v", got)
	}
	if got := b.Tools.Allow; len(got) != 2 || got[0] != "Bash" || got[1] != "Edit" {
		t.Fatalf("allow should be the intersection [Bash Edit], got %v", got)
	}
	// More specific skill pin replaced the broader one.
	if len(b.Skills) != 1 || b.Skills[0].Source.Digest != "d-skill-2" {
		t.Fatalf("skills: %+v", b.Skills)
	}
	// Prose concatenates in layer order, task last.
	if !strings.HasSuffix(strings.TrimSpace(b.Instructions), "Fix the flaky test.") {
		t.Fatalf("task layer not last:\n%s", b.Instructions)
	}
	// Knowledge unions in order.
	if len(b.Context.Knowledge) != 2 || b.Context.Knowledge[0] != "global" {
		t.Fatalf("knowledge: %v", b.Context.Knowledge)
	}
}

func TestEmptyPolicyResolvesRestrictive(t *testing.T) {
	// No layer states a permission mode: the role default applies, and it is
	// never the permissive end of the scale.
	c := &Composer{Source: StaticLayers{{Name: "org", Scope: ScopeBase, Instructions: "x", Source: Source{Kind: SourceInline, Digest: "d"}}}}
	for role, want := range map[Role]string{RoleBuilder: "acceptEdits", RoleGate: "plan", Role("mystery"): "plan"} {
		b, err := c.Resolve(context.Background(), Request{Tenant: "acme", Role: role, Box: "shop"})
		if err != nil {
			t.Fatal(err)
		}
		if b.Tools.PermissionMode != want {
			t.Fatalf("role %s: mode %q, want %q", role, b.Tools.PermissionMode, want)
		}
		if b.Tools.PermissionMode == "bypassPermissions" {
			t.Fatal("an omitted policy resolved to the most permissive mode")
		}
	}
	// A stated mode is narrowed, never widened: base says acceptEdits, task
	// layer says bypassPermissions — the restrictive one wins.
	c2 := &Composer{Source: StaticLayers{
		{Name: "org", Scope: ScopeBase, Source: Source{Kind: SourceInline, Digest: "d"}, Tools: ToolPolicy{PermissionMode: "acceptEdits"}},
	}}
	b, err := c2.Resolve(context.Background(), Request{
		Tenant: "acme", Role: RoleBuilder, Box: "shop",
		Extra: []Layer{{Name: "sneaky", Scope: ScopeTask, Source: Source{Kind: SourceInline, Digest: "d2"}, Tools: ToolPolicy{PermissionMode: "bypassPermissions"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Tools.PermissionMode != "acceptEdits" {
		t.Fatalf("a task layer widened the permission mode to %q", b.Tools.PermissionMode)
	}
}

func TestExplainDiffs(t *testing.T) {
	c := &Composer{Source: layers()}
	b1, _ := c.Resolve(context.Background(), request())
	e1a, err := c.Explain(context.Background(), b1)
	if err != nil {
		t.Fatal(err)
	}
	e1b, _ := c.Explain(context.Background(), b1)
	if !bytes.Equal(e1a, e1b) {
		t.Fatal("Explain is not deterministic")
	}
	altered := layers()
	altered[2].Context.Repos = []ContextRepo{{Repo: "contracts", Commit: "c2"}}
	b2, _ := (&Composer{Source: altered}).Resolve(context.Background(), request())
	e2, _ := c.Explain(context.Background(), b2)
	if bytes.Equal(e1a, e2) {
		t.Fatal("a changed context pin rendered identically")
	}
	if !bytes.Contains(e2, []byte("contracts @ c2")) {
		t.Fatalf("render does not show the new pin:\n%s", e2)
	}
}

func TestUngovernedSurfaces(t *testing.T) {
	c := &Composer{Source: layers()}
	b, _ := c.Resolve(context.Background(), request())
	un := b.Ungoverned()
	// Only the inline task layer lacks change control here.
	if len(un) != 1 || un[0].Scope != ScopeTask {
		t.Fatalf("ungoverned: %+v", un)
	}
}

// TestSessionOptionsMergePerKey: session options are the model's rule applied
// per key — the most specific non-empty value wins, keys nobody sets stay
// absent so the agent's default stays in force AND on the record — and a
// session value is part of the agentref, because an agent at a different
// effort is a different agent.
func TestSessionOptionsMergePerKey(t *testing.T) {
	ls := layers()
	ls[0].Session = map[string]string{"effort": "high", "fast_mode": "false"}
	ls[2].Session = map[string]string{"effort": "low", "agent": ""}
	b, err := (&Composer{Source: ls}).Resolve(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if b.Session["effort"] != "low" || b.Session["fast_mode"] != "false" {
		t.Fatalf("session merge: %v", b.Session)
	}
	if _, set := b.Session["agent"]; set {
		t.Fatalf("an empty value was carried as a setting: %v", b.Session)
	}
	plain, _ := (&Composer{Source: layers()}).Resolve(context.Background(), request())
	if plain.Ref == b.Ref {
		t.Fatal("a session option did not change the agentref")
	}
	e, _ := (&Composer{Source: ls}).Explain(context.Background(), b)
	if !bytes.Contains(e, []byte("session.effort: low")) {
		t.Fatalf("Explain does not show the session option:\n%s", e)
	}
}
