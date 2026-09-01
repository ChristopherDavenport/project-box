// Package acp drives an agent process over the Agent Client Protocol —
// newline-delimited JSON-RPC 2.0 on stdio — and is the enforcement point for
// tool policy.
//
// The stream is the only place policy can be enforced without patching the
// agent: every tool call either asks permission here (and is answered from
// [agent.ToolPolicy], not by a human) or at least announces itself as a
// session update. A denied tool that ASKS is refused; a denied tool that
// APPEARS anyway — an agent running in a mode that never asks — kills the
// session. Advisory enforcement that logs and carries on would be no
// enforcement at all.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// ErrPolicyViolation reports that a denied tool appeared on the stream as
// anything other than a refusable permission request. The session is dead by
// the time this returns; a runtime that let the call finish first would be
// documenting violations, not preventing them.
var ErrPolicyViolation = errors.New("acp: denied tool appeared on the stream — session killed")

// ErrProtocolDialect reports that the agent answered initialize with a
// protocol version this runtime does not speak.
//
// Fail closed, because the failure mode of guessing is silent. A dialect
// drifted once already: v0.70's update shapes did not match the tool_call
// form this runtime counts, so tool-call metrics read 0 across all eight runs
// of the continuity experiment and the only reason anyone knew was a human
// reading a column of zeros. The same shapes carry policy enforcement, so a
// dialect this runtime cannot parse is not merely an undercount — it is a
// stream whose denied tools may never be recognised at all.
var ErrProtocolDialect = errors.New("acp: agent speaks a protocol version this runtime does not")

// ErrTokenCeiling reports that a single run spent past its per-run bound and
// was killed at the stream.
//
// The window budget could not do this. [capacity.Meter.Acquire] is a gate on
// STARTING a run: it asks whether the window has room and then has no further
// say, so the ceiling it enforces is really "the limit, plus whatever one run
// can spend before it stops on its own". Measured, that was not a rounding
// error — a 6,000,000-token window was carried to 25,419,130 by one session
// that ran 110 tool calls for 21 minutes and cost $13.60. It broke no rule;
// there was no rule to break.
//
// So the bound belongs where the spend is visible as it happens, which is the
// same place tool policy is enforced and for the same reason: the stream is
// the only vantage point that exists while the run is still running.
var ErrTokenCeiling = errors.New("acp: run exceeded its token ceiling — session killed")

// protocolVersion is the dialect this runtime is written against, offered at
// initialize and required back. Bump it only together with the update shapes
// below.
const protocolVersion = 1

// knownUpdates is every session update kind this runtime understands. The set
// is explicit so that an unknown kind is a fact rather than a default branch:
// see [Result.UnknownUpdates].
var knownUpdates = []string{
	"tool_call", "tool_call_update", "agent_message_chunk", "agent_thought_chunk",
	"user_message_chunk", "plan", "available_commands_update", "current_mode_update",
	"usage_update",
}

// Usage is what a run cost, as the agent reported it.
//
// Reported is the load-bearing field. An agent that says nothing about cost
// has not run for free, and a budget that treats silence as zero is the
// tool-count bug again with money attached — so absence is carried
// explicitly rather than collapsing into a zero total.
type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedReadTokens  int64
	CachedWriteTokens int64
	// Tokens is the agent's own total when it gave one.
	Tokens int64
	// CostUSD is what the agent says the turn cost in money, from the
	// usage_update notification. Cumulative for the turn, not a delta, so it
	// is taken rather than summed. Zero means the agent never said.
	CostUSD float64
	// Reported is false when nothing on the stream said what the run cost.
	Reported bool
}

// Total is every token the run is billed for, cache reads included — cheaper,
// never free. The agent's own tally wins when it gave one: a sum of the parts
// this runtime happens to recognise is the weaker claim of the two.
func (u Usage) Total() int64 {
	if u.Tokens > 0 {
		return u.Tokens
	}
	return u.InputTokens + u.OutputTokens + u.CachedReadTokens + u.CachedWriteTokens
}

func (u *Usage) add(w wireUsage) {
	u.InputTokens += w.InputTokens
	u.OutputTokens += w.OutputTokens
	u.CachedReadTokens += w.CachedReadTokens
	u.CachedWriteTokens += w.CachedWriteTokens
	u.Tokens += w.TotalTokens
	u.Reported = true
}

// Runtime runs one agent process per call.
type Runtime struct {
	// Command is the agent argv, e.g. {"claude-agent-acp"}, optionally with a
	// confining wrapper in front of it; [confine.WorkspacePlaceholder] and
	// [confine.ContextPlaceholder] are substituted throughout. The permission
	// mode the resolver chose is applied through the protocol's
	// session/set_mode after session/new — no flags, and fail-closed: an agent
	// that refuses the mode does not get a prompt.
	Command []string
	// Dir is the working directory for the agent process: the workspace.
	Dir string
	// ContextDir is the run's materialized context set, which a confining
	// wrapper must be told about or it confines the agent out of its own
	// granted reading material. Empty is only valid for an argv that never
	// names the placeholder; [confine.Substitute] refuses the other case
	// rather than mounting nothing under a name that promised something.
	ContextDir string
	// Env is appended to the inherited environment.
	Env []string
	// MaxTokens bounds what ONE run may spend before the session is killed;
	// zero means unbounded. See [ErrTokenCeiling] for why the window budget
	// cannot express this.
	//
	// Enforced against the stream's running total, so it binds only as
	// tightly as the agent reports: an adapter that says nothing until the
	// end cannot be stopped in the middle by anything short of a clock, which
	// is what build_timeout is for.
	MaxTokens int64
}

// Result is what a completed run reports.
type Result struct {
	StopReason string
	// Text accumulates the agent's message chunks.
	Text string
	// ToolCalls names every tool call that appeared on the stream, permitted
	// or not, in order — the harness's stable tool name where the dialect
	// gives one, the human title otherwise. See [sessionUpdate.tool] for why
	// that preference matters to more than tidiness.
	ToolCalls []string
	// Unenforced counts tool calls that arrived with no name at all, and so
	// were never matched against policy.
	//
	// A number rather than a flag, because the flag was already there and
	// said nothing useful: "some calls could not be checked" is a caveat an
	// operator scrolls past, and "91 of 232 could not be" is the reason to
	// stop and fix the dialect. Non-zero means enforcement had a hole of
	// exactly this size.
	Unenforced int
	// Denied lists permission requests refused by policy.
	Denied []string
	// Escalated carries the agent's own reason for declining or escalating
	// the task — the "_box/escalate" extension verb. Refusal is a recorded
	// act, not a contrived failure: the caller parks the work with this
	// reason attributed to the agent.
	Escalated string
	// Usage is what the run cost, when the agent said. Silence is carried as
	// Usage.Reported false, never as a zero total.
	Usage Usage
	// UnknownUpdates lists session update kinds this runtime does not
	// understand, deduped in first-seen order, plus "tool_call:untitled" for
	// a tool call that arrived with no title for policy to match on.
	//
	// This is the drift alarm. A dialect that moves must be visible on the
	// record while it is still cheap to notice, rather than inferred later
	// from a metric that quietly read zero.
	UnknownUpdates []string
}

// Run executes one prompt under a bundle's policy and blocks until the agent
// finishes, the context ends, or policy kills the session.
func (rt *Runtime) Run(ctx context.Context, b agent.Bundle, prompt string) (Result, error) {
	if len(rt.Command) == 0 {
		return Result{}, errors.New("acp: no agent command configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	argv, err := confine.Substitute(rt.Command, rt.Dir, rt.ContextDir)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = rt.Dir
	cmd.Env = append(os.Environ(), rt.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("acp: start agent: %w", err)
	}
	defer func() {
		stdin.Close()
		cmd.Wait()
	}()

	var (
		mu        sync.Mutex
		res       Result
		violated  error
		overspent error
		// streamed accumulates per-update usage. The prompt result wins when
		// it carries a total of its own; summing both would double-count.
		streamed Usage
		// seenCalls dedupes a tool call announced as both tool_call and
		// tool_call_update. Keying on the call id rather than the shape is
		// what keeps the count right when a dialect prefers one spelling.
		seenCalls = map[string]bool{}
	)
	conn := newConn(stdin, stdout, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session/request_permission":
			var p permissionRequest
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			ok, reason := b.Tools.Permits(p.ToolCall.Title)
			telemetry.ToolCall(ctx, p.ToolCall.Title, ok)
			mu.Lock()
			if !ok {
				res.Denied = append(res.Denied, p.ToolCall.Title)
			}
			mu.Unlock()
			return permissionResponse(p, ok, reason), nil
		case "_box/escalate":
			// Extension verb: the agent declines or escalates on its own
			// judgement. Acknowledged always — a refusal channel that could
			// itself be refused would teach agents not to use it.
			var p struct {
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			mu.Lock()
			res.Escalated = p.Reason
			mu.Unlock()
			return map[string]any{"acknowledged": true}, nil
		case "session/update":
			var u sessionUpdate
			if err := json.Unmarshal(params, &u); err != nil {
				return nil, err
			}
			if w := u.usage(); w != nil {
				mu.Lock()
				streamed.add(*w)
				over := rt.MaxTokens > 0 && overspent == nil && streamed.Total() >= rt.MaxTokens
				if over {
					overspent = fmt.Errorf("%w: %d tokens against a ceiling of %d",
						ErrTokenCeiling, streamed.Total(), rt.MaxTokens)
				}
				mu.Unlock()
				if over {
					cancel() // kills the process via CommandContext
				}
			}
			if c := u.Update.Cost; c != nil && c.Currency == "USD" {
				// Cumulative for the turn, so the last word wins; summing
				// would bill every notification over again.
				mu.Lock()
				res.Usage.CostUSD = c.Amount
				mu.Unlock()
			}
			switch u.Update.SessionUpdate {
			case "tool_call", "tool_call_update":
				// Both spellings announce the same call, so the id decides
				// whether it is new. A dialect that only ever sends the update
				// form still gets counted, and still gets judged.
				name := u.tool()
				key := u.Update.ToolCallID
				if key == "" {
					key = name
				}
				mu.Lock()
				first := !seenCalls[key]
				seenCalls[key] = true
				if first {
					res.ToolCalls = append(res.ToolCalls, name)
					if name == "" {
						// Nothing to match on, so enforcement did not happen
						// here and must not be reported as if it had. Counted,
						// not merely noted: "some calls were unchecked" is a
						// caveat, "91 of 232 were" is a finding.
						res.Unenforced++
						res.noteUnknown("tool_call:unnamed")
					}
				}
				mu.Unlock()
				if !first || name == "" {
					break
				}
				if ok, _ := b.Tools.Permits(name); !ok {
					telemetry.ToolCall(ctx, name, false)
					// The agent is executing a denied tool without asking.
					mu.Lock()
					violated = fmt.Errorf("%w: %q", ErrPolicyViolation, name)
					mu.Unlock()
					cancel() // kills the process via CommandContext
				}
			case "agent_message_chunk":
				mu.Lock()
				res.Text += u.Update.Content.Text
				mu.Unlock()
			default:
				if !slices.Contains(knownUpdates, u.Update.SessionUpdate) {
					mu.Lock()
					res.noteUnknown(u.Update.SessionUpdate)
					mu.Unlock()
				}
			}
			return nil, nil // notification
		default:
			return nil, fmt.Errorf("acp: unhandled agent request %q", method)
		}
	})
	go conn.serve()

	var initRes struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := conn.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
	}, &initRes); err != nil {
		return Result{}, fmt.Errorf("acp: initialize: %w", err)
	}
	// Assert the dialect before spending anything. A version we do not speak
	// is caught here, for free, rather than as a metric that reads zero and an
	// enforcement layer that may not be enforcing.
	if initRes.ProtocolVersion != protocolVersion {
		return Result{}, fmt.Errorf("%w: offered %d, agent answered %d",
			ErrProtocolDialect, protocolVersion, initRes.ProtocolVersion)
	}
	var newRes struct {
		SessionID string `json:"sessionId"`
	}
	if err := conn.call(ctx, "session/new", map[string]any{
		"cwd": rt.Dir, "mcpServers": []any{},
	}, &newRes); err != nil {
		return Result{}, fmt.Errorf("acp: session/new: %w", err)
	}
	if mode := b.Tools.PermissionMode; mode != "" {
		if err := conn.call(ctx, "session/set_mode", map[string]any{
			"sessionId": newRes.SessionID, "modeId": mode,
		}, nil); err != nil {
			// Fail closed: running under the wrong permission mode is the
			// harness-default trap this whole layer exists to prevent.
			return Result{}, fmt.Errorf("acp: agent refused permission mode %q: %w", mode, err)
		}
	}
	var promptRes struct {
		StopReason string     `json:"stopReason"`
		Usage      *wireUsage `json:"usage,omitempty"`
		Meta       *wireMeta  `json:"_meta,omitempty"`
	}
	err = conn.call(ctx, "session/prompt", map[string]any{
		"sessionId": newRes.SessionID,
		"prompt":    []map[string]any{{"type": "text", "text": prompt}},
	}, &promptRes)

	mu.Lock()
	defer mu.Unlock()
	// The turn's own total wins when it has one; otherwise the stream's
	// increments are the best account there is. Summing both would bill the
	// run twice.
	cost := res.Usage.CostUSD // gathered from the stream; survives whichever branch wins
	switch {
	case promptRes.Usage != nil:
		res.Usage.add(*promptRes.Usage)
		if promptRes.Usage.empty() {
			// A usage object that parses to nothing is what a rename looks
			// like from here, and it is indistinguishable from a free turn
			// unless somebody says so.
			res.noteUnknown("usage:all-zero")
		}
	case promptRes.Meta != nil && promptRes.Meta.Usage != nil:
		res.Usage.add(*promptRes.Meta.Usage)
	default:
		res.Usage = streamed
	}
	res.Usage.CostUSD = cost
	if violated != nil {
		return res, violated
	}
	// Before the generic prompt error, which on this path is only ever the
	// kill reporting itself. The usage rides the Result either way: a run
	// stopped for spending too much is the last run whose cost may go
	// unrecorded.
	if overspent != nil {
		return res, overspent
	}
	if err != nil {
		return res, fmt.Errorf("acp: prompt: %w", err)
	}
	res.StopReason = promptRes.StopReason
	return res, nil
}

// noteUnknown records a dialect surprise once. Callers hold mu.
func (r *Result) noteUnknown(kind string) {
	if kind == "" {
		kind = "(empty)"
	}
	if !slices.Contains(r.UnknownUpdates, kind) {
		r.UnknownUpdates = append(r.UnknownUpdates, kind)
	}
}

type toolCall struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
}

type permissionRequest struct {
	SessionID string   `json:"sessionId"`
	ToolCall  toolCall `json:"toolCall"`
	Options   []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// permissionResponse picks the agent's own allow/reject option matching the
// policy decision, falling back to a cancelled outcome when the agent offered
// nothing suitable — never to an allow.
func permissionResponse(p permissionRequest, allow bool, reason string) any {
	want := "reject"
	if allow {
		want = "allow"
	}
	for _, o := range p.Options {
		if len(o.Kind) >= len(want) && o.Kind[:len(want)] == want {
			return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": o.OptionID}}
		}
	}
	return map[string]any{"outcome": map[string]any{"outcome": "cancelled", "reason": reason}}
}

type sessionUpdate struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		SessionUpdate string `json:"sessionUpdate"`
		ToolCallID    string `json:"toolCallId"`
		Title         string `json:"title"`
		Content       struct {
			Text string `json:"text"`
		} `json:"content"`
		// Cost rides usage_update, and is the only place the agent says what
		// a turn cost in money rather than tokens. Cumulative for the turn.
		Cost *struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"cost,omitempty"`
		Meta *wireMeta `json:"_meta,omitempty"`
	} `json:"update"`
	Meta *wireMeta `json:"_meta,omitempty"`
}

// tool names a call the way policy and the tally both want it named: the
// harness's stable tool name when the dialect supplies one, the human title
// otherwise, and empty when neither — which is a fact the caller must record
// rather than paper over, because it means nothing was matched.
func (u sessionUpdate) tool() string {
	for _, m := range []*wireMeta{u.Update.Meta, u.Meta} {
		if m != nil && m.ClaudeCode != nil && m.ClaudeCode.ToolName != "" {
			return m.ClaudeCode.ToolName
		}
	}
	return u.Update.Title
}

// usage finds a cost report on an update, at either of the two places the
// dialect may hang it. Both spellings are part of the pinned dialect: an
// agent that reports somewhere else reads as unmetered — loudly — rather
// than as free.
func (u sessionUpdate) usage() *wireUsage {
	if u.Update.Meta != nil && u.Update.Meta.Usage != nil {
		return u.Update.Meta.Usage
	}
	if u.Meta != nil && u.Meta.Usage != nil {
		return u.Meta.Usage
	}
	return nil
}

type wireMeta struct {
	Usage *wireUsage `json:"usage,omitempty"`
	// ClaudeCode carries the harness's own account of a tool call. toolName is
	// the STABLE identifier — "Bash", "Edit", "Read" — as against title, which
	// is prose written for a human to read and for Bash is the whole command
	// line.
	//
	// That distinction turned out to be the difference between a tool policy
	// and a decoration. Permits is an exact match, so a deny list naming
	// "Bash" never fired against a title of `cd /Users/... && find . -name
	// '*.go'`, and the tally keyed on the same string grew a new entry per
	// command — 46 "distinct tools" in one run of 53 calls. Meanwhile
	// tool_call_update carries no title at all, and the enforcement path
	// skipped those outright: measured across four real runs, 91 of 232 calls
	// went unmatched for want of anything to match on.
	ClaudeCode *struct {
		ToolName string `json:"toolName"`
	} `json:"claudeCode,omitempty"`
}

// wireUsage is the cost report as it appears on the wire, taken from the
// adapter's own source rather than guessed: claude-agent-acp's sessionUsage()
// returns exactly these five fields on the prompt response.
//
// The names matter more than they look. An earlier version of this struct
// spelled the cache counters cacheReadInputTokens / cacheCreationInputTokens —
// the Anthropic API's names, not the adapter's. Input and output parsed, the
// two cache figures silently read zero, and Reported still came back true. For
// an agent re-reading a repository the cache read is usually the largest of
// the four, so the meter would have shown a confident, plausible figure that
// was wrong by a wide margin, with nothing anywhere to say so — the exact
// failure the dialect assertions in this package exist to prevent, in the one
// place those assertions did not reach.
type wireUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	// TotalTokens is the adapter's own tally over the other four. Preferred
	// when present: it is what the agent believes it spent, and a sum of the
	// parts this runtime happens to recognise is a weaker claim.
	TotalTokens int64 `json:"totalTokens"`
}

// empty reports a usage object that parsed to nothing at all — the signature
// of the field names having moved.
func (w wireUsage) empty() bool {
	return w.InputTokens == 0 && w.OutputTokens == 0 &&
		w.CachedReadTokens == 0 && w.CachedWriteTokens == 0 && w.TotalTokens == 0
}

// conn is a minimal bidirectional JSON-RPC 2.0 endpoint over ndjson. Small on
// purpose: the protocol surface the runtime needs is three outbound calls and
// two inbound methods, and a dependency would not make the framing simpler.
type conn struct {
	w       io.Writer
	r       *bufio.Scanner
	handler func(method string, params json.RawMessage) (any, error)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResult
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

func newConn(w io.Writer, r io.Reader, handler func(string, json.RawMessage) (any, error)) *conn {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	return &conn{w: w, r: sc, handler: handler, pending: map[int64]chan rpcResult{}}
}

func (c *conn) send(m rpcMessage) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

func (c *conn) call(ctx context.Context, method string, params any, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(rpcMessage{ID: &id, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out != nil && r.result != nil {
			return json.Unmarshal(r.result, out)
		}
		return nil
	}
}

func (c *conn) serve() {
	for c.r.Scan() {
		line := c.r.Bytes()
		if len(line) == 0 {
			continue
		}
		var m rpcMessage
		if err := json.Unmarshal(line, &m); err != nil {
			continue // a malformed line is the agent's bug; keep serving
		}
		switch {
		case m.Method != "" && m.ID != nil: // request from the agent
			go func(m rpcMessage) {
				result, err := c.handler(m.Method, m.Params)
				resp := rpcMessage{ID: m.ID}
				if err != nil {
					resp.Error = &rpcError{Code: -32603, Message: err.Error()}
				} else {
					b, _ := json.Marshal(result)
					resp.Result = b
				}
				c.send(resp)
			}(m)
		case m.Method != "": // notification
			c.handler(m.Method, m.Params)
		case m.ID != nil: // response to one of our calls
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				var err error
				if m.Error != nil {
					err = m.Error
				}
				ch <- rpcResult{result: m.Result, err: err}
			}
		}
	}
	// EOF: fail every pending call so nothing blocks forever on a dead agent.
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcResult{err: io.ErrUnexpectedEOF}
		delete(c.pending, id)
	}
}
