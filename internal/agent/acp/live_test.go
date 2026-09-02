package acp

import (
	"context"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestLiveClaudeAgentBuilds drives the REAL claude-agent-acp through the
// whole factory loop: resolve, session, set_mode, prompt, the agent edits
// and commits by its own judgement, gates check, the lander lands.
//
// Gated on BOX_ACP_LIVE=<path-to-claude-agent-acp>: it spends a real model
// session and needs this machine's Claude auth, so it never runs in CI and
// never by accident — the fake-agent suite carries the protocol coverage;
// this proves the actual adapter speaks the same language.
func TestLiveClaudeAgentBuilds(t *testing.T) {
	bin := os.Getenv("BOX_ACP_LIVE")
	if bin == "" {
		t.Skip("set BOX_ACP_LIVE=/path/to/claude-agent-acp to run the live smoke (spends a real session)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	s, lp, router, st, remote := rig(t, "")
	b := lp.Builder.(*Builder)
	b.Command = []string{bin}
	b.Env = nil // the real agent needs the real environment, auth included
	// The only place a REAL agent's frames can be captured, which is what
	// makes the pinned dialect refreshable rather than hand-maintained: set
	// BOX_ACP_TRANSCRIPT and diff the result against testdata/. Off unless
	// asked — a transcript carries the prompt and the agent's output.
	if path := os.Getenv("BOX_ACP_TRANSCRIPT"); path != "" {
		tape, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer tape.Close()
		b.Transcript = tape
		t.Logf("recording the protocol transcript to %s", path)
	}
	// No denies for the live run: title formats differ between harnesses,
	// and a mistaken title match would kill a paid session. The fake-agent
	// suite owns the enforcement coverage.
	b.Composer = &agent.Composer{Source: agent.StaticLayers{{
		Name: "builder", Scope: agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: "d-live"},
		Instructions: "You are a build agent in an automated factory. Be precise and minimal.",
		Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
	}}}

	item := runItemCtx(t, ctx, s, lp, router, "live-1",
		"Create a file named hello.txt containing exactly the line 'hello from the factory' and commit it with the message 'add hello.txt'. Nothing else.")
	if item.State != queue.StateDone {
		t.Fatalf("live agent run: %+v", item)
	}
	out := mustSh(t, remote, "git", "show", "refs/heads/main:hello.txt")
	if !strings.Contains(out, "hello from the factory") {
		t.Fatalf("the landed file is wrong:\n%s", out)
	}
	t.Logf("landed by a real agent: %s", item.Head)
	if wake, ok, _ := st.WakeLayer(ctx, 5); ok {
		t.Logf("handoff received:\n%s", wake.Instructions)
	}
}
