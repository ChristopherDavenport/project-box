package acp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

func runSession(t *testing.T, scenario string, b agent.Bundle) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return runtimeFor(t, scenario).Run(ctx, b, "do the work")
}

// TestModelIsSetThroughTheProtocol. The bundle names a model; the runtime
// applies it through session/set_config_option on the option the agent tags
// with the protocol's model category, and the agent's own answer — not the
// request — is what the Result carries. No environment variable, no flag:
// the protocol has a verb for this and every ACP agent that exposes a model
// exposes it there.
func TestModelIsSetThroughTheProtocol(t *testing.T) {
	b := bundle()
	b.Model = "sonnet"
	res, err := runSession(t, "session", b)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := res.Session.Model(); got != "sonnet" {
		t.Fatalf("session reports model %q, want sonnet", got)
	}
	// The agent's own account, not the runtime's bookkeeping: the fake
	// reports what it is actually running when prompted.
	if !strings.Contains(res.Text, "model:sonnet") {
		t.Fatalf("the agent did not run on the requested model: %q", res.Text)
	}
	// "sonnet" lives inside a group in the fake's option list; flattening is
	// what made it a legal value.
	if opt := res.Session.ByCategory("model"); opt == nil || !slices.Contains(opt.Values, "sonnet") || !slices.Contains(opt.Values, "opus") {
		t.Fatalf("model option values not flattened over groups: %+v", opt)
	}
}

// TestSessionOptionsAreSetThroughTheProtocol: the rest of the configuration
// goes the same way, by the id the agent lists — including a boolean, which
// the protocol types separately and the runtime must send as one.
func TestSessionOptionsAreSetThroughTheProtocol(t *testing.T) {
	b := bundle()
	b.Session = map[string]string{"effort": "low", "fast_mode": "true"}
	res, err := runSession(t, "session", b)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(res.Text, "effort:low") || !strings.Contains(res.Text, "fast:true") {
		t.Fatalf("session options did not reach the agent: %q", res.Text)
	}
	if opt := res.Session.Option("fast_mode"); opt == nil || opt.Current != "true" || opt.Type != "boolean" {
		t.Fatalf("boolean option mis-carried: %+v", opt)
	}
}

// TestModelRefusalFailsClosed: an agent that refuses the model gets no
// prompt. Running on whatever it preferred instead is the silent default
// this seam exists to end.
func TestModelRefusalFailsClosed(t *testing.T) {
	b := bundle()
	b.Model = "sonnet"
	res, err := runSession(t, "stubborn", b)
	if !errors.Is(err, ErrSessionConfig) {
		t.Fatalf("a refused model did not fail the run: err=%v text=%q", err, res.Text)
	}
	if res.Text != "" {
		t.Fatalf("a prompt was sent after the model was refused: %q", res.Text)
	}
	// The refusal says what the agent DOES offer, because that is the
	// operator's next move.
	if !strings.Contains(err.Error(), "opus") || !strings.Contains(err.Error(), "sonnet") {
		t.Fatalf("the refusal does not list the agent's values: %v", err)
	}
	// And the Result still carries the session so the record can say so.
	if !res.Session.Opened || res.Session.Model() != "opus" {
		t.Fatalf("the refused run lost the session it opened: %+v", res.Session)
	}
}

// TestModelSubstitutionFailsClosed: an agent that says yes and runs its own
// preference is caught by the assertion, not trusted for the acknowledgement.
func TestModelSubstitutionFailsClosed(t *testing.T) {
	b := bundle()
	b.Model = "sonnet"
	_, err := runSession(t, "substitute", b)
	if !errors.Is(err, ErrSessionConfig) {
		t.Fatalf("a substituted model did not fail the run: %v", err)
	}
	if !strings.Contains(err.Error(), `answered model="opus"`) {
		t.Fatalf("the failure does not name what the agent answered: %v", err)
	}
}

// TestModelNeedsAnOptionToSet: an agent that exposes no configuration at all
// cannot be asked for a model, and a bundle that asks anyway is refused
// rather than run on an unknown. Asking for nothing is fine — and the
// record then says the agent exposed nothing, which is a different fact
// from the agent having a model nobody chose.
func TestModelNeedsAnOptionToSet(t *testing.T) {
	b := bundle()
	b.Model = "sonnet"
	if _, err := runSession(t, "nooptions", b); !errors.Is(err, ErrSessionConfig) {
		t.Fatalf("a model request against an agent with no options did not fail: %v", err)
	}
	res, err := runSession(t, "session", bundle())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Session.Model() != "opus" {
		t.Fatalf("asking for no model should leave the agent's default in force and on the record, got %q", res.Session.Model())
	}
}

// TestSessionSurfaceIsCarried is the whole surface in one Result: the modes,
// the mode in force after set_mode, the commands the agent offered, and the
// protocol's account of context and of the account's own limit.
func TestSessionSurfaceIsCarried(t *testing.T) {
	res, err := runSession(t, "session", bundle())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	s := res.Session
	if !s.Opened {
		t.Fatal("session not marked opened")
	}
	if s.Modes.Current != "acceptEdits" || !slices.Contains(s.Modes.Available, "default") {
		t.Fatalf("modes: %+v", s.Modes)
	}
	// Delivered, as distinct from granted: the agent listed the skills.
	if !slices.Contains(s.Commands, "box-skills:go") || !slices.Contains(s.Commands, "box-skills:sqlite") {
		t.Fatalf("commands did not carry the delivered skills: %v", s.Commands)
	}
	if res.Usage.ContextUsed != 34000 || res.Usage.ContextSize != 200000 {
		t.Fatalf("context occupancy lost: used=%d size=%d", res.Usage.ContextUsed, res.Usage.ContextSize)
	}
	rl := res.Usage.RateLimit
	if rl == nil || rl.Status != "allowed_warning" || rl.Window != "five_hour" || rl.Utilization != 0.82 || rl.ResetsAt != 1788491400 {
		t.Fatalf("the account's limit was not carried: %+v", rl)
	}
	for _, kind := range []string{"available_commands_update", "current_mode_update", "usage_update"} {
		if slices.Contains(res.UnknownUpdates, kind) {
			t.Fatalf("%s read as dialect drift; it is part of the protocol", kind)
		}
	}
}

// TestKnownUpdatesAreTheProtocolSet pins the runtime's vocabulary to the
// protocol's SessionUpdate union. A kind the protocol defines and the runtime
// does not name would read as drift on every run that used it, which is the
// alarm firing for the wrong reason.
func TestKnownUpdatesAreTheProtocolSet(t *testing.T) {
	// @agentclientprotocol/sdk schema/types.gen.d.ts, the version
	// claude-agent-acp 0.70.0 ships against.
	protocol := []string{
		"agent_message_chunk", "agent_thought_chunk", "available_commands_update",
		"config_option_update", "current_mode_update", "plan", "plan_removed",
		"plan_update", "session_info_update", "tool_call", "tool_call_update",
		"usage_update", "user_message_chunk",
	}
	got := slices.Clone(knownUpdates)
	slices.Sort(got)
	if !slices.Equal(got, protocol) {
		t.Fatalf("knownUpdates = %v\nprotocol     = %v", got, protocol)
	}
}
