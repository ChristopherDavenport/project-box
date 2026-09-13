package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// claimed puts an item in the state a session is defined over, without
// running it: these tests are about what the surface SAYS about a run, and a
// real run would settle before anything could look at it.
func claimed(t *testing.T, w *world, id string) queue.WorkItem {
	t.Helper()
	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	item, err := q.Submit(context.Background(), queue.WorkItem{
		ID: id, Tenant: tn, Box: "shop", Repo: "app", Ingress: "ing-1", Task: "ship the widget",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err = q.Claim(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestSessionsAreOnePerRunningUnit: the fan-out endpoint. Two units in flight
// are two entries with two timelines, because the operator's question is
// whether THIS unit is moving and a merged feed cannot answer it.
func TestSessionsAreOnePerRunningUnit(t *testing.T) {
	w := rig(t)
	w.srv.Live = &control.Live{}
	ctx := context.Background()

	claimed(t, w, "wi-a")
	claimed(t, w, "wi-b")
	w.srv.Live.Append("wi-a", control.Frame{Kind: "started"})
	w.srv.Live.Append("wi-b", control.Frame{Kind: "started"})
	w.srv.Live.Append("wi-a", control.Frame{Kind: "tool", Tool: "Read"})

	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := c.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want one per running unit", len(sessions))
	}
	by := map[string]control.Session{}
	for _, s := range sessions {
		by[s.ID] = s
	}
	if got := len(by["wi-a"].Timeline); got != 2 {
		t.Errorf("wi-a timeline = %d frames, want 2", got)
	}
	if got := len(by["wi-b"].Timeline); got != 1 {
		t.Errorf("wi-b timeline = %d frames, want 1 — one card is carrying another's work", got)
	}

	one, err := c.Session(ctx, "wi-a")
	if err != nil {
		t.Fatal(err)
	}
	if !one.Watching || one.State != queue.StateClaimed || one.Task != "ship the widget" {
		t.Errorf("single session = %+v", one.QueueEntry)
	}
}

// TestEventsScopeToOneItem is the per-item event scope the live card needs.
//
// Not a filter the client applies: on a busy daemon the whole factory's feed
// arrives so that a page can throw almost all of it away. Scoping stops the
// server reading what nobody asked for, and it is what keeps one card's
// stream readable while five units are running.
func TestEventsScopeToOneItem(t *testing.T) {
	w := rig(t)
	w.srv.Live = &control.Live{}
	claimed(t, w, "wi-watched")
	claimed(t, w, "wi-other")

	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/events?item=wi-watched", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && !strings.HasPrefix(sc.Text(), "event: hello") {
	}

	// Both units say something; only one of them is this subscriber's.
	w.srv.Live.Append("wi-other", control.Frame{Kind: "tool", Tool: "SomebodyElsesTool"})
	w.srv.Live.Append("wi-watched", control.Frame{Kind: "tool", Tool: "TheWatchedTool"})

	deadline := time.Now().Add(15 * time.Second)
	saw := false
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if strings.Contains(line, "SomebodyElsesTool") {
			t.Fatalf("a scoped subscriber received another unit's frame: %s", line)
		}
		if strings.Contains(line, "TheWatchedTool") {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatal("the scoped subscriber never saw its own unit's frame")
	}
	// And the whole factory's activity is NOT on this connection: an ingress
	// opened now belongs to no item and must not arrive here.
	c, _ := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if _, err := c.OpenIngress(ctx, "issues", "issue-elsewhere", "unrelated", ""); err != nil {
		t.Fatal(err)
	}
	w.srv.Live.Append("wi-watched", control.Frame{Kind: "message", Text: "still here"})
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		if strings.Contains(line, "index/ingress") {
			t.Fatalf("a scoped subscriber received the factory's activity feed: %s", line)
		}
		if strings.Contains(line, "still here") {
			return
		}
	}
	t.Fatal("the scoped subscriber stopped receiving its own unit")
}

// TestSessionsWithoutALiveBus: a daemon nothing reports to answers truthfully
// rather than erroring — identity and progress, an empty timeline, and
// `watching` false so nobody reads the silence as an idle agent.
func TestSessionsWithoutALiveBus(t *testing.T) {
	w := rig(t)
	claimed(t, w, "wi-quiet")
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	c, _ := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")

	sessions, err := c.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Watching || len(sessions[0].Timeline) != 0 {
		t.Fatalf("sessions = %+v", sessions)
	}
}

// TestAgentSessionsEndpointListsOneUnitsAttempts is issue #107's api half:
// GET /v1/items/{id}/sessions answers just the AgentSession rows for one
// unit, the same rows Session's own AgentSessions field carries — and an id
// with no agent-run records answers an empty list, matching runFrames'
// doctrine, rather than a 404.
func TestAgentSessionsEndpointListsOneUnitsAttempts(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	claimed(t, w, "wi-attempts")

	body, err := json.Marshal(map[string]any{
		"agentref": "ref-1", "role": "builder", "mode": "acceptEdits",
		"seat": "mason", "envelope": "(none declared)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.srv.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "workitem/wi-attempts", Kind: "agent-run",
		Subject: "ref-1", Actor: "mason", Body: body,
	}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := c.AgentSessions(ctx, "wi-attempts")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AgentRef != "ref-1" || rows[0].Role != "builder" {
		t.Fatalf("agent sessions = %+v", rows)
	}

	empty, err := c.AgentSessions(ctx, "wi-nothing-ever-ran")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("agent sessions for an id with no agent-run records = %+v, want empty", empty)
	}
}
