package boxcfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
