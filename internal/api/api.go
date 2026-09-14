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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
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
	// Plans, when set, lets retry and resolve reach an origination whose
	// planning failed — the one exception that exists before any item does.
	Plans *plan.Store
	// EscalationResolved, when set, is told a person gave up on an
	// origination's planning — invoked only once s.Plans.Resolve itself has
	// succeeded, carrying the ingress id, the resolving actor, the plan
	// failure's own recorded reason (read just before Resolve overwrote the
	// chain's newest standing), and the resolver's own note. Nil is exactly
	// today's behavior: resolving an escalation closes nothing — the same
	// nil-means-unwired seam as loop.PRLander and loop.Approval.
	EscalationResolved func(ctx context.Context, id, actor, reason, note string)
	// Meter, when set, puts the capacity window's account on /v1/status —
	// what the factory has spent, and how much of that it could not measure.
	// A cost brake nobody can read is a brake nobody trusts.
	Meter *capacity.Meter
	// Box and Runner identify this daemon in /v1/status.
	Box, Runner string
	// Sections is each configured repository's declared section names,
	// keyed by [queue.WorkItem.Repo] — GET /v1/sections needs these to
	// report a section that has never landed anything at zero rather than
	// leave it out of the census entirely (issue #200). Wired at assemble
	// from the box configuration; nil reports every repo as having declared
	// none.
	Sections map[string][]string
	// Objective is the exception queue's response-time objective, reported
	// on /v1/status beside the measurement so a reader never has to look up
	// what the number is being judged against. Zero means
	// [control.DefaultObjective].
	Objective time.Duration
	Started   time.Time
	// Reload, when set, re-reads the daemon's configuration; wired by the
	// daemon, exposed as POST /v1/reload.
	Reload func(ctx context.Context, actor string) (string, error)
	// Live, when set, is the window into runs still going: the ACP stream
	// fanned in from every agent this daemon is running. It is what turns a
	// claimed item from `state=claimed head=` into a unit somebody can watch.
	// Nil leaves every session's timeline empty and `watching` false, which is
	// the truthful answer for a daemon nothing reports to.
	Live *control.Live
	// UI, when set, is served at / with SPA fallback: a path that is not a
	// file gets index.html, so the router's deep links work on refresh.
	UI fs.FS
	// Build, when set, reports this daemon's own running build for
	// /v1/status. Nil defaults to version.ThisBuild directly — the production
	// behavior; a test substitutes a fixed fact the same way internal/daemon's
	// own build field does, rather than depending on how the test binary
	// itself happened to be built.
	Build func() version.Build
	// FollowBranch, when set, resolves the box's self_deploy.from branch to
	// the commit this daemon's own mirror last recorded for it —
	// mirror.Resolve is a local git rev-parse in the mirror directory, never
	// a fetch, so /v1/status shows the box's own record of the branch rather
	// than a fresh look at it. Nil when no box has self_deploy configured,
	// which /v1/status reads as "these fields are absent," not an error.
	FollowBranch func(ctx context.Context) (branch, head string, err error)
	// LastPass, when set, reports this daemon's own most recent self-deploy
	// housekeeping pass: when it ran, what it decided, and why. Nil-safe
	// like FollowBranch above, but for a different absence: ok is false
	// (rather than the seam itself being nil) until the first pass this
	// process has run, which /v1/status must also read as the fields being
	// absent — a restart carries no memory of a pass from before it.
	LastPass func() (at time.Time, decision, reason string, ok bool)
	// ActorClaims is the allowlist an X-Box-Actor claim must appear in to be
	// honored at all — see [boxcfg.Config.ActorClaims]. Nil, cmd/boxd's flag
	// path, refuses every claim: the peer still becomes Actor, but no header
	// ever reaches OnBehalfOf. Swappable on reload, same as Routes.
	ActorClaims []string
	// Auth resolves who a MUTATING request acts as — see [Authenticator]. A
	// NIL-SAFE seam exactly like loop.PRLander: nil resolves to
	// [PeerAuthenticator], which is byte-for-byte what mustActor did before
	// this seam existed, so every suite that drives a write over a real unix
	// socket keeps passing unchanged. Production wiring always sets one,
	// even to name peer explicitly, so a box's choice is legible in its own
	// configuration rather than in this field's absence. Swappable on
	// reload, same as Routes and ActorClaims.
	Auth Authenticator

	mu sync.RWMutex // guards Routes, ActorClaims, Auth and Token swaps
}

// SetRoutes swaps the route table on reload.
func (s *Server) SetRoutes(r map[string]ingress.Route) {
	s.mu.Lock()
	s.Routes = r
	s.mu.Unlock()
}

// SetActorClaims swaps the X-Box-Actor allowlist on reload — a box that
// tightens or loosens who may be claimed should not need a restart to mean
// it, and a stale allowlist surviving a reload would be a widened envelope
// nobody asked for.
func (s *Server) SetActorClaims(claims []string) {
	s.mu.Lock()
	s.ActorClaims = claims
	s.mu.Unlock()
}

// SetAuth swaps the authenticator on reload — a box that rotates a token
// table or switches methods should not need a restart to mean it.
func (s *Server) SetAuth(a Authenticator) {
	s.mu.Lock()
	s.Auth = a
	s.mu.Unlock()
}

func (s *Server) claimAllowed(claim string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Contains(s.ActorClaims, claim)
}

// authenticator answers the configured [Authenticator], or [PeerAuthenticator]
// when none was set — the nil-safe seam [Server.Auth] documents.
func (s *Server) authenticator() Authenticator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Auth != nil {
		return s.Auth
	}
	return PeerAuthenticator{}
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
	mux.HandleFunc("GET /v1/flakes", s.flakes)
	mux.HandleFunc("GET /v1/sections", s.sections)
	mux.HandleFunc("GET /v1/items", s.items)
	mux.HandleFunc("GET /v1/items/{id}", s.item)
	mux.HandleFunc("GET /v1/items/{id}/evidence", s.itemEvidence)
	mux.HandleFunc("GET /v1/items/{id}/session", s.session)
	mux.HandleFunc("GET /v1/items/{id}/sessions", s.itemSessions)
	mux.HandleFunc("GET /v1/sessions", s.sessions)
	mux.HandleFunc("GET /v1/runs/{run}/frames", s.runFrames)
	mux.HandleFunc("GET /v1/provenance/{sha}", s.provenance)
	mux.HandleFunc("GET /v1/provenance/{sha}/chain", s.provenanceChain)
	mux.HandleFunc("GET /v1/provenance/{sha}/export", s.provenanceExport)
	mux.HandleFunc("GET /v1/plans/{ref}", s.planByRef)
	mux.HandleFunc("GET /v1/plans/{ref}/diff/{other}", s.planDiff)
	mux.HandleFunc("GET /v1/ingress/{id}", s.ingress)
	mux.HandleFunc("GET /v1/ingress/{id}/plans", s.ingressPlans)
	mux.HandleFunc("GET /v1/origins/{key}", s.origins)
	mux.HandleFunc("GET /v1/origins/{key}/export", s.originsExport)
	mux.HandleFunc("GET /v1/issues/{n}", s.originsByIssue)
	mux.HandleFunc("GET /v1/prs/{n}", s.originsByPR)
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

// mustActor resolves who a MUTATING request acts as: the subject the
// configured [Authenticator] vouched for — kernel credentials by default,
// never anything the caller wrote unauthenticated. A subject the
// authenticator cannot vouch for is refused outright rather than defaulted
// to any name; "local-operator" does not exist here for the same reason.
// Pid is filled in whenever the connection itself carries a kernel-asserted
// peer, regardless of which method actually authenticated it — the pid is a
// fact about the transport, not about the auth decision. An X-Box-Actor
// header is a claim, never an identity: honored only when it appears in this
// box's own allowlist, in which case it lands in Identity.OnBehalfOf while
// the authenticated subject stays Identity.Name — delegation intersects and
// a claim only ever narrows who authority was exercised for, never replaces
// who exercised it. An unlisted claim refuses the whole request, not just
// the claim, so a caller cannot silently fall back to acting as the subject
// when the on-behalf-of it asked for was refused.
func (s *Server) mustActor(r *http.Request) (queue.Identity, error) {
	subj, err := s.authenticator().Authenticate(r)
	if err != nil {
		return queue.Identity{}, err
	}
	id := queue.Identity{Name: subj.Name}
	if cred := peerFromContext(r.Context()); cred.ok {
		id.Pid = cred.pid
	}
	if claim := r.Header.Get("X-Box-Actor"); claim != "" {
		if !s.claimAllowed(claim) {
			return queue.Identity{}, fmt.Errorf("actor claim %q is not in this box's allowlist", claim)
		}
		id.OnBehalfOf = claim
	}
	return id, nil
}

// bestEffortSubject resolves this request's Subject through the configured
// authenticator WITHOUT refusing — used only to annotate a delivery's
// Source (openIngress), where an absent subject is a legitimate case (an
// unauthenticated caller hitting a route that requires nothing) rather than
// a reason to refuse the write. [ingress.Route.Require] is the actual gate;
// see openIngress. Matches peerName's existing doctrine: an empty Subject is
// the honest "unknown", never a guess.
func (s *Server) bestEffortSubject(r *http.Request) Subject {
	subj, err := s.authenticator().Authenticate(r)
	if err != nil {
		return Subject{}
	}
	return subj
}

// refuseActor writes mustActor's refusal in the api package's error
// envelope. 403: the connection is authenticated (it is a socket peer or it
// would not have reached the handler) but not authorized for this claim or
// for writing at all.
func refuseActor(w http.ResponseWriter, err error) {
	httpErrCode(w, http.StatusForbidden, "actor_required", err.Error())
}

// peerName is a best-effort peer for READ-ONLY context: unlike mustActor it
// never refuses, because a GET must answer over whatever transport reaches
// it — tests and `box room`'s browser-facing surface both drive reads over
// plain TCP. Empty is the honest "unknown" a read has no reason to reject.
func (s *Server) peerName(r *http.Request) string {
	cred := peerFromContext(r.Context())
	if !cred.ok {
		return ""
	}
	return accountName(cred.uid)
}

// room is this request's control surface, carrying the server's objective so
// every reader measures the queue against the same number.
func (s *Server) room(r *http.Request) *control.Room {
	return &control.Room{Store: s.Store, Tenant: s.Tenant, Actor: s.peerName(r),
		Objective: s.Objective, Live: s.Live}
}

// Status is /v1/status.
type Status struct {
	Tenant  tenant.ID `json:"tenant"`
	Box     string    `json:"box"`
	Runner  string    `json:"runner"`
	Stopped bool      `json:"stopped"`
	// Claims is Stopped with its record attached: who stopped or resumed
	// claims, when, and the reason they gave. The bit alone sent an operator
	// to the activity feed to find out whether the stop was theirs.
	Claims     queue.Control `json:"claims"`
	Pending    int           `json:"pending"`
	Exceptions int           `json:"exceptions"`
	UptimeSec  int64         `json:"uptime_sec"`
	Routes     []string      `json:"routes"`
	Version    string        `json:"version"`
	// ExceptionQueue is the count's missing half: how old the oldest parked
	// item is, and whether that clears the objective. A count alone cannot
	// distinguish a queue being worked from one nobody has opened in a week.
	ExceptionQueue control.ExceptionHealth `json:"exception_queue"`
	// Backlog is where Pending comes from: the listing at GET /v1/items,
	// counted. Both numbers are folds of the same rows rather than a counter
	// beside a listing, so "pending=14" and the fourteen units it names cannot
	// come apart — and the state breakdown says which fourteen.
	Backlog control.Backlog `json:"backlog"`
	// Spend is the capacity window's account. Nil when the server was wired
	// without a meter; absent rather than zero, because a zero here would
	// read as "nothing spent" when it means "nobody asked".
	Spend *capacity.Report `json:"spend,omitempty"`
	// BuildCommit and BuildDirty are this daemon's own running build, from
	// version.ThisBuild: the commit the binary was actually compiled from,
	// and whether its tree had uncommitted changes. Both omitted when the
	// build is not knowable — an unstamped `go test` binary, an unstamped
	// build outside a VCS checkout — because a zero value here would read as
	// "clean, no commit" when the honest answer is "unknown."
	BuildCommit string `json:"build_commit,omitempty"`
	BuildDirty  bool   `json:"build_dirty,omitempty"`
	// FollowBranch and FollowHead are self_deploy's followed branch and the
	// commit this daemon's own mirror last recorded for it — the mirror's own
	// record, not a fresh fetch. Both absent when the box has no self_deploy
	// configured.
	FollowBranch string `json:"follow_branch,omitempty"`
	FollowHead   string `json:"follow_head,omitempty"`
	// LastPassAt, LastPassDecision and LastPassReason project this
	// daemon's most recent self-deploy housekeeping pass — when it ran,
	// what it decided ("current", "skipped", "deploying", "abandoned",
	// "refused", or one of a few error decisions), and why. All three are
	// absent together: either no box has self_deploy configured, or this
	// process has not yet run a pass. A pass that decided nothing still
	// ran, and still shows up here.
	LastPassAt       string `json:"last_pass_at,omitempty"`
	LastPassDecision string `json:"last_pass_decision,omitempty"`
	LastPassReason   string `json:"last_pass_reason,omitempty"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, err := s.Queue.Control(ctx, s.Tenant)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	// Both counts come from the room rather than a fold apiece here: two
	// places counting the same thing is two places that can disagree, and the
	// room is the one that also knows how old the oldest parked item is and
	// which states its pending number is made of.
	//
	// Pending used to be counted here, by its own walk of index/pending with
	// its own copy of the not-yet-settled vocabulary. It agreed with the
	// listing by coincidence of two switches matching, which is the agreement
	// that lasts exactly until a state is added to one of them.
	backlog, err := s.room(r).Backlog(ctx)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	health, err := s.room(r).ExceptionHealth(ctx)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	st := Status{
		Tenant: s.Tenant, Box: s.Box, Runner: s.Runner, Stopped: claims.Stopped, Claims: claims,
		Pending: backlog.Pending, Backlog: backlog,
		Exceptions: health.Parked, ExceptionQueue: health,
		UptimeSec: int64(time.Since(s.Started).Seconds()), Routes: s.routeNames(),
		Version: version.String(),
	}
	if s.Meter != nil {
		rep := s.Meter.Report()
		st.Spend = &rep
	}
	build := version.ThisBuild()
	if s.Build != nil {
		build = s.Build()
	}
	if build.Known {
		st.BuildCommit = build.Commit
		st.BuildDirty = build.Dirty
	}
	if s.FollowBranch != nil {
		branch, head, err := s.FollowBranch(ctx)
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		st.FollowBranch = branch
		st.FollowHead = head
	}
	if s.LastPass != nil {
		if at, decision, reason, ok := s.LastPass(); ok {
			st.LastPassAt = at.UTC().Format(time.RFC3339)
			st.LastPassDecision = decision
			st.LastPassReason = reason
		}
	}
	writeJSON(w, st)
}

// items lists the work queue. `?state=` narrows it and may repeat or be
// comma-separated; unfiltered returns everything the tenant has submitted.
//
// The listing verb the surface never had: `pending=14` was the whole view
// into the queue, and /v1/items/{id} needs an id you could only know from the
// submit response you kept.
//
// Every row carries the frontier as well as the state — whether the item is
// ready to claim, which declared dependencies have not settled, which item a
// cascade started at, and who holds the claim with how much lease left. There
// is deliberately no `?ready=` filter beside `?state=`: the frontier is a
// projection over the same rows, and a caller narrowing to it would lose the
// contrast — a lane of five ready items is only alarming next to the fact
// that nothing is claimed.
func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	var states []queue.State
	for _, v := range r.URL.Query()["state"] {
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				states = append(states, queue.State(part))
			}
		}
	}
	out, err := s.room(r).Queue(r.Context(), states...)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if out == nil {
		out = []control.QueueEntry{}
	}
	writeJSON(w, out)
}

// exceptions lists what still needs a person, loudest and oldest first, each
// row carrying its age and how many times somebody has already been told.
func (s *Server) exceptions(w http.ResponseWriter, r *http.Request) {
	items, err := s.room(r).Exceptions(r.Context())
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []control.Exception{}
	}
	writeJSON(w, items)
}

// flakes lists every test the loop has proven flaky, newest occurrence
// first, each row carrying its count and the detail of the latest proof.
func (s *Server) flakes(w http.ResponseWriter, r *http.Request) {
	items, err := s.room(r).Flakes(r.Context())
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []control.FlakeEntry{}
	}
	writeJSON(w, items)
}

// sections is GET /v1/sections: the per-repository section census folded
// from the landed chain (issue #200) — what has landed where, what waited
// on a section and for how long, and which sections tend to change
// together. Keyed by repo, using s.Sections' declared names so a section
// that has never landed anything still appears, at zero.
func (s *Server) sections(w http.ResponseWriter, r *http.Request) {
	room := s.room(r)
	out := make(map[string]control.SectionCensus, len(s.Sections))
	for repo, names := range s.Sections {
		census, err := room.Sections(r.Context(), repo, names)
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		out[repo] = census
	}
	writeJSON(w, out)
}

// sessions is the per-unit live view: one entry per RUNNING unit, each with
// its own identity, its own progress, and its own timeline.
//
// A collection rather than a merged feed, because fan-out is the case that
// breaks a feed. Several units in flight interleave into one stream that
// reads as noise, and the question an operator has — is THIS unit moving —
// cannot be answered by scrolling. One entry per unit answers it per unit.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.room(r).Sessions(r.Context())
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if out == nil {
		out = []control.Session{}
	}
	writeJSON(w, out)
}

// session is one unit's card, whatever its state: an operator following a
// link wants to be told the run settled, not shown an empty page.
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	out, err := s.room(r).Session(r.Context(), r.PathValue("id"))
	if err != nil {
		httpErr(w, 404, err.Error())
		return
	}
	writeJSON(w, out)
}

// itemSessions lists one unit's agent sessions from its chain — the same
// rows Session's own AgentSessions field carries, on their own for a caller
// that wants just the attempts. The chain outlives the claim, so a settled
// item still answers, and matching runFrames' doctrine (runs.go), an id with
// no agent-run records answers an empty list rather than a 404.
func (s *Server) itemSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.room(r).AgentSessions(r.Context(), r.PathValue("id"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if out == nil {
		out = []control.AgentSession{}
	}
	writeJSON(w, out)
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
	Summary string `json:"summary,omitempty"`
	// Channel optionally declares how this delivery arrived: "hand" for an
	// operator-run submit, "script" for an unattended caller. It is the
	// caller's own claim, recorded as such on [ingress.Source.Channel] —
	// absent stays absent, never defaulted to "hand", because a channel
	// nobody declared is not known to be a hand-run one.
	Channel string `json:"channel,omitempty"`
	// Priority optionally claims this delivery's ordering — see
	// [queue.Priority]. Empty inherits the box's default (normal); an
	// unrecognised value is the caller's own error, refused before
	// [ingress.Router.Open] ever sees it.
	Priority string `json:"priority,omitempty"`
}

// OpenIngressResponse reports what the delivery named.
type OpenIngressResponse struct {
	Ingress ingress.Ingress `json:"ingress"`
	Opened  bool            `json:"opened"`
	// Item is the id intake will use, so a client can poll it immediately.
	Item string `json:"item"`
}

// declaredChannels is the vocabulary an [OpenIngressRequest.Channel] claim
// may name — the two ways a caller external to this package hands work in.
// "route:<name>" and "factory" are stamped by producers inside the box
// (poll.go, loop.OpenRepair) about themselves, never a caller's claim.
var declaredChannels = map[string]bool{"hand": true, "script": true}

func (s *Server) openIngress(w http.ResponseWriter, r *http.Request) {
	var req OpenIngressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, 400, "bad body: "+err.Error())
		return
	}
	if req.Channel != "" && !declaredChannels[req.Channel] {
		httpErr(w, 400, fmt.Sprintf("channel %q — only \"hand\" or \"script\" may be declared", req.Channel))
		return
	}
	switch queue.Priority(req.Priority) {
	case "", queue.PriorityLow, queue.PriorityNormal, queue.PriorityHigh, queue.PriorityUrgent:
	default:
		httpErr(w, 400, fmt.Sprintf("priority %q — the levels are low, normal, high, urgent", req.Priority))
		return
	}
	route, ok := s.route(req.Route)
	if !ok {
		httpErrCode(w, 404, "route_not_found", fmt.Sprintf("no route %q — routes are operator configuration, not caller request", req.Route))
		return
	}
	subj := s.bestEffortSubject(r)
	if len(route.Require) > 0 && !slices.Contains(route.Require, subj.Method) {
		reason := fmt.Sprintf("route %q requires one of %v; this delivery authenticated as %q", route.Name, route.Require, subj.Method)
		s.refuseIngress(w, r, route.Name, subj, reason)
		return
	}
	ing, opened, err := s.Router.Open(r.Context(), route, ingress.Delivery{
		Key: req.Key, Payload: []byte(req.Payload), Title: req.Summary, Received: time.Now().UTC(),
		Source:   ingress.Source{Subject: subj.Name, Method: subj.Method, Channel: req.Channel},
		Priority: queue.Priority(req.Priority),
	})
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, OpenIngressResponse{Ingress: ing, Opened: opened, Item: "wi-" + ing.ID})
}

// refuseIngress writes a route.Require refusal AND records it on the
// route's own standing chain — the pollScope precedent
// (internal/ingress/poll.go), applied to a wire-side refusal rather than a
// listing failure: "refused at the wire with the reason" (issue #188) must
// itself be provable, not just returned to the one caller who saw it. The
// record is appended before the response is written, same as poll.go's
// recordFailure: a refusal that could not even be recorded is a louder
// problem than the refusal itself, so that failure escalates to a 500
// rather than being swallowed behind the 403.
func (s *Server) refuseIngress(w http.ResponseWriter, r *http.Request, route string, subj Subject, reason string) {
	body, _ := json.Marshal(map[string]string{"subject": subj.Name, "method": subj.Method, "reason": reason})
	if _, err := s.Store.Log().Append(r.Context(), evidence.Record{
		Tenant: s.Tenant, Scope: "route/" + route, Kind: "ingress-refused",
		Subject: route, Actor: subj.Name, Body: body,
	}); err != nil {
		httpErr(w, 500, fmt.Sprintf("refused (%s) and the refusal could not be recorded: %v", reason, err))
		return
	}
	httpErrCode(w, http.StatusForbidden, "auth_required", reason)
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
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	s.reopen(w, r, s.Queue.Resolve, queue.StateDone, func(ctx context.Context, id, actor, note string) error {
		// Read before Resolve appends its own record — the plan's newest
		// standing is still "failed" here, so Reason is the plan-failed
		// body, not the note this call is about to write over it with.
		st, _ := s.Plans.Standing(ctx, s.Tenant, id)
		if err := s.Plans.Resolve(ctx, s.Tenant, id, actor, note); err != nil {
			return err
		}
		if s.EscalationResolved != nil {
			s.EscalationResolved(ctx, id, actor, st.Reason, note)
		}
		return nil
	})
}

func (s *Server) retry(w http.ResponseWriter, r *http.Request) {
	s.reopen(w, r, s.Queue.Retry, queue.StatePending, func(ctx context.Context, id, actor, note string) error {
		return s.Plans.Retry(ctx, s.Tenant, id, actor, note)
	})
}

// reopen applies one of the two verbs off the exception queue. An origination
// whose planning failed is on that queue before any item exists, so the same
// verb reaches it through the plan store and answers in the item's shape.
func (s *Server) reopen(w http.ResponseWriter, r *http.Request,
	f func(context.Context, tenant.ID, string, queue.Identity, string) (queue.WorkItem, error),
	planned queue.State, g func(context.Context, string, string, string) error) {
	who, err := s.mustActor(r)
	if err != nil {
		refuseActor(w, err)
		return
	}
	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, http.StatusBadRequest, "a JSON body with a reason is required")
		return
	}
	id := r.PathValue("id")
	if ingress.IsID(id) {
		if s.Plans == nil {
			httpErr(w, http.StatusNotFound, "no plan store: "+id+" cannot be reopened here")
			return
		}
		if err := g(r.Context(), id, who.Name, req.Reason); err != nil {
			httpErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, queue.WorkItem{ID: id, Tenant: s.Tenant, Ingress: id, State: planned, Note: req.Reason})
		return
	}
	item, err := f(r.Context(), s.Tenant, id, who, req.Reason)
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
	who, err := s.mustActor(r)
	if err != nil {
		refuseActor(w, err)
		return
	}
	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		httpErr(w, 400, "a reason is required — it becomes the record")
		return
	}
	if err := f(r.Context(), s.Tenant, who.Name, req.Reason); err != nil {
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
	who, err := s.mustActor(r)
	if err != nil {
		refuseActor(w, err)
		return
	}
	summary, err := s.Reload(r.Context(), who.Name)
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
