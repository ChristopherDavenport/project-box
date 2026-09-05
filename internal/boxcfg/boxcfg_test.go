package boxcfg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "box.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAgentArgvAcceptsBothForms: the common case stays one word, and a
// confining wrapper is expressible (issue #6). A bare string is ONE argument,
// never a shell line — a config that reinterprets spaces runs something the
// operator did not write.
func TestAgentArgvAcceptsBothForms(t *testing.T) {
	for _, tc := range []struct {
		name, agent string
		want        []string
	}{
		{"scalar", `agent: claude-agent-acp`, []string{"claude-agent-acp"}},
		{"scalar with spaces stays one argument", `agent: "my agent"`, []string{"my agent"}},
		{"argv", `agent: [docker, run, --rm, -w, "{{workspace}}", img, claude-agent-acp]`,
			[]string{"docker", "run", "--rm", "-w", "{{workspace}}", "img", "claude-agent-acp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    seat: mason
    `+tc.agent+`
    repos:
      - name: app
        remote: /tmp/app.git
`))
			if err != nil {
				t.Fatal(err)
			}
			if got := []string(c.Boxes[0].Agent); !slices.Equal(got, tc.want) {
				t.Fatalf("agent argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConfineFoldsIntoTheAgentArgv: the daemon runs one argv and knows
// nothing about confinement, so declaring an image has to produce the wrapper
// rather than sit beside it. The rendering itself is asserted in
// internal/agent/confine; what matters here is that it reaches Agent and that
// the agent command survives at the tail.
func TestConfineFoldsIntoTheAgentArgv(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    confine:
      image: box/agent:1
      user: "1000:1000"
      env: [ANTHROPIC_API_KEY]
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	argv := []string(c.Boxes[0].Agent)
	if len(argv) < 2 || argv[0] != "docker" || argv[len(argv)-1] != "claude-agent-acp" {
		t.Fatalf("agent argv = %q, want the agent wrapped in a container", argv)
	}
	if !slices.ContainsFunc(argv, func(a string) bool { return strings.Contains(a, confine.ContextPlaceholder) }) {
		t.Errorf("agent argv = %q, want the context set mounted too — a wrapper told only about the workspace confines the agent out of its own reading material", argv)
	}
}

// TestConfineIsRefusedWhenItWouldConfineNothing: a repo's build command runs
// on the daemon itself, so a confine block with no agent is a security
// posture that silently applies to nothing — and an image is the one field
// with no sensible default, because the absence of one is a wrapper that
// cannot start.
func TestConfineIsRefusedWhenItWouldConfineNothing(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"no agent to wrap", `
tenant: acme
boxes:
  - name: shop
    confine:
      image: box/agent:1
    repos:
      - name: app
        remote: /tmp/app.git
        build: go build ./...
`},
		{"no image to wrap it in", `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    confine:
      user: "1000:1000"
    repos:
      - name: app
        remote: /tmp/app.git
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, tc.body)); err == nil {
				t.Fatal("accepted a confine block that confines nothing")
			}
		})
	}
}

// TestNoAgentAndNoBuildIsRefused: the existing opinion, still held once the
// agent field became a list — an empty list is no agent, not an agent named
// "".
func TestNoAgentAndNoBuildIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err == nil {
		t.Fatal("a repo with neither agent nor build command was accepted")
	}
}

// TestTokenBudgetLoads: the brake is configurable in the currency the invoice
// is denominated in.
func TestTokenBudgetLoads(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
budget:
  max_concurrent: 3
  max_tokens_per_window: 2000000
  window: 1h
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Budget.MaxTokensPerWindow != 2_000_000 {
		t.Fatalf("token budget = %d", c.Budget.MaxTokensPerWindow)
	}
	if c.WindowDuration().Hours() != 1 {
		t.Fatalf("window = %v", c.WindowDuration())
	}
}

// TestModelAndSessionLoad: the box names the model by the value the agent
// lists, and the rest of the session by option id.
func TestModelAndSessionLoad(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    model: sonnet
    session:
      effort: medium
      fast_mode: "true"
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if bx.Model != "sonnet" || bx.Session["effort"] != "medium" || bx.Session["fast_mode"] != "true" {
		t.Fatalf("model=%q session=%v", bx.Model, bx.Session)
	}
}

// TestSessionRefusesTheSpellingsThatHaveFields. Two ways to say the model
// would be two places for the record to disagree with itself; the permission
// mode is the tool policy's and travels through session/set_mode, never as an
// option. Both are refused at load with the field they wanted.
func TestSessionRefusesTheSpellingsThatHaveFields(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"model", "model has its own field"},
		{"mode", "permission mode"},
	} {
		_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    session:
      `+tc.key+`: x
    repos:
      - name: app
        remote: /tmp/app.git
`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("session.%s: err = %v, want %q", tc.key, err, tc.want)
		}
	}
	// And a session with no agent has nothing to configure.
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    model: sonnet
    repos:
      - name: app
        remote: /tmp/app.git
        build: make
`))
	if err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Errorf("model without an agent: err = %v", err)
	}
}
