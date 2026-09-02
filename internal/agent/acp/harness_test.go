package acp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// delivered runs one turn against the fake agent and returns the options the
// runtime actually put on session/new, as the agent received them. Asserting
// on harnessOptions alone would test the constructor; the point is what
// crosses the wire.
func delivered(t *testing.T, rt *Runtime, b agent.Bundle) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt.Command = []string{os.Args[0]}
	rt.Env = append(rt.Env, "ACP_FAKE=harness")
	if rt.Dir == "" {
		rt.Dir = t.TempDir()
	}
	res, err := rt.Run(ctx, b, "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var meta struct {
		ClaudeCode struct {
			Options map[string]any `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal([]byte(res.Text), &meta); err != nil {
		t.Fatalf("session/new carried no readable _meta (%q): %v", res.Text, err)
	}
	if meta.ClaudeCode.Options == nil {
		t.Fatal("session/new carried no claudeCode.options — the bundle's surface reached the agent nowhere")
	}
	return meta.ClaudeCode.Options
}

// TestBundleSurfaceReachesTheAgent is the seam that was missing: Skills, MCP
// and Plugins were resolved, hashed into the agentref and delivered nowhere.
func TestBundleSurfaceReachesTheAgent(t *testing.T) {
	b := bundle()
	b.MCP = []agent.MCPRef{
		{Name: "telemetry", Command: []string{"node", "cli.js", "mcp"}},
		{Name: "remote", URL: "https://mcp.example/mcp"},
	}
	opts := delivered(t, &Runtime{SkillsDir: "/tmp/run.skills", LoadedSkills: "/tmp/run.skills"}, b)

	servers, _ := opts["mcpServers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("granted 2 mcp servers, agent was offered %d: %v", len(servers), servers)
	}
	stdio, _ := servers["telemetry"].(map[string]any)
	if stdio["type"] != "stdio" || stdio["command"] != "node" {
		t.Errorf("stdio server mis-shaped: %v", stdio)
	}
	if args, _ := stdio["args"].([]any); len(args) != 2 || args[0] != "cli.js" {
		t.Errorf("stdio args lost the tail: %v", stdio["args"])
	}
	if http, _ := servers["remote"].(map[string]any); http["type"] != "http" || http["url"] != "https://mcp.example/mcp" {
		t.Errorf("http server mis-shaped: %v", servers["remote"])
	}

	plugins, _ := opts["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("expected the generated skills plugin, got %v", plugins)
	}
	p, _ := plugins[0].(map[string]any)
	if p["path"] != "/tmp/run.skills" || p["type"] != "local" || p["skipMcpDiscovery"] != true {
		t.Errorf("skills plugin mis-shaped: %v", p)
	}
}

// TestAnEmptySkillsDirIsNotAPlugin. The directory exists for every run so a
// confining wrapper can mount it unconditionally, but handing it to the
// harness when nothing was written would fail the plugin load — a manifest
// that is not there — for the one case where granting nothing is correct.
func TestAnEmptySkillsDirIsNotAPlugin(t *testing.T) {
	opts := delivered(t, &Runtime{SkillsDir: "/tmp/run.skills"}, bundle())
	if _, ok := opts["plugins"]; ok {
		t.Errorf("an empty skills directory was offered as a plugin: %v", opts["plugins"])
	}
}

// TestPluginMCPIsNeverAmbient is the security property, asserted at the wire
// rather than assumed from a default. strictMcpConfig is what stops a
// plugin's own .mcp.json loading with the plugin — the difference between a
// builder inheriting a machine's knowledge and inheriting its credentials.
func TestPluginMCPIsNeverAmbient(t *testing.T) {
	for _, inherit := range []bool{true, false} {
		b := bundle()
		b.Plugins = []agent.PluginRef{{
			Name: "slack", Marketplace: "somewhere", Path: "/plugins/slack", MCP: true,
		}}
		opts := delivered(t, &Runtime{InheritSettings: inherit}, b)
		if opts["strictMcpConfig"] != true {
			t.Errorf("inherit=%v: strictMcpConfig is %v — plugin MCP would load with the plugin",
				inherit, opts["strictMcpConfig"])
		}
		if servers, _ := opts["mcpServers"].(map[string]any); len(servers) != 0 {
			t.Errorf("inherit=%v: bundle granted no servers but %d were offered", inherit, len(servers))
		}
	}
}

// TestInheritanceDecidesWhoOwnsThePluginSet. Inheriting means the harness's
// own settings load the plugins and the runtime must not pass them again;
// not inheriting means the bundle is the only authority and project scope
// still has to survive, because CLAUDE.md discovery needs it.
func TestInheritanceDecidesWhoOwnsThePluginSet(t *testing.T) {
	b := bundle()
	b.Plugins = []agent.PluginRef{{Name: "go", Marketplace: "mkt", Path: "/plugins/go"}}

	inherited := delivered(t, &Runtime{InheritSettings: true}, b)
	if sources := stringList(t, inherited["settingSources"]); len(sources) != 3 {
		t.Errorf("inheriting should keep all three setting sources, got %v", sources)
	}
	if _, ok := inherited["plugins"]; ok {
		t.Error("inheriting passed the plugin list as well — the same plugin would load twice, from two owners")
	}

	owned := delivered(t, &Runtime{InheritSettings: false}, b)
	sources := stringList(t, owned["settingSources"])
	if len(sources) != 1 || sources[0] != "project" {
		t.Errorf("locked down should keep project scope and only that, got %v", sources)
	}
	plugins, _ := owned["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("bundle's plugin was not delivered: %v", plugins)
	}
	if p, _ := plugins[0].(map[string]any); p["path"] != "/plugins/go" || p["skipMcpDiscovery"] != true {
		t.Errorf("plugin mis-shaped: %v", plugins[0])
	}
}

func stringList(t *testing.T, v any) []string {
	t.Helper()
	xs, _ := v.([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}
