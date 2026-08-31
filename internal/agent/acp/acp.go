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
	"sync"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// ErrPolicyViolation reports that a denied tool appeared on the stream as
// anything other than a refusable permission request. The session is dead by
// the time this returns; a runtime that let the call finish first would be
// documenting violations, not preventing them.
var ErrPolicyViolation = errors.New("acp: denied tool appeared on the stream — session killed")

// Runtime runs one agent process per call.
type Runtime struct {
	// Command is the agent argv, e.g. {"claude-agent-acp"}. The permission
	// mode the resolver chose is applied through the protocol's
	// session/set_mode after session/new — no flags, and fail-closed: an
	// agent that refuses the mode does not get a prompt.
	Command []string
	// Dir is the working directory for the agent process: the workspace.
	Dir string
	// Env is appended to the inherited environment.
	Env []string
}

// Result is what a completed run reports.
type Result struct {
	StopReason string
	// Text accumulates the agent's message chunks.
	Text string
	// ToolCalls lists every tool title that appeared on the stream, permitted
	// or not, in order — the raw material for the run's telemetry.
	ToolCalls []string
	// Denied lists permission requests refused by policy.
	Denied []string
	// Escalated carries the agent's own reason for declining or escalating
	// the task — the "_box/escalate" extension verb. Refusal is a recorded
	// act, not a contrived failure: the caller parks the work with this
	// reason attributed to the agent.
	Escalated string
}

// Run executes one prompt under a bundle's policy and blocks until the agent
// finishes, the context ends, or policy kills the session.
func (rt *Runtime) Run(ctx context.Context, b agent.Bundle, prompt string) (Result, error) {
	if len(rt.Command) == 0 {
		return Result{}, errors.New("acp: no agent command configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	argv := append([]string{}, rt.Command...)
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
		mu       sync.Mutex
		res      Result
		violated error
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
			switch u.Update.SessionUpdate {
			case "tool_call":
				mu.Lock()
				res.ToolCalls = append(res.ToolCalls, u.Update.Title)
				mu.Unlock()
				if ok, _ := b.Tools.Permits(u.Update.Title); !ok {
					telemetry.ToolCall(ctx, u.Update.Title, false)
					// The agent is executing a denied tool without asking.
					mu.Lock()
					violated = fmt.Errorf("%w: %q", ErrPolicyViolation, u.Update.Title)
					mu.Unlock()
					cancel() // kills the process via CommandContext
				}
			case "agent_message_chunk":
				mu.Lock()
				res.Text += u.Update.Content.Text
				mu.Unlock()
			}
			return nil, nil // notification
		default:
			return nil, fmt.Errorf("acp: unhandled agent request %q", method)
		}
	})
	go conn.serve()

	var initRes json.RawMessage
	if err := conn.call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
	}, &initRes); err != nil {
		return Result{}, fmt.Errorf("acp: initialize: %w", err)
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
		StopReason string `json:"stopReason"`
	}
	err = conn.call(ctx, "session/prompt", map[string]any{
		"sessionId": newRes.SessionID,
		"prompt":    []map[string]any{{"type": "text", "text": prompt}},
	}, &promptRes)

	mu.Lock()
	defer mu.Unlock()
	if violated != nil {
		return res, violated
	}
	if err != nil {
		return res, fmt.Errorf("acp: prompt: %w", err)
	}
	res.StopReason = promptRes.StopReason
	return res, nil
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
		Title         string `json:"title"`
		Content       struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"update"`
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
