// Package api is the daemon's interface: one HTTP+JSON surface for UX,
// CLIs, and remote delegation, identical over a local unix socket and over
// TCP in a cloud deployment. What differs by environment is transport and
// authentication, never the API.
//
// The API writes intent and reads state; it never executes. POST /v1/ingress
// opens an ingress and the daemon's own tick turns it into work — so a
// network caller gets exactly the semantics a local operator does: the stop
// button, the budget, the fences all bind identically, because there is only
// one path to execution.
//
// Locally the listener is a unix domain socket: filesystem permissions are
// the auth, and no port is exposed to anything. Over TCP a bearer token is
// required (refusing to start without one — an open TCP control plane is a
// misconfiguration, not a default), and that check is the single seam OIDC
// replaces at Phase 7.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

// Server serves one tenant's daemon API — over a unix domain socket, and
// nothing else. The daemon's whole trust model is the socket's file mode:
// no tokens, no cookies, no TLS, no network. Everything network-shaped is a
// LAYER in front of the socket (cmd/boxgate carries the bearer token today
// and is the seam OIDC replaces; `box room` fronts it for a local browser),
// so seams multiply without the daemon changing — the dockerd posture.
type Server struct {
	Store  store.Store
	Tenant tenant.ID
	// Routes is the server-side route table: clients name a route, never
	// define one — which kinds work may open, and on whose authority, is the
	// operator's configuration, not the caller's request.
	Routes map[string]ingress.Route
	Router *ingress.StoreRouter
	Queue  *queue.Queue
	// Meter, when set, puts the capacity window's account on /v1/status —
	// what the factory has spent, and how much of that it could not measure.
	// A cost brake nobody can read is a brake nobody trusts.
	Meter *capacity.Meter
	// Box and Runner identify this daemon in /v1/status.
	Box, Runner string
	Started     time.Time
	// Reload, when set, re-reads the daemon's configuration; wired by the
	// daemon, exposed as POST /v1/reload.
	Reload func(ctx context.Context, actor string) (string, error)
	// UI, when set, is served at / with SPA fallback: a path that is not a
	// file gets index.html, so the router's deep links work on refresh.
	UI fs.FS

	mu sync.RWMutex // guards Routes and Token swaps
}

// SetRoutes swaps the route table on reload.
func (s *Server) SetRoutes(r map[string]ingress.Route) {
	s.mu.Lock()
	s.Routes = r
	s.mu.Unlock()
}

func (s *Server) route(name string) (ingress.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.Routes[name]
	return r, ok
}

func (s *Server) routeNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.Routes))
	for n := range s.Routes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Handler returns the API as an http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("GET /v1/exceptions", s.exceptions)
	mux.HandleFunc("GET /v1/items", s.items)
	mux.HandleFunc("GET /v1/items/{id}", s.item)
	mux.HandleFunc("GET /v1/items/{id}/evidence", s.itemEvidence)
	mux.HandleFunc("GET /v1/provenance/{sha}", s.provenance)
	mux.HandleFunc("GET /v1/activity", s.activity)
	mux.HandleFunc("GET /v1/events", s.events)
	mux.HandleFunc("POST /v1/ingress", s.openIngress)
	mux.HandleFunc("POST /v1/stop", s.stop)
	mux.HandleFunc("POST /v1/resume", s.resume)
	mux.HandleFunc("POST /v1/items/{id}/resolve", s.resolve)
	mux.HandleFunc("POST /v1/items/{id}/retry", s.retry)
	mux.HandleFunc("POST /v1/reload", s.reload)
	if s.UI != nil {
		files := http.FileServerFS(s.UI)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				httpErr(w, http.StatusMethodNotAllowed, "the UI is read-only; the API lives under /v1")
				return
			}
			p := strings.TrimPrefix(r.URL.Path, "/")
			if p != "" && p != "v1" {
				if f, err := s.UI.Open(p); err == nil {
					f.Close()
					files.ServeHTTP(w, r)
					return
				}
			}
			// SPA fallback: the control room's router owns unknown paths.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			files.ServeHTTP(w, r2)
		})
	}
	return mux
}

// actor is who a mutating request acts as. With bearer auth this is still
// the operator's own claim; OIDC turns it into a verified subject.
func (s *Server) actor(r *http.Request) string {
	if a := r.Header.Get("X-Box-Actor"); a != "" {
		return a
	}
	return "local-operator"
}

// Status is /v1/status.
type Status struct {
	Tenant     tenant.ID `json:"tenant"`
	Box        string    `json:"box"`
	Runner     string    `json:"runner"`
	Stopped    bool      `json:"stopped"`
	Pending    int       `json:"pending"`
	Exceptions int       `json:"exceptions"`
	UptimeSec  int64     `json:"uptime_sec"`
	Routes     []string  `json:"routes"`
	Version    string    `json:"version"`
	// Spend is the capacity window's account. Nil when the server was wired
	// without a meter; absent rather than zero, because a zero here would
	// read as "nothing spent" when it means "nobody asked".
	Spend *capacity.Report `json:"spend,omitempty"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stopped, err := s.Queue.Stopped(ctx, s.Tenant)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	pending, exceptions := 0, 0
	seen := map[string]bool{}
	for rec, err := range s.Store.Log().Since(ctx, s.Tenant, "index/pending", 1) {
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		item, err := s.Queue.Load(ctx, s.Tenant, rec.Subject)
		if err != nil {
			continue
		}
		switch item.State {
		case queue.StatePending, queue.StateWaiting:
			pending++
		case queue.StateParked:
			exceptions++
		}
	}
	st := Status{
		Tenant: s.Tenant, Box: s.Box, Runner: s.Runner, Stopped: stopped,
		Pending: pending, Exceptions: exceptions,
		UptimeSec: int64(time.Since(s.Started).Seconds()), Routes: s.routeNames(),
		Version: version.String(),
	}
	if s.Meter != nil {
		rep := s.Meter.Report()
		st.Spend = &rep
	}
	writeJSON(w, st)
}

// items lists the work queue. `?state=` narrows it and may repeat or be
// comma-separated; unfiltered returns everything the tenant has submitted.
//
// The listing verb the surface never had: `pending=14` was the whole view
// into the queue, and /v1/items/{id} needs an id you could only know from the
// submit response you kept.
func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	var states []queue.State
	for _, v := range r.URL.Query()["state"] {
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				states = append(states, queue.State(part))
			}
		}
	}
	room := &control.Room{Store: s.Store, Tenant: s.Tenant, Actor: s.actor(r)}
	out, err := room.Queue(r.Context(), states...)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if out == nil {
		out = []control.QueueEntry{}
	}
	writeJSON(w, out)
}

func (s *Server) exceptions(w http.ResponseWriter, r *http.Request) {
	room := &control.Room{Store: s.Store, Tenant: s.Tenant, Actor: s.actor(r)}
	items, err := room.Exceptions(r.Context())
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, items)
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	item, err := s.Queue.Load(r.Context(), s.Tenant, r.PathValue("id"))
	if err != nil {
		httpErr(w, 404, err.Error())
		return
	}
	writeJSON(w, item)
}

func (s *Server) itemEvidence(w http.ResponseWriter, r *http.Request) {
	s.writeChain(w, r, "workitem/"+r.PathValue("id"))
}

func (s *Server) provenance(w http.ResponseWriter, r *http.Request) {
	var out []evidence.Record
	for rec, err := range s.Store.Log().BySubject(r.Context(), s.Tenant, r.PathValue("sha")) {
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, out)
}

// OpenIngressRequest is the delegation verb's body: name a configured
// route, supply the occurrence. This is how a UX submits work and how a
// remote orchestrator delegates it — the same call.
type OpenIngressRequest struct {
	Route   string `json:"route"`
	Key     string `json:"key"`
	Payload string `json:"payload,omitempty"`
}

// OpenIngressResponse reports what the delivery named.
type OpenIngressResponse struct {
	Ingress ingress.Ingress `json:"ingress"`
	Opened  bool            `json:"opened"`
	// Item is the id intake will use, so a client can poll it immediately.
	Item string `json:"item"`
}

func (s *Server) openIngress(w http.ResponseWriter, r *http.Request) {
	var req OpenIngressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, 400, "bad body: "+err.Error())
		return
	}
	route, ok := s.route(req.Route)
	if !ok {
		httpErrCode(w, 404, "route_not_found", fmt.Sprintf("no route %q — routes are operator configuration, not caller request", req.Route))
		return
	}
	ing, opened, err := s.Router.Open(r.Context(), route, ingress.Delivery{
		Key: req.Key, Payload: []byte(req.Payload), Received: time.Now().UTC(),
	})
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, OpenIngressResponse{Ingress: ing, Opened: opened, Item: "wi-" + ing.ID})
}

// ControlRequest carries a reason; stop and resume are records, and a
// record needs one.
type ControlRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request)   { s.control(w, r, s.Queue.Stop) }
func (s *Server) resume(w http.ResponseWriter, r *http.Request) { s.control(w, r, s.Queue.Resume) }

// resolve and retry are the two ways off the exception queue, and the only
// endpoints on this surface that change an item rather than record an intent
// for the pump to act on. That is deliberate: the pump is the only thing that
// may START work, but an exception is by definition work the pump has already
// given up on, and asking it to also be the path back would mean an operator
// could not clear a queue the pump had stopped draining.
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) { s.reopen(w, r, s.Queue.Resolve) }
func (s *Server) retry(w http.ResponseWriter, r *http.Request)   { s.reopen(w, r, s.Queue.Retry) }

func (s *Server) reopen(w http.ResponseWriter, r *http.Request,
	f func(context.Context, tenant.ID, string, string, string) (queue.WorkItem, error)) {
	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, http.StatusBadRequest, "a JSON body with a reason is required")
		return
	}
	item, err := f(r.Context(), s.Tenant, r.PathValue("id"), s.actor(r), req.Reason)
	switch {
	case errors.Is(err, queue.ErrNoItem):
		httpErr(w, http.StatusNotFound, err.Error())
	case err != nil:
		// A wrong-state reopen is the caller's mistake, not the server's: the
		// item moved under them, which is exactly what an operator racing a
		// runner needs told rather than retried.
		httpErr(w, http.StatusConflict, err.Error())
	default:
		writeJSON(w, item)
	}
}

func (s *Server) control(w http.ResponseWriter, r *http.Request, f func(context.Context, tenant.ID, string, string) error) {
	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		httpErr(w, 400, "a reason is required — it becomes the record")
		return
	}
	if err := f(r.Context(), s.Tenant, s.actor(r), req.Reason); err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if s.Reload == nil {
		httpErr(w, http.StatusNotImplemented, "this daemon was started without a config file; reload needs one")
		return
	}
	summary, err := s.Reload(r.Context(), s.actor(r))
	if err != nil {
		httpErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, map[string]string{"reloaded": summary})
}

func (s *Server) writeChain(w http.ResponseWriter, r *http.Request, scope string) {
	var out []evidence.Record
	for rec, err := range s.Store.Log().Since(r.Context(), s.Tenant, scope, 1) {
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// httpErr writes the error envelope every client parses: a human message
// and a MACHINE code. The dockerd lesson adopted early: clients that must
// string-match error prose calcify the prose; codes are the contract.
func httpErr(w http.ResponseWriter, code int, msg string) {
	httpErrCode(w, code, defaultCode(code), msg)
}

func httpErrCode(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": code})
}

func defaultCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusUnprocessableEntity:
		return "invalid"
	case http.StatusNotImplemented:
		return "unsupported"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	}
	return "internal"
}

// Listen binds the daemon's ONLY listener kind: a unix domain socket. The
// daemon is never network-reachable itself; anything network-shaped is a
// layer in front (see cmd/boxgate). This is a design decision, not a
// missing feature.
func Listen(spec string) (net.Listener, error) {
	if !strings.HasPrefix(spec, "unix:") {
		return nil, fmt.Errorf("api: the daemon listens on a unix socket only (%q given) — put a gate in front for the network (boxgate, or any proxy)", spec)
	}
	path := strings.TrimPrefix(spec, "unix:")
	// A previous daemon's socket file blocks bind; a live one refuses
	// connections distinctly, so removal here only clears the stale case.
	_ = removeIfSocket(path)
	return net.Listen("unix", path)
}
