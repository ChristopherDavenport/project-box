package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	for {
		m := f.read()
		id := m["id"]
		switch m["method"] {
		case "initialize":
			v := 1
			if scenario == "dialect" {
				v = 2 // an agent from a protocol this runtime has not read
			}
			f.write(map[string]any{"id": id, "result": map[string]any{"protocolVersion": v}})
		case "session/new":
			sessionID = "sess-1"
			f.write(map[string]any{"id": id, "result": map[string]any{"sessionId": sessionID}})
		case "session/set_mode":
			if p, ok := m["params"].(map[string]any); ok {
				mode, _ = p["modeId"].(string)
			}
			f.write(map[string]any{"id": id, "result": map[string]any{}})
		case "session/prompt":
			var report string
			switch scenario {
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
			case "escalate":
				resp := f.request("_box/escalate", map[string]any{"reason": "task needs credentials I do not hold"})
				if res, ok := resp["result"].(map[string]any); !ok || res["acknowledged"] != true {
					f.notify("session/update", map[string]any{"sessionId": sessionID,
						"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "escalation refused?!"}}})
				}
				f.write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case "worker":
				// A real day's work: edit, commit, hand off.
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
