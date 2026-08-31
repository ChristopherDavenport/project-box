package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
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
	mode := ""
	for i, a := range os.Args {
		if a == "--permission-mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	var sessionID string
	for {
		m := f.read()
		id := m["id"]
		switch m["method"] {
		case "initialize":
			f.write(map[string]any{"id": id, "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			sessionID = "sess-1"
			f.write(map[string]any{"id": id, "result": map[string]any{"sessionId": sessionID}})
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

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
