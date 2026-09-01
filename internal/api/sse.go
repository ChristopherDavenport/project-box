package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
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
	// A claimed item was a black box: the index scopes say a unit exists and
	// later that it landed, and everything between — the agent starting, the
	// commit, each gate verdict, an escalation and its reason — lives on the
	// item's own chain, which nothing was tailing. Watching a run meant
	// reloading a page. In-flight items are followed too, and dropped again
	// when they settle, so the tail set is bounded by work actually moving.
	following := s.inFlight(r.Context())
	for id := range following {
		scope := "workitem/" + id
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
					// A newly indexed item is one to follow from here on.
					if scope == "index/pending" {
						following[rec.Subject] = true
					}
				}
			}
			for id := range following {
				scope := "workitem/" + id
				settled := false
				for rec, err := range s.Store.Log().Since(r.Context(), s.Tenant, scope, last[scope]+1) {
					if err != nil {
						return
					}
					last[scope] = rec.Seq
					b, _ := json.Marshal(toActivity(scope, rec))
					fmt.Fprintf(w, "event: activity\ndata: %s\n\n", b)
					settled = settled || terminalKind(rec.Kind)
				}
				if settled {
					// Its story is told. A settled item that is re-queued
					// re-enters through index/pending.
					delete(following, id)
				}
			}
			fl.Flush()
		}
	}
}

// terminalKind reports whether a record settles an item.
//
// "parked" settles the stream but not the item: an operator may retry it back
// into the queue, and the retry re-enters the follow set through the
// index/pending record [queue.Queue.Retry] writes.
func terminalKind(k string) bool {
	return k == "done" || k == "failed" || k == "parked" || k == "resolved"
}

// inFlight names the items whose chains are worth following: everything the
// tenant has submitted that has not settled. One fold at connect, never per
// tick — the set only grows through index/pending, which is already tailed.
func (s *Server) inFlight(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for rec, err := range s.Store.Log().Since(ctx, s.Tenant, "index/pending", 1) {
		if err != nil {
			return out
		}
		item, lerr := s.Queue.Load(ctx, s.Tenant, rec.Subject)
		if lerr != nil {
			continue
		}
		switch item.State {
		case queue.StateDone, queue.StateFailed, queue.StateParked, queue.StateResolved:
		default:
			out[rec.Subject] = true
		}
	}
	return out
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
