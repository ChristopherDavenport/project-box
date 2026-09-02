package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

type fakeResolver struct {
	calls int
	err   error
	// digest is appended to each ref, standing in for the tree lookup.
	digest string
}

func (f *fakeResolver) ResolveSkills(_ context.Context, refs []agent.SkillRef) ([]agent.SkillRef, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]agent.SkillRef, 0, len(refs))
	for _, r := range refs {
		r.Source.Digest = r.Name + "-" + f.digest
		out = append(out, r)
	}
	return out, nil
}

func pin(name string) agent.SkillRef {
	return agent.SkillRef{Name: name, Source: agent.Source{
		Kind: agent.SourceGit, Repo: "git@example:mkt.git", Commit: "c0ffee", Path: "skills/" + name,
	}}
}

func resolve(t *testing.T, s *Source) agent.Bundle {
	t.Helper()
	c := &agent.Composer{Source: s}
	b, err := c.Resolve(context.Background(), agent.Request{
		Tenant: "acme", Role: agent.RoleBuilder, Box: "pb", Task: "do the work",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return b
}

// TestSurfaceIsResolvedPerRun. Per-run resolution is what makes the factory
// live: a set captured once at daemon start drifts from the machine with
// nothing to say so, and editing a pin would need a restart to take.
func TestSurfaceIsResolvedPerRun(t *testing.T) {
	r := &fakeResolver{digest: "v1"}
	s := &Source{Box: "pb", Skills: []agent.SkillRef{pin("go")}, Resolver: r}

	first := resolve(t, s)
	if r.calls != 1 {
		t.Fatalf("skills resolved %d times on one run", r.calls)
	}
	if len(first.Skills) != 1 || first.Skills[0].Source.Digest != "go-v1" {
		t.Fatalf("resolved skill did not reach the bundle: %+v", first.Skills)
	}

	// The pinned tree changes underneath — a pin edited in box.yaml, or a
	// skill repo rewritten. The next run must see it and must not share a ref.
	r.digest = "v2"
	second := resolve(t, s)
	if r.calls != 2 {
		t.Error("the second run reused the first run's resolution")
	}
	if first.Ref == second.Ref {
		t.Error("different skill content produced the same agentref — two agents recorded as one")
	}
}

// TestPluginSetIsRecordedEvenWhenInherited is the whole point of discovery.
// Inheriting the machine's plugins is a legitimate choice; not knowing which
// ones were inherited is not, because the plugin set is authority — hooks on
// every tool call, subagents with their own sessions.
func TestPluginSetIsRecordedEvenWhenInherited(t *testing.T) {
	dir := configDir(t, `{"go@mkt": true}`, map[string]string{
		"go@mkt": userInstall("2026.830.1", "611d9835", "go"),
	})
	inherited := resolve(t, &Source{Box: "pb", Inherit: true, ConfigDir: dir})
	if len(inherited.Plugins) != 1 || inherited.Plugins[0].ID() != "go@mkt" {
		t.Fatalf("inherited plugins did not reach the bundle: %v", inherited.Plugins)
	}
	if inherited.Plugins[0].Commit != "611d9835" {
		t.Error("the recorded plugin carries no provenance")
	}

	notInherited := resolve(t, &Source{Box: "pb", Inherit: false, ConfigDir: dir})
	if len(notInherited.Plugins) != 0 {
		t.Errorf("plugins arrived without being inherited: %v", notInherited.Plugins)
	}
	if inherited.Ref == notInherited.Ref {
		t.Error("a bundle with the machine's plugins and one without share an agentref")
	}
}

// TestPluginChurnChangesTheRef. Enabling a plugin between runs changes what
// the agent can do, so it has to change what the record calls it.
func TestPluginChurnChangesTheRef(t *testing.T) {
	one := configDir(t, `{"go@mkt": true}`, map[string]string{
		"go@mkt": userInstall("1", "sha", "go"),
	})
	two := configDir(t, `{"go@mkt": true, "slack@mkt": true}`, map[string]string{
		"go@mkt":    userInstall("1", "sha", "go"),
		"slack@mkt": userInstall("1", "sha", "slack"),
	})
	upgraded := configDir(t, `{"go@mkt": true}`, map[string]string{
		"go@mkt": userInstall("2", "sha2", "go"),
	})

	a := resolve(t, &Source{Box: "pb", Inherit: true, ConfigDir: one}).Ref
	b := resolve(t, &Source{Box: "pb", Inherit: true, ConfigDir: two}).Ref
	c := resolve(t, &Source{Box: "pb", Inherit: true, ConfigDir: upgraded}).Ref
	if a == b {
		t.Error("adding a plugin did not change the agentref")
	}
	if a == c {
		t.Error("upgrading a plugin did not change the agentref")
	}
	if resolve(t, &Source{Box: "pb", Inherit: true, ConfigDir: one}).Ref != a {
		t.Error("the same machine state resolved to two different refs — resolution is not deterministic")
	}
}

// TestGrantingNothingIsNotALayer. An empty harness layer would still carry a
// digest into the ref, making "this box grants nothing" and "this box was
// never asked" two different agents on the record for no reason.
func TestGrantingNothingIsNotALayer(t *testing.T) {
	base := agent.Layer{
		Name: "builder", Scope: agent.ScopeRole,
		Source: agent.Source{Kind: agent.SourceInline, Digest: "role"}, Instructions: "build",
	}
	bare := resolve(t, &Source{Base: []agent.Layer{base}, Box: "pb"})
	for _, l := range bare.Layers {
		if l.Name == "harness" {
			t.Fatal("an empty harness layer was composed anyway")
		}
	}
}

// TestUnresolvableSkillFailsTheRun. Carrying on without a skill the bundle
// promised is invisible from outside: an agent that was never given a skill
// and one that chose not to use it produce identical evidence.
func TestUnresolvableSkillFailsTheRun(t *testing.T) {
	boom := errors.New("commit is not in the mirror")
	s := &Source{Box: "pb", Skills: []agent.SkillRef{pin("go")}, Resolver: &fakeResolver{err: boom}}
	if _, err := (&agent.Composer{Source: s}).Resolve(context.Background(), agent.Request{
		Tenant: "acme", Role: agent.RoleBuilder, Box: "pb", Task: "work",
	}); !errors.Is(err, boom) {
		t.Errorf("an unresolvable skill did not stop the run: %v", err)
	}

	s = &Source{Box: "pb", Skills: []agent.SkillRef{pin("go")}}
	if _, err := (&agent.Composer{Source: s}).Resolve(context.Background(), agent.Request{
		Tenant: "acme", Role: agent.RoleBuilder, Box: "pb", Task: "work",
	}); err == nil {
		t.Error("skills were pinned with no resolver and the run proceeded")
	}
}
