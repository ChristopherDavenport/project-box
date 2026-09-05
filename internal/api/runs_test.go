package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
)

// TestTheRunLogIsReadableAfterTheRun: what the live view showed while a unit
// ran is answerable once it is over, by the run's id, oldest first. A run
// nothing was kept for answers with an empty list: "nothing kept" is an
// answer, and not the one the chain gives for a run that never was.
func TestTheRunLogIsReadableAfterTheRun(t *testing.T) {
	w := rig(t)
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	ctx := context.Background()
	rl := w.srv.Store.Runs()
	for _, f := range []runlog.Frame{
		{Run: "wi-x", Kind: "started", Text: "agentref"},
		{Run: "wi-x", Kind: "tool", Call: "t-1", Tool: "Read"},
		{Run: "wi-x", Kind: "tool", Call: "t-1", Tool: "Read", Text: "Read main.go"},
		{Run: "wi-other", Kind: "started"},
	} {
		if _, err := rl.Append(ctx, w.srv.Tenant, f); err != nil {
			t.Fatal(err)
		}
	}
	var got []runlog.Frame
	getJSON(t, ts.URL+"/v1/runs/wi-x/frames", &got)
	if len(got) != 3 || got[0].Kind != "started" || got[2].Text != "Read main.go" || got[2].Call != "t-1" {
		t.Fatalf("frames = %+v, want the three kept for wi-x in order", got)
	}
	var none []runlog.Frame
	getJSON(t, ts.URL+"/v1/runs/wi-none/frames", &none)
	if none == nil || len(none) != 0 {
		t.Fatalf("a run nothing was kept for = %v, want an empty list", none)
	}
}

// TestAWatcherArrivingMidRunGetsTheRunSoFar: an item's stream opens with what
// the bus already holds for that item, then follows. Before this a page
// opened two minutes into a run showed an agent that had done nothing until
// the page opened — and nothing again once the run was over.
func TestAWatcherArrivingMidRunGetsTheRunSoFar(t *testing.T) {
	w := rig(t)
	if w.srv.Live == nil {
		w.srv.Live = &control.Live{}
	}
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	w.srv.Live.Append("wi-x", control.Frame{Kind: "started", Text: "agentref"})
	w.srv.Live.Append("wi-x", control.Frame{Kind: "tool", Call: "t-1", Tool: "Read", Text: "Read main.go"})
	w.srv.Live.Append("wi-other", control.Frame{Kind: "started"})

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/events?item=wi-x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	var seen []control.Frame
	next := func() (string, bool) {
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				return strings.TrimPrefix(line, "data: "), true
			}
		}
		return "", false
	}
	if d, ok := next(); !ok || !strings.Contains(d, `"tenant"`) {
		t.Fatalf("no hello: %q", d)
	}
	for len(seen) < 2 {
		d, ok := next()
		if !ok {
			t.Fatalf("stream ended after %d replayed frames", len(seen))
		}
		var f control.Frame
		if err := json.Unmarshal([]byte(d), &f); err != nil {
			t.Fatalf("not a frame: %s", d)
		}
		seen = append(seen, f)
	}
	if seen[0].Kind != "started" || seen[1].Call != "t-1" || seen[1].Text != "Read main.go" {
		t.Fatalf("replayed = %+v, want the item's two frames in order", seen)
	}
	for _, f := range seen {
		if f.Item != "wi-x" {
			t.Fatalf("another item's frame leaked into the replay: %+v", f)
		}
	}
	// Then it follows: a frame appended after the subscription arrives once,
	// and the replayed frames do not arrive again.
	w.srv.Live.Append("wi-x", control.Frame{Kind: "tool", Call: "t-2", Tool: "Edit", Text: "Edit main.go"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, ok := next()
		if !ok {
			break
		}
		var f control.Frame
		if json.Unmarshal([]byte(d), &f) != nil || f.Item != "wi-x" || f.Kind != "tool" {
			continue
		}
		if f.Call == "t-1" {
			t.Fatalf("a replayed frame was sent again: %+v", f)
		}
		if f.Call == "t-2" {
			return
		}
	}
	t.Fatal("the frame appended after subscribing never arrived")
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("GET %s: %v: %s", url, err, b)
	}
}
