package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
)

// TestMain doubles this test binary as the fake agent: re-executed with
// ACP_FAKE set, it speaks the agent side of the protocol instead of running
// tests. The enforcement tests need a real subprocess and a real stream — a
// mocked conn would test the mock.
func TestMain(m *testing.M) {
	if s := os.Getenv("ACP_FAKE"); s != "" {
		fakeAgent(s)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runtimeFor(t *testing.T, scenario string) *Runtime {
	t.Helper()
	return &Runtime{
		Command: []string{os.Args[0]},
		Dir:     t.TempDir(),
		Env:     []string{"ACP_FAKE=" + scenario},
	}
}

func bundle() agent.Bundle {
	return agent.Bundle{
		Ref: "test-ref",
		Tools: agent.ToolPolicy{
			Allow:          []string{"Bash", "Edit", "Read"},
			Deny:           []string{"DeleteProd"},
			PermissionMode: "acceptEdits",
		},
	}
}

// TestDeniedToolIsDeniedAtTheStream is the Phase 2 exit criterion, by a test
// that actually tries it: the agent asks permission for an allowed tool and a
// denied one, and the denied one is refused on the wire.
func TestDeniedToolIsDeniedAtTheStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "polite").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason: %q", res.StopReason)
	}
	if !slices.Contains(res.Denied, "DeleteProd") {
		t.Fatalf("DeleteProd was not denied: %+v", res)
	}
	if slices.Contains(res.Denied, "Bash") {
		t.Fatal("an allowed tool was denied")
	}
	// The fake echoes what the wire told it: Bash allowed, DeleteProd
	// rejected, and the permission mode the resolver chose — not the harness
	// default — on its command line.
	for _, want := range []string{"bash:allowed", "deleteprod:rejected", "mode:acceptEdits"} {
		if !contains(res.Text, want) {
			t.Fatalf("agent's view missing %q in %q", want, res.Text)
		}
	}
}

// TestPermissionIsJudgedByPathNotJustName: issue #73. A builder whose
// workspace sits inside the operator's own checkout read and wrote it by
// absolute path, and nothing refused it — the runtime's tool policy judges
// names at the stream and never paths. A file-shaped ask is now judged
// against the workspace, the materialized context set, and the skills
// directory (the three subtrees a confining wrapper is told to let through),
// refused when its path falls outside all three or carries none at all, and
// Bash is left to the name check alone since a command line is not a path.
func TestPermissionIsJudgedByPathNotJustName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "pathy")
	rt.ContextDir = t.TempDir()
	outside := t.TempDir()
	rt.Env = append(rt.Env, "ACP_FAKE_OUTSIDE="+outside, "ACP_FAKE_CONTEXT="+rt.ContextDir)
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason: %q", res.StopReason)
	}
	for _, want := range []string{
		"read-inside:allowed",
		"read-outside:denied",
		"read-context:allowed",
		"read-nopath:denied",
		"bash-outside:allowed",
	} {
		if !contains(res.Text, want) {
			t.Errorf("agent's view missing %q in %q", want, res.Text)
		}
	}
	// The refused-by-path denial names both the tool and the path, so the
	// record reads "Read /home/..." rather than just "Read".
	found := false
	for _, d := range res.Denied {
		if strings.Contains(d, "Read") && strings.Contains(d, outside) {
			found = true
		}
	}
	if !found {
		t.Errorf("res.Denied does not carry the tool and the refused path: %+v", res.Denied)
	}
	if slices.Contains(res.Denied, "Read") {
		t.Error("the in-bounds and context reads were denied")
	}
}

// TestRogueToolKillsSession: an agent that runs a denied tool WITHOUT asking —
// a mode that never prompts — is killed at the first sight of it. Advisory
// enforcement would be a log line; this is the difference.
func TestRogueToolKillsSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "rogue").Run(ctx, bundle(), "do the work")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("want ErrPolicyViolation, got %v (res %+v)", err, res)
	}
	if !slices.Contains(res.ToolCalls, "DeleteProd") {
		t.Fatalf("the violating call should be on the record: %+v", res)
	}
}

// TestEscalationIsARecordedAct: the agent declines with its reason; the
// runtime carries it out, acknowledged — never refused.
func TestEscalationIsARecordedAct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "escalate").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Escalated != "task needs credentials I do not hold" {
		t.Fatalf("escalation lost: %+v", res)
	}
	if contains(res.Text, "refused") {
		t.Fatal("the refusal channel was refused")
	}
}

// TestMidRunHandoffIsARecordedAct: the agent asks for a fresh session and
// says what its successor needs; the runtime carries both out, acknowledged.
//
// Acknowledged is the load-bearing half, as it is for escalate. The answer an
// agent falls back on when a channel refuses it is the degraded commit, which
// is the thing this verb exists to stop it producing.
func TestMidRunHandoffIsARecordedAct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "handoff").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.HandedOff != "context is nearly full and the migration is half applied" {
		t.Fatalf("handoff lost: %+v", res)
	}
	if res.Escalated != "" {
		t.Fatalf("a handoff read as an escalation: %+v", res)
	}
	// One claim per line: a wall of prose is one note nobody can supersede or
	// mark stale on its own.
	want := []string{"widget.go is migrated", "the tests in widget_test.go are not"}
	if !slices.Equal(res.HandoffNotes, want) {
		t.Fatalf("notes = %q, want %q", res.HandoffNotes, want)
	}
	if contains(res.Text, "refused") {
		t.Fatal("the handoff channel was refused")
	}
}

// TestUsageIsCarriedFromTheTurn: the turn's own total is the account, and it
// is what the capacity meter charges. Cache reads count — cheaper, never free.
func TestUsageIsCarriedFromTheTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "metered").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Usage.Reported {
		t.Fatal("a reported cost read as unreported")
	}
	if got := res.Usage.Total(); got != 11000 {
		t.Fatalf("total tokens %d, want 11000 (1200+300+9000+500)", got)
	}
	// The cache figures are the point. Reading only input+output would give
	// 1500 here and look entirely plausible.
	if res.Usage.CachedReadTokens != 9000 || res.Usage.CachedWriteTokens != 500 {
		t.Fatalf("cache counters lost to a field-name mismatch: %+v", res.Usage)
	}
	if res.Usage.CostUSD != 0.0432 {
		t.Fatalf("the turn's cost in money was dropped: %+v", res.Usage)
	}
	if slices.Contains(res.UnknownUpdates, "usage_update") {
		t.Fatal("usage_update read as dialect drift; it is part of the dialect")
	}
}

// TestRenamedUsageFieldsAreLoud: a usage object present and parsing to nothing
// is what a rename looks like from here, and it is indistinguishable from a
// free turn unless something says so. This is the case that actually bit —
// the cache counters were spelled with the Anthropic API's names rather than
// the adapter's, so two of four fields read zero and Reported stayed true.
func TestRenamedUsageFieldsAreLoud(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "renamed").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Contains(res.UnknownUpdates, "usage:all-zero") {
		t.Fatalf("a usage object that parsed to nothing went unremarked: %+v", res)
	}
}

// TestUsageAccumulatesFromTheStream: a dialect that reports per update rather
// than per turn is still metered, and the two are never summed together.
func TestUsageAccumulatesFromTheStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "streamusage").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Usage.Reported || res.Usage.Total() != 300 {
		t.Fatalf("streamed usage lost: %+v", res.Usage)
	}
}

// TestSilenceIsNotZeroSpend is the whole reason Usage carries Reported. An
// agent that says nothing about cost has not run for free, and a meter that
// reads unknown as zero is a brake that quietly disengaged.
func TestSilenceIsNotZeroSpend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "polite").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Usage.Reported {
		t.Fatal("silence was recorded as a cost report")
	}
	if res.Usage.Total() != 0 {
		t.Fatalf("unreported usage invented a total: %+v", res.Usage)
	}
}

// TestDialectDriftIsLoud: the last drift zeroed a metric across eight
// experiment runs and nobody noticed until a human read the column. An
// unrecognised update kind is now a fact on the result, and a tool call
// announced in the update spelling is still counted and still judged —
// the same shapes carry policy, so parsing them is not only bookkeeping.
func TestDialectDriftIsLoud(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "drift").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Contains(res.ToolCalls, "Bash") {
		t.Fatalf("a tool call in the update spelling went uncounted: %+v", res.ToolCalls)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("one call counted more than once: %+v", res.ToolCalls)
	}
	if !slices.Contains(res.UnknownUpdates, "reasoning_delta") {
		t.Fatalf("an unknown update kind was swallowed: %+v", res.UnknownUpdates)
	}
}

// TestProtocolDialectIsAsserted: a version we do not speak is refused at
// initialize, before a session is opened or a token is spent.
func TestProtocolDialectIsAsserted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := runtimeFor(t, "dialect").Run(ctx, bundle(), "do the work")
	if !errors.Is(err, ErrProtocolDialect) {
		t.Fatalf("want ErrProtocolDialect, got %v", err)
	}
}

// TestSetModeTimeoutIsNotARefusal: issue #102. An agent that never answers
// session/set_mode is a clock running out, not the agent refusing the mode —
// the two used to read identically as "agent refused permission mode". The
// fake never replies, so a few hundred milliseconds is safe: there is no
// legitimate answer this could race against.
func TestSetModeTimeoutIsNotARefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := runtimeFor(t, "mode-timeout").Run(ctx, bundle(), "do the work")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if !strings.Contains(err.Error(), "context deadline exceeded waiting for session/set_mode") {
		t.Fatalf("error does not name the request in flight: %v", err)
	}
	if strings.Contains(err.Error(), "refused") {
		t.Fatalf("a timeout must not read as a refusal: %v", err)
	}
}

// TestUnnamedEnvDoesNotReachTheAgent: the daemon's whole environment used to
// reach every agent by nothing more than starting a subprocess in it (issue
// #67). A canary the box never named must not survive that anymore. Not
// t.Parallel: t.Setenv forbids it.
func TestUnnamedEnvDoesNotReachTheAgent(t *testing.T) {
	t.Setenv("ACP_ENV_CANARY", "leaked-secret-value")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "envecho").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if contains(res.Text, "ACP_ENV_CANARY") {
		t.Fatalf("a variable the box never named reached the agent:\n%s", res.Text)
	}
	if contains(res.Text, "leaked-secret-value") {
		t.Fatalf("the canary's value reached the agent:\n%s", res.Text)
	}
}

// TestNamedEnvReachesTheAgent: a name the box DOES ask for, via EnvNames,
// reaches the agent with its value intact.
func TestNamedEnvReachesTheAgent(t *testing.T) {
	t.Setenv("ACP_ENV_NAMED", "the-value")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "envecho")
	rt.EnvNames = []string{"ACP_ENV_NAMED"}
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !contains(res.Text, "ACP_ENV_NAMED=the-value") {
		t.Fatalf("a name the box asked for did not reach the agent:\n%s", res.Text)
	}
	if !slices.Contains(res.EnvNames, "ACP_ENV_NAMED") {
		t.Fatalf("the delivered name is not on the result: %v", res.EnvNames)
	}
}

// TestBaseEnvReachesTheAgent: PATH and HOME reach the agent with no box
// naming either — the dogfood box runs claude-agent-acp with no confine
// block, and HOME and PATH are what keep it able to find the binary and its
// own model credential.
func TestBaseEnvReachesTheAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "envecho").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")} {
		if !contains(res.Text, name) {
			t.Fatalf("base env %q did not reach the agent:\n%s", name, res.Text)
		}
	}
}

// TestConfigDirReachesTheAgent: CLAUDE_CONFIG_DIR, when the daemon's own
// environment carries it, reaches the agent — the harness config dir
// resolver's own doc comment says why: a daemon and the agents it spawns must
// never disagree about which machine state is in play.
func TestConfigDirReachesTheAgent(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/some-claude-config-dir")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "envecho").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !contains(res.Text, "CLAUDE_CONFIG_DIR=/tmp/some-claude-config-dir") {
		t.Fatalf("CLAUDE_CONFIG_DIR did not reach the agent:\n%s", res.Text)
	}
}

// TestExplicitEnvAlwaysWinsOverEnvNames: Env's own assignment for a name also
// listed in EnvNames must win — the two are different things, and the test
// rig's own ACP_FAKE would silently break if Env stopped winning.
func TestExplicitEnvAlwaysWinsOverEnvNames(t *testing.T) {
	t.Setenv("ACP_ENV_NAMED", "from-the-daemon")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "envecho")
	rt.EnvNames = []string{"ACP_ENV_NAMED"}
	rt.Env = append(rt.Env, "ACP_ENV_NAMED=from-the-runtime")
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !contains(res.Text, "ACP_ENV_NAMED=from-the-runtime") {
		t.Fatalf("Env's explicit assignment did not win:\n%s", res.Text)
	}
	if contains(res.Text, "from-the-daemon") {
		t.Fatalf("the daemon's value leaked despite an explicit override:\n%s", res.Text)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(haystack) > 0 && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})())
}

// --- the fake agent -------------------------------------------------------

type fakeConn struct {
	enc *json.Encoder
	dec *bufio.Scanner
	id  int64
}

func (f *fakeConn) write(m map[string]any) {
	m["jsonrpc"] = "2.0"
	f.enc.Encode(m)
}

func (f *fakeConn) read() map[string]any {
	for f.dec.Scan() {
		if len(f.dec.Bytes()) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(f.dec.Bytes(), &m) == nil {
			return m
		}
	}
	os.Exit(0) // client hung up
	return nil
}

// request sends an agent->client request and blocks for its response.
func (f *fakeConn) request(method string, params map[string]any) map[string]any {
	f.id++
	f.write(map[string]any{"id": f.id, "method": method, "params": params})
	for {
		m := f.read()
		if m["id"] != nil && m["method"] == nil {
			return m
		}
		// Interleaved client call while we wait: shouldn't happen in these
		// scenarios; answer nothing and keep waiting.
	}
}

func (f *fakeConn) notify(method string, params map[string]any) {
	f.write(map[string]any{"method": method, "params": params})
}

func fakeAgent(scenario string) {
	f := &fakeConn{enc: json.NewEncoder(os.Stdout), dec: bufio.NewScanner(os.Stdin)}
	// The scripted scenarios below each probe one shape. "replay:<path>" is
	// the whole dialect instead, supplied as data — see conformance_test.go.
	if path, ok := strings.CutPrefix(scenario, "replay:"); ok {
		replayTranscript(f, path)
		return
	}
	mode := ""
	var sessionID string
	// loadedID and loadedCwd are what the last session/load call carried, so
	// a resume scenario's prompt handler can echo them back for the test to
	// assert against — the same convention "session"'s prompt reply uses for
	// model/effort/fast/mode.
	var loadedID, loadedCwd string
	// resumeScenarios advertise the loadSession capability at initialize:
	// every scenario in this file that exercises session/load. A scenario
	// absent from this set is what "the agent does not advertise resume" is
	// tested against, without needing a scenario of its own.
	resumeScenarios := map[string]bool{
		"resume": true, "resume-load-fails": true, "resume-mode-refused": true,
	}
	// The session configuration the fake exposes, shaped like the real
	// adapter's: a model option tagged with the protocol's model category
	// (one plain value and one group, so flattening is exercised), an
	// effort option, and a boolean. Defaults stand in for "whatever the
	// machine says".
	model, effort, fast := "opus", "high", false
	configOptions := func() []map[string]any {
		return []map[string]any{
			{"id": "mode", "name": "Mode", "category": "mode", "type": "select", "currentValue": mode,
				"options": []map[string]any{{"value": "default", "name": "Default"}, {"value": "acceptEdits", "name": "Accept edits"}}},
			{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": model,
				"options": []map[string]any{
					{"value": "opus", "name": "Opus"},
					{"group": "small", "name": "Small", "options": []map[string]any{{"value": "sonnet", "name": "Sonnet"}, {"value": "haiku", "name": "Haiku"}}},
				}},
			{"id": "effort", "name": "Effort", "category": "thought_level", "type": "select", "currentValue": effort,
				"options": []map[string]any{{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}}},
			{"id": "fast_mode", "name": "Fast mode", "type": "boolean", "currentValue": fast},
		}
	}
	// newMeta is what session/new carried, kept so a scenario can hand it
	// back. The harness surface is delivered in that one call and nowhere
	// else, so echoing it is the only way to assert from outside that what
	// the bundle granted is what the agent was actually offered.
	newMeta := "null"
	for {
		m := f.read()
		id := m["id"]
		switch m["method"] {
		case "initialize":
			v := 1
			if scenario == "dialect" {
				v = 2 // an agent from a protocol this runtime has not read
			}
			result := map[string]any{"protocolVersion": v}
			if resumeScenarios[scenario] {
				result["agentCapabilities"] = map[string]any{"loadSession": true}
			}
			f.write(map[string]any{"id": id, "result": result})
		case "session/new":
			sessionID = "sess-1"
			if p, ok := m["params"].(map[string]any); ok {
				if b, err := json.Marshal(p["_meta"]); err == nil {
					newMeta = string(b)
				}
			}
			result := map[string]any{"sessionId": sessionID}
			if scenario != "nooptions" {
				result["modes"] = map[string]any{"currentModeId": "default",
					"availableModes": []map[string]any{{"id": "default", "name": "Default"}, {"id": "acceptEdits", "name": "Accept edits"}}}
				result["configOptions"] = configOptions()
			}
			f.write(map[string]any{"id": id, "result": result})
			// What the real adapter does the instant a session exists: list
			// the commands — the plugin skills the bundle granted among them.
			f.notify("session/update", map[string]any{"sessionId": sessionID,
				"update": map[string]any{"sessionUpdate": "available_commands_update",
					"availableCommands": []map[string]any{
						{"name": "box-skills:go", "description": "Idiomatic Go"},
						{"name": "box-skills:sqlite", "description": "SQLite best practices"},
					}}})
		case "session/load":
			p, _ := m["params"].(map[string]any)
			loadedID, _ = p["sessionId"].(string)
			loadedCwd, _ = p["cwd"].(string)
			if scenario == "resume-load-fails" {
				f.write(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": "no such session"}})
				break
			}
			sessionID = loadedID
			// The protocol replays the conversation as notifications rather
			// than answering it inline — one turn of it, enough for the
			// runtime's tolerance of a replay under this call to matter.
			f.notify("session/update", map[string]any{"sessionId": sessionID,
				"update": map[string]any{"sessionUpdate": "agent_message_chunk",
					"content": map[string]any{"type": "text", "text": "(replayed) the earlier turn\n"}}})
			f.write(map[string]any{"id": id, "result": map[string]any{
				"modes": map[string]any{"currentModeId": "default",
					"availableModes": []map[string]any{{"id": "default", "name": "Default"}, {"id": "acceptEdits", "name": "Accept edits"}}},
				"configOptions": configOptions(),
			}})
		case "session/set_mode":
			if scenario == "resume-mode-refused" {
				f.write(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "mode refused on the resumed session"}})
				break
			}
			if scenario == "mode-timeout" {
				// Never answers: the runtime's context expires waiting for
				// this call, which must read as a timeout, not a refusal.
				break
			}
			if p, ok := m["params"].(map[string]any); ok {
				mode, _ = p["modeId"].(string)
			}
			f.write(map[string]any{"id": id, "result": map[string]any{}})
			f.notify("session/update", map[string]any{"sessionId": sessionID,
				"update": map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": mode}})
		case "session/set_config_option":
			p, _ := m["params"].(map[string]any)
			configID, _ := p["configId"].(string)
			switch {
			case scenario == "stubborn":
				// The real adapter's refusal: a JSON-RPC error naming the value.
				f.write(map[string]any{"id": id, "error": map[string]any{"code": -32602,
					"message": fmt.Sprintf("Invalid value for config option %s: %v", configID, p["value"])}})
				continue
			case scenario == "substitute" && configID == "model":
				// Says yes, does its own thing: the answer is a model that
				// was not asked for, which the runtime must catch.
				model = "opus"
			case configID == "model":
				model, _ = p["value"].(string)
			case configID == "effort":
				effort, _ = p["value"].(string)
			case configID == "fast_mode":
				fast, _ = p["value"].(bool)
			case configID == "mode":
				mode, _ = p["value"].(string)
			}
			f.write(map[string]any{"id": id, "result": map[string]any{"configOptions": configOptions()}})
		case "session/prompt":
			var report string
			switch scenario {
			case "harness":
				f.notify("session/update", map[string]any{
					"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": newMeta}},
				})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "envecho":
				// Reports the FAKE AGENT'S OWN environment back over the wire —
				// this is the real subprocess's os.Environ(), not a mock, so
				// asserting on it is a subprocess-level check of what buildEnv
				// actually constructed for cmd.Env.
				f.notify("session/update", map[string]any{
					"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": strings.Join(os.Environ(), "\n")}},
				})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "polite":
				report = "mode:" + mode
				for _, tool := range []string{"Bash", "DeleteProd"} {
					resp := f.request("session/request_permission", map[string]any{
						"sessionId": sessionID,
						"toolCall":  map[string]any{"toolCallId": "t-" + tool, "title": tool, "kind": "execute"},
						"options": []map[string]any{
							{"optionId": "yes", "name": "Allow", "kind": "allow_once"},
							{"optionId": "no", "name": "Reject", "kind": "reject_once"},
						},
					})
					verdict := "rejected"
					if res, ok := resp["result"].(map[string]any); ok {
						if oc, ok := res["outcome"].(map[string]any); ok && oc["optionId"] == "yes" {
							verdict = "allowed"
						}
					}
					report += fmt.Sprintf(" %s:%s", lower(tool), verdict)
				}
				f.notify("session/update", map[string]any{
					"sessionId": sessionID,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": report}},
				})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "pathy":
				// Issue #73: a permission ask judged by name alone never
				// catches a builder that reads or writes outside its own
				// workspace — nothing judges WHERE. This scenario asks for a
				// Read at five different paths and one Bash, the way the real
				// dialect carries rawInput and _meta.claudeCode.toolName on a
				// permission ask.
				cwd, _ := os.Getwd()
				outside := os.Getenv("ACP_FAKE_OUTSIDE")
				ctxDir := os.Getenv("ACP_FAKE_CONTEXT")
				ask := func(callID, tool string, rawInput map[string]any) string {
					resp := f.request("session/request_permission", map[string]any{
						"sessionId": sessionID,
						"toolCall": map[string]any{
							"toolCallId": callID, "title": tool, "kind": "read",
							"rawInput": rawInput,
							"_meta":    map[string]any{"claudeCode": map[string]any{"toolName": tool}},
						},
						"options": []map[string]any{
							{"optionId": "yes", "name": "Allow", "kind": "allow_once"},
							{"optionId": "no", "name": "Reject", "kind": "reject_once"},
						},
					})
					if res, ok := resp["result"].(map[string]any); ok {
						if oc, ok := res["outcome"].(map[string]any); ok && oc["optionId"] == "yes" {
							return "allowed"
						}
					}
					return "denied"
				}
				report = "read-inside:" + ask("t-in", "Read", map[string]any{"file_path": filepath.Join(cwd, "inside.txt")})
				report += " read-outside:" + ask("t-out", "Read", map[string]any{"file_path": filepath.Join(outside, "evil.txt")})
				report += " read-context:" + ask("t-ctx", "Read", map[string]any{"file_path": filepath.Join(ctxDir, "doc.md")})
				report += " read-nopath:" + ask("t-nopath", "Read", map[string]any{})
				report += " bash-outside:" + ask("t-bash", "Bash", map[string]any{"command": "cat " + filepath.Join(outside, "evil.txt")})
				f.notify("session/update", map[string]any{
					"sessionId": sessionID,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": report}},
				})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "escalate":
				resp := f.request("_box/escalate", map[string]any{"reason": "task needs credentials I do not hold"})
				if res, ok := resp["result"].(map[string]any); !ok || res["acknowledged"] != true {
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "escalation refused?!"}}})
				}
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "handoff":
				resp := f.request("_box/handoff", map[string]any{
					"reason": "context is nearly full and the migration is half applied",
					"notes":  "widget.go is migrated\n\nthe tests in widget_test.go are not\n",
				})
				if res, ok := resp["result"].(map[string]any); !ok || res["acknowledged"] != true {
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "handoff refused?!"}}})
				}
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "halfway", "halfwaytext":
				// A session that gets part of the way and runs out of room. It
				// commits what it has — the successor continues in this tree —
				// and asks for a fresh session rather than degrading.
				os.WriteFile("part.txt", []byte("half the widget\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "half the widget")
				if scenario == "halfway" {
					f.request("_box/handoff", map[string]any{
						"reason": "out of room with the widget half written",
						"notes":  "part.txt holds the first half; widget.go is what remains",
					})
					f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
					break
				}
				// The other channel, for a harness that cannot call an unknown
				// method — which is every claude-agent-acp shipped so far.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text",
							"text": "HANDOFF-NOW: out of room with the widget half written\nHANDOFF: part.txt holds the first half; widget.go is what remains\n"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "finisher":
				// The fresh session. It finishes what is already in the tree,
				// and refuses to invent it again if the tree came back empty —
				// so a successor that had to redo the work fails the test
				// loudly instead of landing an identical-looking commit.
				if _, err := os.Stat("part.txt"); err != nil {
					f.request("_box/escalate", map[string]any{
						"reason": "the predecessor's work is not in this workspace"})
					f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
					break
				}
				os.WriteFile("widget.go", []byte("package app // finished by the successor\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "finish the widget")
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "Finished what the last session started.\nHANDOFF: the widget is whole now\n"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "worker", "resume-load-fails":
				// A real day's work: edit, commit, hand off. "resume-load-fails"
				// rides the same prompt handling because its own point is
				// upstream of here — session/load refuses — and the cold
				// attempt that follows a refused continuation is an ordinary
				// build like this one.
				os.WriteFile("widget.go", []byte("package app // built by the fake worker\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "add widget")
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "Added the widget and committed.\nHANDOFF: the widget package expects blue as the default\n"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "repair":
				// The run after a red one: it fixes what is already on the
				// branch rather than starting the work over.
				os.WriteFile("fix.go", []byte("package app // the repair\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "fix the red")
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "Fixed the default and committed.\nHANDOFF: the red was the widget default, not the test\n"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "lazy":
				// Declares victory, changes nothing, escalates nothing —
				// the silent failure the adapter must never let look done.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "All done! (it was not)"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "toolsy":
				// A day's work with a paper trail: one tool used, one asked
				// for and refused.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call",
						"toolCallId": "t-1", "title": "Bash", "kind": "execute"}})
				f.request("session/request_permission", map[string]any{
					"sessionId": sessionID,
					"toolCall":  map[string]any{"toolCallId": "t-2", "title": "DeleteProd", "kind": "execute"},
					"options": []map[string]any{
						{"optionId": "yes", "name": "Allow", "kind": "allow_once"},
						{"optionId": "no", "name": "Reject", "kind": "reject_once"},
					},
				})
				os.WriteFile("widget.go", []byte("package app // built by the fake worker\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "add widget")
				f.write(map[string]any{"id": id, "result": map[string]any{
					"stopReason": "end_turn",
					"usage":      map[string]any{"inputTokens": 10, "outputTokens": 5},
				}})
			case "resume":
				// The continued session: it picks up mid-task rather than
				// starting over, so its commit is distinct from a cold
				// "worker" run's — and it echoes what session/load carried,
				// so the test can assert the id and cwd it was actually
				// sent.
				os.WriteFile("resumed.go", []byte("package app // continued by the fake resumed session\n"), 0o644)
				gitc("add", "-A")
				gitc("commit", "-m", "continue after the gate's refusal")
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text",
							"text": fmt.Sprintf("resumed:%s cwd:%s\nHANDOFF: continued from the refused attempt\n", loadedID, loadedCwd)}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn",
					"usage": map[string]any{"inputTokens": 10, "outputTokens": 5}}})
			case "session":
				// Reports the session as it stands, so a test can assert that
				// what was set is what is in force — and the protocol's
				// account of context and the account's limit, as the real
				// adapter sends them on usage_update.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "usage_update", "used": 12000, "size": 200000}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "usage_update", "used": 34000, "size": 200000,
						"_meta": map[string]any{"_claude/rateLimit": map[string]any{
							"status": "allowed_warning", "rateLimitType": "five_hour",
							"utilization": 0.82, "resetsAt": 1788491400}}}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text",
							"text": fmt.Sprintf("model:%s effort:%s fast:%v mode:%s", model, effort, fast, mode)}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn",
					"usage": map[string]any{"inputTokens": 10, "outputTokens": 5}}})
			case "planner", "planner-bad", "planner-noplan":
				// A plan in the planner's own answer shape, with prose around
				// it and a brace inside a task, the way an agent actually
				// replies. "planner-bad" names a section the repository does
				// not declare; "planner-noplan" never gets to the JSON.
				section := "api"
				if scenario == "planner-bad" {
					section = "db"
				}
				answer := "Read the tree. Two units, the view after the endpoint.\n\n```json\n" +
					`{"units":[{"id":"api-endpoint","task":"Add GET /v1/widgets in internal/api (returns {\"widgets\":[]})","summary":"Widgets endpoint","sections":["` + section + `"],"depends_on":[],"model":"haiku"},` +
					`{"id":"room-view","task":"Render widgets in web/src","summary":"Widgets view","sections":["web"],"depends_on":["api-endpoint"]}],"why":"the view needs the endpoint"}` +
					"\n```\nHANDOFF: widgets live under internal/api\n"
				if scenario == "planner-noplan" {
					answer = "I looked around and it all seems fine to me."
				}
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": answer}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn",
					"usage": map[string]any{"inputTokens": 900, "outputTokens": 120}}})
			case "planner-escalate":
				f.request("_box/escalate", map[string]any{"reason": "this origination is a question, not a change"})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "review-pass", "review-fail", "review-silent", "review-neither",
				"review-resolved", "review-resolutions-missing", "review-unresolved-pass":
				// The gate role's occupant. Every scenario first reports what
				// it was actually handed — where it is standing, whether that
				// place will take a write, and the patch it was given — so a
				// test can assert the material from the agent's side of the
				// wire rather than from the directory the test laid out.
				cwd, _ := os.Getwd()
				writable := "no"
				if err := os.WriteFile("probe.txt", []byte("x"), 0o644); err == nil {
					writable = "yes"
					os.Remove("probe.txt")
				}
				patch, _ := os.ReadFile("diff.patch")
				answer := fmt.Sprintf("cwd:%s writable:%s\n--- diff.patch ---\n%s--- end of diff.patch ---\n", cwd, writable, patch)
				switch scenario {
				case "review-pass":
					answer += "\n```json\n" + `{"verdict":"pass","reason":"does what the origination asked, inside the section it declared"}` +
						"\n```\nHANDOFF: the widget package is reviewed and its default is blue\n"
				case "review-fail":
					answer += "\n```json\n" + `{"verdict":"fail","reason":"the change edits db/schema.sql, which the unit never declared"}` + "\n```\n"
				case "review-silent":
					answer += "\nLooks fine to me, I suppose.\n"
				case "review-neither":
					answer += "\n```json\n" + `{"verdict":"maybe","reason":"I could not make up my mind"}` + "\n```\n"
				case "review-resolved":
					// Attempt 1's finding is fixed; the diff still has a new
					// problem of its own, so the change fails on that even
					// though nothing earlier survived.
					answer += "\n```json\n" + `{"verdict":"fail","reason":"schema.sql is no longer touched, but api/widget.go now reaches outside its declared section",` +
						`"resolved":[{"attempt":1,"gate":"review","resolved":true,"note":"db/schema.sql is untouched now"}]}` + "\n```\n"
				case "review-resolutions-missing":
					// A prior-carrying answer that never says whether the
					// named finding was fixed — trouble, not a verdict.
					answer += "\n```json\n" + `{"verdict":"fail","reason":"still looks wrong to me"}` + "\n```\n"
				case "review-unresolved-pass":
					// Self-contradictory: marks the earlier finding
					// unresolved and passes the change anyway.
					answer += "\n```json\n" + `{"verdict":"pass","reason":"good enough",` +
						`"resolved":[{"attempt":1,"gate":"review","resolved":false,"note":"db/schema.sql is still touched"}]}` + "\n```\n"
				}
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": answer}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn",
					"usage": map[string]any{"inputTokens": 4200, "outputTokens": 90}}})
			case "metered":
				// The real dialect, copied from claude-agent-acp's
				// sessionUsage() and turnOutcome(): usage on the prompt
				// response root, plus a usage_update carrying the money.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "usage_update",
						"used": 11000, "size": 200000,
						"cost": map[string]any{"amount": 0.0432, "currency": "USD"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{
					"stopReason": "end_turn",
					"usage": map[string]any{
						"inputTokens": 1200, "outputTokens": 300,
						"cachedReadTokens": 9000, "cachedWriteTokens": 500,
						"totalTokens": 11000,
					}}})
			case "renamed":
				// The failure this pinning exists to catch: a usage object is
				// present, and every field in it has been renamed.
				f.write(map[string]any{"id": id, "result": map[string]any{
					"stopReason": "end_turn",
					"usage": map[string]any{
						"input_tokens": 1200, "output_tokens": 300,
					}}})
			case "streamusage":
				// Cost dribbled out per update instead, under _meta.
				for range 2 {
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "agent_message_chunk",
							"content": map[string]any{"type": "text", "text": "chunk"}},
						"_meta": map[string]any{"usage": map[string]any{"inputTokens": 100, "outputTokens": 50}},
					})
				}
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "spendy":
				// A run that never stops spending and never settles. Nothing
				// but the ceiling can end this: no stop reason is coming, and
				// the only other bound is a wall clock.
				for {
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "agent_message_chunk",
							"content": map[string]any{"type": "text", "text": "still going"}},
						"_meta": map[string]any{"usage": map[string]any{"inputTokens": 1000, "outputTokens": 0}},
					})
					time.Sleep(2 * time.Millisecond)
				}
			case "drift":
				// The dialect moved: a tool call announced only in the update
				// spelling, and a kind this runtime has never heard of.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update",
						"toolCallId": "t-9", "title": "Bash", "status": "in_progress"}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "reasoning_delta",
						"content": map[string]any{"type": "text", "text": "hmm"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "metaname":
				// The real dialect: tool_call_update carries no title, and
				// names the tool only under _meta.claudeCode.toolName. The
				// title, when there is one, is prose — for Bash it is the
				// whole command line, which an exact-match policy can never
				// match and a tally counts as a brand new tool every time.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update",
						"toolCallId": "t-1", "status": "in_progress",
						"_meta": map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}}}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update",
						"toolCallId": "t-2", "status": "in_progress",
						"title": "cd /work && find . -name '*.go' | head -80",
						"_meta": map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "latetitle":
				// The real dialect for a Read and a Bash, shape for shape, as
				// recorded against claude-agent-acp 0.70 on 2026-09-05: the
				// call announced with a placeholder title and an empty
				// content LIST, the proper title on the update that follows
				// (with content, which for a Bash carries its description),
				// a bare _meta-only update, the title repeated, and the
				// completion with the output. Content is a list on every one
				// of these and an object only on a message chunk; a decoder
				// that typed it for the chunk dropped all of these but the
				// bare one.
				meta := func(tool string) map[string]any {
					return map[string]any{"claudeCode": map[string]any{"toolName": tool}}
				}
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t-1",
						"title": "Read File", "kind": "read", "status": "pending",
						"rawInput": map[string]any{}, "locations": []any{}, "content": []any{}, "_meta": meta("Read")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-1",
						"title": "Read README.md", "kind": "read",
						"rawInput": map[string]any{"file_path": "/work/README.md"}, "content": []any{}, "_meta": meta("Read")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-1", "_meta": meta("Read")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-1", "status": "completed",
						"rawOutput": "1\t# hello\n", "_meta": meta("Read"),
						"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "# hello"}}}}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t-2",
						"title": "Terminal", "kind": "execute", "status": "pending",
						"rawInput": map[string]any{}, "content": []any{}, "_meta": meta("Bash")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-2",
						"title": "ls hack", "kind": "execute",
						"rawInput": map[string]any{"command": "ls hack", "description": "List the harness"},
						"content":  []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "List the harness"}}},
						"_meta":    meta("Bash")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-2",
						"title": "ls hack", "kind": "execute", "rawInput": map[string]any{"command": "ls hack"}, "_meta": meta("Bash")}})
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t-2", "status": "completed",
						"rawOutput": "box.yaml\n", "_meta": meta("Bash"),
						"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "box.yaml"}}}}})
				// And a chunk, whose content is the object: still the text.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "DONE"}}})
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "metadenied":
				// A DENIED tool that names itself only in the metadata. This
				// used to walk straight through: no title meant nothing to
				// match, so the enforcement path skipped it entirely.
				f.notify("session/update", map[string]any{"sessionId": sessionID,
					"update": map[string]any{"sessionUpdate": "tool_call_update",
						"toolCallId": "t-1", "status": "in_progress",
						"_meta": map[string]any{"claudeCode": map[string]any{"toolName": "DeleteProd"}}}})
				time.Sleep(5 * time.Second) // only the client killing us ends this
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "nameless":
				// Neither a title nor a toolName: genuinely unmatchable, and
				// the run must say how many times that happened.
				for i, id2 := range []string{"t-1", "t-2", "t-3"} {
					_ = i
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "tool_call_update",
							"toolCallId": id2, "status": "in_progress"}})
				}
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "rogue":
				// Execute a denied tool without asking, then dawdle: only the
				// client killing us ends this session.
				f.notify("session/update", map[string]any{
					"sessionId": sessionID,
					"update":    map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t-1", "title": "DeleteProd", "kind": "execute", "status": "in_progress"},
				})
				time.Sleep(time.Minute)
			}
		}
	}
}

func gitc(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=fake-agent", "GIT_AUTHOR_EMAIL=agent@invalid",
		"GIT_COMMITTER_NAME=fake-agent", "GIT_COMMITTER_EMAIL=agent@invalid")
	cmd.Run()
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// TestConfiningWrapperGetsBothGrantedPaths: runner confinement is a wrapper
// around the agent (issue #6), and every wrapper worth having must be told
// which directories to let through. Both are chosen per run, so the argv
// cannot name them — it names placeholders and the runtime fills them in.
//
// The context directory is half of it. A wrapper handed only the workspace
// confines the agent out of its own granted reading material, which would
// make context sets and confinement mutually exclusive.
func TestConfiningWrapperGetsBothGrantedPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "polite")
	rt.ContextDir = t.TempDir()
	// A stand-in for the confinement: handed both paths the way `docker run
	// --mount` would be, and unable to start if either placeholder never
	// became a path.
	rt.Command = []string{"/bin/sh", "-c",
		`test -d "` + confine.ContextPlaceholder + `" && cd "` + confine.WorkspacePlaceholder + `" && exec "$0"`,
		os.Args[0]}
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run through a wrapper: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("the wrapped agent did not complete: %q", res.StopReason)
	}
}

// TestConfiningWrapperIsRefusedRatherThanEmptied: an argv that names the
// context placeholder for a run that has no context directory must fail, not
// quietly substitute nothing. A mount source of "" is not a narrower
// confinement than the one that was configured.
func TestConfiningWrapperIsRefusedRatherThanEmptied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "polite")
	rt.ContextDir = ""
	rt.Command = []string{"/bin/sh", "-c", `cd "` + confine.ContextPlaceholder + `" && exec "$0"`, os.Args[0]}
	if _, err := rt.Run(ctx, bundle(), "do the work"); err == nil {
		t.Fatal("an unresolvable context placeholder ran anyway")
	}
}
