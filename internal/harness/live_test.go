package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// TestLiveHarnessResolves resolves a real box.yaml against this machine's
// real harness configuration and the real remotes its skills are pinned to,
// then materializes them — everything an actual run does except spending a
// model session.
//
// Gated on BOX_HARNESS_LIVE=<path-to-box.yaml>, because it clones from the
// network and reads ~/.claude. The unit suite proves the logic against
// fixtures; this proves the pins in a config an operator actually ships
// resolve to trees that exist, which is the failure a fixture cannot catch:
// a commit that was force-pushed away, a path that moved between releases,
// a private remote the daemon's credential channel cannot reach.
//
//	go test -run TestLiveHarness ./internal/harness/ \
//	  -args -v   # BOX_HARNESS_LIVE=hack/dogfood/box.yaml
func TestLiveHarnessResolves(t *testing.T) {
	path := os.Getenv("BOX_HARNESS_LIVE")
	if path == "" {
		t.Skip("set BOX_HARNESS_LIVE=/path/to/box.yaml to resolve a real config against the network")
	}
	cfg, err := boxcfg.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mirror, err := gitforge.NewMirror(t.TempDir(), tenant.ID(cfg.Tenant), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, bx := range cfg.Boxes {
		skills := make([]agent.SkillRef, 0, len(bx.Harness.Skills))
		for _, s := range bx.Harness.Skills {
			skills = append(skills, agent.SkillRef{Name: s.Name, Source: agent.Source{
				Kind: agent.SourceGit, Repo: s.Repo, Commit: s.Commit, Path: s.Path,
			}})
		}
		src := &Source{
			Box: bx.Name, Inherit: bx.Harness.Plugins == boxcfg.PluginsInherit,
			PluginMCP: bx.Harness.PluginMCP, Skills: skills, Resolver: mirror,
		}
		layers, err := src.Layers(ctx, agent.Request{Box: bx.Name, Role: agent.RoleBuilder})
		if err != nil {
			t.Fatalf("box %s: %v", bx.Name, err)
		}
		if len(layers) == 0 {
			t.Logf("box %s grants nothing", bx.Name)
			continue
		}
		l := layers[len(layers)-1]
		t.Logf("box %s: %d plugins, %d skills, %d mcp", bx.Name, len(l.Plugins), len(l.Skills), len(l.MCP))
		for _, p := range l.Plugins {
			if p.MCP != bx.Harness.PluginMCP {
				t.Errorf("plugin %s reports mcp=%v against a config of %v", p.ID(), p.MCP, bx.Harness.PluginMCP)
			}
		}

		// Materialize, because a digest that resolves and a tree that
		// extracts are different claims.
		dir := filepath.Join(t.TempDir(), bx.Name+".skills")
		t.Cleanup(func() { gitforge.RemoveContext(dir) })
		wrote, err := mirror.MaterializeSkills(ctx, dir, l.Skills)
		if err != nil {
			t.Fatalf("box %s: materialize: %v", bx.Name, err)
		}
		if !wrote {
			continue
		}
		for _, s := range l.Skills {
			if _, err := os.Stat(filepath.Join(dir, "skills", s.Name, "SKILL.md")); err != nil {
				t.Errorf("skill %s pinned at %s has no SKILL.md at %s — the agent would load an empty directory",
					s.Name, s.Source.Commit, s.Source.Path)
			}
		}
	}
}
