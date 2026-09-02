package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// pinnedDialect is the transcript this runtime is written against. It is data,
// not code, so teaching the runtime a newer dialect is a reviewable diff of
// frames rather than an edit to a hand-written fake — which is the difference
// between a pin and a habit. See testdata/README.md for its provenance.
const pinnedDialect = "testdata/claude-agent-acp-0.70.jsonl"

// TestTheDialectIsPinned replays a recorded session at the runtime and asserts
// what the parser made of every shape in it.
//
// The scenario tests each probe one shape with a fake written beside them; this
// asserts a whole conversation at once, the client's own call sequence
// included — a runtime that stopped setting the permission mode, or reordered
// initialize and session/new, fails here rather than in production.
//
// Its subject is finding 5a: v0.70's update shapes did not match the tool_call
// form this runtime counted, so the tool-call metric read 0 for all eight runs
// of the continuity experiment and nothing anywhere said so. The counters are
// asserted against exact values AND, separately, asserted to be non-zero with
// the shape feeding each one named — because a metric that can silently read
// zero is the failure, not the particular wrong number.
func TestTheDialectIsPinned(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := runtimeFor(t, "replay:"+pinPath(t)).Run(ctx, bundle(), "add the widget package")
	if err != nil {
		t.Fatalf("the pinned dialect no longer replays: %v", err)
	}

	if res.StopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn — the prompt response shape moved", res.StopReason)
	}
	// Three calls, announced three ways: a tool_call and its tool_call_update
	// (one call, counted once), an update-only call named solely under
	// _meta.claudeCode.toolName, and a tool_call whose title is a filename.
	// The denied tool is a permission request, which is not a call.
	if want := []string{"Read", "Bash", "Edit"}; !slices.Equal(res.ToolCalls, want) {
		t.Errorf("tool calls = %v, want %v", res.ToolCalls, want)
	}
	if res.Unenforced != 0 {
		t.Errorf("Unenforced = %d: a call in the pinned dialect was never matched against policy", res.Unenforced)
	}
	if want := []string{"DeleteProd"}; !slices.Equal(res.Denied, want) {
		t.Errorf("denied = %v, want %v — the permission request shape moved", res.Denied, want)
	}
	if len(res.UnknownUpdates) != 0 {
		t.Errorf("the pinned dialect reads as drift: %v", res.UnknownUpdates)
	}
	if res.Escalated != "" {
		t.Errorf("escalated = %q; 0.70 cannot call unknown methods, so the pin carries no such frame", res.Escalated)
	}
	for _, want := range []string{"Reading the tree first.", "Added the widget package"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("agent_message_chunk %q was not accumulated:\n%s", want, res.Text)
		}
	}
	want := Usage{
		InputTokens: 1200, OutputTokens: 300, CachedReadTokens: 9000, CachedWriteTokens: 500,
		Tokens: 11000, CostUSD: 0.0432, Reported: true,
	}
	if res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}

	// The zero smell, made mechanical. Every one of these is a counter a
	// dialect change can drive to zero while the session still parses, exits
	// clean, and reads as a quiet run.
	for _, c := range []struct {
		metric string
		got    int64
		shape  string
	}{
		{"tool calls", int64(len(res.ToolCalls)), "tool_call / tool_call_update"},
		{"denials", int64(len(res.Denied)), "session/request_permission"},
		{"input tokens", res.Usage.InputTokens, "usage.inputTokens on the prompt response"},
		{"output tokens", res.Usage.OutputTokens, "usage.outputTokens on the prompt response"},
		{"cache reads", res.Usage.CachedReadTokens, "usage.cachedReadTokens on the prompt response"},
		{"cache writes", res.Usage.CachedWriteTokens, "usage.cachedWriteTokens on the prompt response"},
		{"total tokens", res.Usage.Total(), "usage.totalTokens on the prompt response"},
		{"message text", int64(len(res.Text)), "agent_message_chunk"},
	} {
		if c.got == 0 {
			t.Errorf("%s read 0 against the pinned dialect: %s no longer parses", c.metric, c.shape)
		}
	}
	if res.Usage.CostUSD == 0 {
		t.Error("cost read 0 against the pinned dialect: usage_update's cost object no longer parses")
	}
}

// TestTheRecorderProducesAReplayableTranscript closes the loop the pin rests
// on. Recording is how the dialect is re-pinned from a live agent, and a
// recorder that captures less than a run needs is a refresh procedure nobody
// can carry out. So: replay the pin while recording, replay the recording, and
// require the two runs to be indistinguishable.
func TestTheRecorderProducesAReplayableTranscript(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var tape bytes.Buffer
	rt := runtimeFor(t, "replay:"+pinPath(t))
	rt.Transcript = &tape
	first, err := rt.Run(ctx, bundle(), "add the widget package")
	if err != nil {
		t.Fatalf("recorded run: %v", err)
	}
	if tape.Len() == 0 {
		t.Fatal("the transcript is empty: nothing was recorded")
	}

	path := filepath.Join(t.TempDir(), "recorded.jsonl")
	if err := os.WriteFile(path, tape.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := runtimeFor(t, "replay:"+path).Run(ctx, bundle(), "add the widget package")
	if err != nil {
		t.Fatalf("the recording does not replay: %v\n%s", err, tape.String())
	}
	if fmt.Sprintf("%+v", first) != fmt.Sprintf("%+v", second) {
		t.Errorf("a run replayed from its own recording differs:\n first: %+v\nsecond: %+v", first, second)
	}
}

func pinPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(pinnedDialect)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// --- the replaying fake agent ---------------------------------------------

// tapeEntry is one frame as [conn.record] wrote it.
type tapeEntry struct {
	From  string          `json:"from"`
	Frame json.RawMessage `json:"frame,omitempty"`
	Raw   string          `json:"raw,omitempty"`
}

// replayTranscript plays a recorded transcript back at the runtime, frame for
// frame in the order the frames crossed the wire. It is the agent side of the
// fake-agent suite with its script supplied as data.
//
// One thing is rewritten and one thing is checked. Ids the CLIENT chose are
// remapped to the ids this run chose, because a response must answer the call
// that was actually made; everything else goes out as recorded, bytes and all,
// since a pin that reformats what it replays is asserting the reformatting.
// And each client frame in the transcript must be the call the client actually
// makes at that point, so the pin covers the conversation and not just the
// parsing.
func replayTranscript(f *fakeConn, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	live := map[float64]any{} // id the transcript recorded -> id this run used
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e tapeEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		switch e.From {
		case "client":
			want, id := frameMethod(e.Frame)
			got := f.read() // blocks; exits if the client hung up
			if want != "" && got["method"] != want {
				// The runtime's call sequence drifted from the recording.
				// Answered as an rpc error so the failure names itself rather
				// than surfacing as an EOF on a dead agent.
				f.write(map[string]any{"id": got["id"], "error": map[string]any{
					"code": -32600,
					"message": fmt.Sprintf("transcript %s: expected the client to call %q, got %q",
						filepath.Base(path), want, got["method"]),
				}})
				return
			}
			if want != "" {
				live[id] = got["id"]
			}
		case "agent":
			if e.Frame == nil {
				fmt.Println(e.Raw) // a frame that was not JSON, replayed as it was
				continue
			}
			f.enc.Encode(json.RawMessage(retarget(e.Frame, live)))
		}
	}
	// The recording is spent. Keep the process alive and reading, exactly as
	// the scripted scenarios do: the client's hang-up is what ends an agent.
	for {
		f.read()
	}
}

// frameMethod reports a frame's method — empty for a response — and its id.
func frameMethod(frame json.RawMessage) (string, float64) {
	var m struct {
		Method string  `json:"method"`
		ID     float64 `json:"id"`
	}
	json.Unmarshal(frame, &m)
	return m.Method, m.ID
}

// retarget rewrites a response's id to the one this run's client actually
// used. Requests the agent originates keep theirs: those ids are the agent's
// to choose, and the client only echoes them back.
func retarget(frame json.RawMessage, live map[float64]any) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(frame, &m) != nil {
		return frame
	}
	if _, isRequest := m["method"]; isRequest {
		return frame
	}
	_, id := frameMethod(frame)
	to, ok := live[id]
	if !ok {
		return frame
	}
	b, err := json.Marshal(to)
	if err != nil {
		return frame
	}
	m["id"] = b
	out, err := json.Marshal(m)
	if err != nil {
		return frame
	}
	return out
}
