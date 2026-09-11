package boxcfg

import (
	"strings"
	"testing"
)

// harnessCfg wraps a harness block in the smallest valid config around it.
func harnessCfg(t *testing.T, harness string) (Config, error) {
	t.Helper()
	return Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    seat: mason
`+harness+`
    repos:
      - name: api
        remote: git@example:api.git
`))
}

const goCommit = "611d9835ec22d3b5ceba07afafc4b3ddce719e1f"

// TestHarnessDefaultsToLiveInheritanceWithoutCredentials is the pair of
// defaults that matter: a factory nobody can extend without a restart is a
// factory nobody extends, and a builder that inherits a machine's plugins
// must not thereby inherit its tokens.
func TestHarnessDefaultsToLiveInheritanceWithoutCredentials(t *testing.T) {
	c, err := harnessCfg(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	h := c.Boxes[0].Harness
	if h.Plugins != PluginsInherit {
		t.Errorf("harness.plugins defaulted to %q, wanted %q", h.Plugins, PluginsInherit)
	}
	if h.PluginMCP {
		t.Error("plugin MCP defaulted on — an unattended builder would hold whatever the machine is signed into")
	}
	if len(h.MCP) != 0 {
		t.Error("MCP servers appeared without being granted")
	}
}

// TestSkillRefsMustIdentifyAnExactTree. The whole value of a ref is that the
// exact skill a builder ran is answerable afterwards; every way of being
// vague about it has to be refused at load, where the operator is watching,
// rather than at run time on a tick nobody is reading.
func TestSkillRefsMustIdentifyAnExactTree(t *testing.T) {
	for _, tc := range []struct{ name, skills, want string }{
		{"branch instead of a commit", `
      skills:
        - name: go
          repo: git@example:mkt.git
          commit: main
          path: backend/go/skills/go`, "full 40-character commit"},
		{"abbreviated sha", `
      skills:
        - name: go
          repo: git@example:mkt.git
          commit: 611d983
          path: backend/go/skills/go`, "full 40-character commit"},
		{"no repo", `
      skills:
        - name: go
          commit: ` + goCommit + `
          path: backend/go/skills/go`, "no repo"},
		{"no path", `
      skills:
        - name: go
          repo: git@example:mkt.git
          commit: ` + goCommit, "no path"},
		{"no name", `
      skills:
        - repo: git@example:mkt.git
          commit: ` + goCommit + `
          path: backend/go/skills/go`, "no name"},
		{"name escapes its directory", `
      skills:
        - name: ../../etc
          repo: git@example:mkt.git
          commit: ` + goCommit + `
          path: backend/go/skills/go`, "plain directory name"},
		{"declared twice", `
      skills:
        - name: go
          repo: git@example:mkt.git
          commit: ` + goCommit + `
          path: backend/go/skills/go
        - name: go
          repo: git@example:other.git
          commit: ` + goCommit + `
          path: other`, "declared twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harnessCfg(t, "    harness:"+tc.skills)
			if err == nil {
				t.Fatal("accepted a skill ref that cannot name an exact tree")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not say what is wrong (%q, wanted %q)", err, tc.want)
			}
		})
	}
}

// TestValidSkillRefLoads, and keeps every field: the pin is worthless if any
// part of it is dropped between the file and the resolver.
func TestValidSkillRefLoads(t *testing.T) {
	c, err := harnessCfg(t, `    harness:
      plugins: none
      plugin_mcp: true
      skills:
        - name: golang
          repo: https://github.com/example/mkt.git
          commit: `+goCommit+`
          path: backend/go/skills/go
      mcp:
        - name: telemetry
          command: [node, cli.js, mcp]
          tools: [telemetry_cost]`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	h := c.Boxes[0].Harness
	if h.Plugins != PluginsNone || !h.PluginMCP {
		t.Errorf("harness toggles lost: %+v", h)
	}
	if len(h.Skills) != 1 {
		t.Fatalf("skills lost: %v", h.Skills)
	}
	s := h.Skills[0]
	if s.Name != "golang" || s.Commit != goCommit || s.Path != "backend/go/skills/go" ||
		s.Repo != "https://github.com/example/mkt.git" {
		t.Errorf("skill ref lost a field: %+v", s)
	}
	if len(h.MCP) != 1 || h.MCP[0].Name != "telemetry" ||
		len(h.MCP[0].Command) != 3 || len(h.MCP[0].Tools) != 1 {
		t.Errorf("mcp grant lost a field: %+v", h.MCP)
	}
}

// TestMCPGrantNeedsExactlyOneTransport, and an unknown plugin mode is a typo
// rather than a silent fallback to whatever the default happens to be.
func TestHarnessRejectsAmbiguousGrants(t *testing.T) {
	for _, tc := range []struct{ name, harness, want string }{
		{"unknown plugin mode", `    harness:
      plugins: everything`, `harness.plugins "everything"`},
		{"mcp with neither transport", `    harness:
      mcp:
        - name: telemetry`, "exactly one of command or url"},
		{"mcp with both transports", `    harness:
      mcp:
        - name: telemetry
          url: https://example/mcp
          command: [node]`, "exactly one of command or url"},
		{"mcp declared twice", `    harness:
      mcp:
        - name: telemetry
          url: https://a/mcp
        - name: telemetry
          url: https://b/mcp`, "declared twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harnessCfg(t, tc.harness)
			if err == nil {
				t.Fatal("accepted an ambiguous grant")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not say what is wrong (%q, wanted %q)", err, tc.want)
			}
		})
	}
}

// TestHarnessEnvIsNamesOnly matches the refusal [confine.Profile.Wrap] already
// makes for a confine block's own env: a name, never an assignment, because a
// value here would put a credential in the config file rather than in the
// daemon's own environment where it belongs.
func TestHarnessEnvIsNamesOnly(t *testing.T) {
	for _, tc := range []struct{ name, env, want string }{
		{"assignment instead of a name", `    harness:
      env: ["OPENAI_API_KEY=sk-live-whatever"]`, "not an assignment"},
		{"empty entry", `    harness:
      env: [""]`, "not an assignment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harnessCfg(t, tc.env)
			if err == nil {
				t.Fatal("accepted harness.env carrying a value")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not say what is wrong (%q, wanted %q)", err, tc.want)
			}
		})
	}
}

// TestHarnessEnvNamesLoad: a well-formed list of names loads and is kept
// whole — the daemon's assemble step is what turns these into the agent's
// actual environment, and it can only forward what Load kept.
func TestHarnessEnvNamesLoad(t *testing.T) {
	c, err := harnessCfg(t, `    harness:
      env: [OPENAI_API_KEY, DATADOG_API_KEY]`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	h := c.Boxes[0].Harness
	if len(h.Env) != 2 || h.Env[0] != "OPENAI_API_KEY" || h.Env[1] != "DATADOG_API_KEY" {
		t.Errorf("harness.env lost a name: %v", h.Env)
	}
}
