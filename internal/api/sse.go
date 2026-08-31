package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
)

// activityScopes are the chains that together tell the daemon's story from
// the outside: what arrived, what is queued, what landed, what it cost, and
// what the stop button did. A per-record global feed arrives with change
// streams in Phase 7; until then the indexes ARE the activity.
var activityScopes = []string{
	"index/ingress", "index/pending", "index/landings", "index/exceptions",
	"control/capacity", "control/claims",
}

// Activity is one record on the composite feed.
type Activity struct {
	Scope string          `json:"scope"`
	Seq   uint64          `json:"seq"`
	Kind  string          `json:"kind"`
	Sub   string          `json:"subject"`
	Actor string          `json:"actor"`
	At    time.Time       `json:"at"`
	Body  json.RawMessage `json:"body,omitempty"`
}

func toActivity(scope string, r evidence.Record) Activity {
	// Record bodies are whatever their writer meant — JSON for structured
	// records, plain prose for reasons and notes. The feed is JSON, so prose
	// rides quoted rather than corrupting the stream.
	body := json.RawMessage(r.Body)
	if !json.Valid(r.Body) {
		body, _ = json.Marshal(string(r.Body))
	}
	return Activity{Scope: scope, Seq: r.Seq, Kind: r.Kind, Sub: r.Subject,
		Actor: r.Actor, At: r.At, Body: body}
}

// activity returns the recent composite feed, newest last.
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	var out []Activity
	for _, scope := range activityScopes {
		for rec, err := range s.Store.Log().Since(r.Context(), s.Tenant, scope, 1) {
			if err != nil {
				httpErr(w, 500, err.Error())
				return
			}
			out = append(out, toActivity(scope, rec))
		}
	}
	sortActivities(out)
	if len(out) > 200 {
		out = out[len(out)-200:]
	}
	writeJSON(w, out)
}

// events streams the same feed as server-sent events: a UX subscribes once
// and watches the factory run. Poll-based tailing per connection — cheap at
// this scale, replaced by a push feed when the store grows one.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		httpErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	last := map[string]uint64{}
	// First pass establishes the horizon: subscribers want what happens
	// next, not a replay (GET /v1/activity is the replay).
	for _, scope := range activityScopes {
		for rec, err := range s.Store.Log().Since(r.Context(), s.Tenant, scope, 1) {
			if err == nil && rec.Seq > last[scope] {
				last[scope] = rec.Seq
			}
		}
	}
	fmt.Fprintf(w, "event: hello\ndata: {\"tenant\":%q}\n\n", s.Tenant)
	fl.Flush()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			for _, scope := range activityScopes {
				for rec, err := range s.Store.Log().Since(r.Context(), s.Tenant, scope, last[scope]+1) {
					if err != nil {
						return
					}
					last[scope] = rec.Seq
					b, _ := json.Marshal(toActivity(scope, rec))
					fmt.Fprintf(w, "event: activity\ndata: %s\n\n", b)
				}
			}
			fl.Flush()
		}
	}
}

func sortActivities(a []Activity) {
	// stable enough: by time, then scope/seq for determinism
	sortSlice(a, func(x, y Activity) bool {
		if !x.At.Equal(y.At) {
			return x.At.Before(y.At)
		}
		if x.Scope != y.Scope {
			return x.Scope < y.Scope
		}
		return x.Seq < y.Seq
	})
}
