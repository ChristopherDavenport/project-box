package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Client is the typed Go client for the daemon API — what cmd/box uses, and
// what a Go UX or a delegating orchestrator embeds. Other languages speak
// the same JSON over the same two transports.
type Client struct {
	http  *http.Client
	base  string
	token string
	actor string
}

// Dial connects to "unix:/path" or "tcp:host:port" (http). Token may be
// empty for unix sockets.
func Dial(spec, token, actor string) (*Client, error) {
	switch {
	case strings.HasPrefix(spec, "unix:"):
		path := strings.TrimPrefix(spec, "unix:")
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}
		return &Client{http: &http.Client{Transport: tr}, base: "http://boxd", token: token, actor: actor}, nil
	case strings.HasPrefix(spec, "tcp:"):
		return &Client{http: &http.Client{}, base: "http://" + strings.TrimPrefix(spec, "tcp:"), token: token, actor: actor}, nil
	default:
		return nil, fmt.Errorf("api: dial spec %q — want unix:/path or tcp:host:port", spec)
	}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.actor != "" {
		req.Header.Set("X-Box-Actor", c.actor)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return &Error{Method: method, Path: path, Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Status fetches /v1/status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.do(ctx, "GET", "/v1/status", nil, &s)
}

// Exceptions lists parked items, loudest and oldest first, with the age and
// escalation count the raw work item cannot carry.
func (c *Client) Exceptions(ctx context.Context) ([]control.Exception, error) {
	var out []control.Exception
	return out, c.do(ctx, "GET", "/v1/exceptions", nil, &out)
}

// Items lists the work queue, narrowed to the given states or unfiltered when
// none are given.
func (c *Client) Items(ctx context.Context, states ...string) ([]control.QueueEntry, error) {
	path := "/v1/items"
	if len(states) > 0 {
		path += "?state=" + url.QueryEscape(strings.Join(states, ","))
	}
	var out []control.QueueEntry
	return out, c.do(ctx, "GET", path, nil, &out)
}

// Item fetches one item's state.
func (c *Client) Item(ctx context.Context, id string) (queue.WorkItem, error) {
	var out queue.WorkItem
	return out, c.do(ctx, "GET", "/v1/items/"+id, nil, &out)
}

// Sessions is the per-unit live view: one entry per running unit.
func (c *Client) Sessions(ctx context.Context) ([]control.Session, error) {
	var out []control.Session
	return out, c.do(ctx, "GET", "/v1/sessions", nil, &out)
}

// Session is one unit's card — identity, progress, and what its agent is
// saying — whatever state the unit is in.
func (c *Client) Session(ctx context.Context, id string) (control.Session, error) {
	var out control.Session
	return out, c.do(ctx, "GET", "/v1/items/"+id+"/session", nil, &out)
}

// Provenance is the audit query over the wire.
func (c *Client) Provenance(ctx context.Context, sha string) ([]evidence.Record, error) {
	var out []evidence.Record
	return out, c.do(ctx, "GET", "/v1/provenance/"+sha, nil, &out)
}

// OpenIngress is the delegation verb.
func (c *Client) OpenIngress(ctx context.Context, route, key, payload string) (OpenIngressResponse, error) {
	var out OpenIngressResponse
	return out, c.do(ctx, "POST", "/v1/ingress", OpenIngressRequest{Route: route, Key: key, Payload: payload}, &out)
}

// Stop presses the stop button; Resume lifts it.
func (c *Client) Stop(ctx context.Context, reason string) error {
	return c.do(ctx, "POST", "/v1/stop", ControlRequest{Reason: reason}, nil)
}

// Resume lifts a stop.
func (c *Client) Resume(ctx context.Context, reason string) error {
	return c.do(ctx, "POST", "/v1/resume", ControlRequest{Reason: reason}, nil)
}

// Evidence is an item's whole chain — the raw material behind every rendered
// view, and the only place the agent's prompt and its own account of the run
// can be read back.
func (c *Client) Evidence(ctx context.Context, id string) ([]evidence.Record, error) {
	var out []evidence.Record
	return out, c.do(ctx, "GET", "/v1/items/"+id+"/evidence", nil, &out)
}

// Reopen takes an item off the exception queue: verb is "resolve" (closed by
// hand, no landing) or "retry" (back into the queue for another attempt).
func (c *Client) Reopen(ctx context.Context, verb, id, reason string) (queue.WorkItem, error) {
	var out queue.WorkItem
	return out, c.do(ctx, "POST", "/v1/items/"+id+"/"+verb, ControlRequest{Reason: reason}, &out)
}

// Reload asks the daemon to re-read its configuration.
func (c *Client) Reload(ctx context.Context) (string, error) {
	var out map[string]string
	if err := c.do(ctx, "POST", "/v1/reload", nil, &out); err != nil {
		return "", err
	}
	return out["reloaded"], nil
}

// Activity fetches the recent composite feed.
func (c *Client) Activity(ctx context.Context) ([]Activity, error) {
	var out []Activity
	return out, c.do(ctx, "GET", "/v1/activity", nil, &out)
}

// Error is the typed API failure: match on Code, never on Message — the
// prose is for people and may change; the code is the contract.
type Error struct {
	Method, Path  string
	Status        int
	Code, Message string
}

// Error renders for humans.
func (e *Error) Error() string {
	return fmt.Sprintf("api: %s %s: %d %s (%s)", e.Method, e.Path, e.Status, e.Message, e.Code)
}
