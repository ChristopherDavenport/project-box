// Package daemon assembles a running boxd from box.yaml — and reassembles it
// without a restart. A reload swaps the executor table, the route table, the
// budget, and the tick interval under locks; in-flight runs finish on the
// configuration they started with, and the next tick sees the new world.
//
// Not everything reloads. The store, the mirror root, the tenant, and the
// listener are the daemon's IDENTITY, not its configuration: changing them
// hot would strand state mid-flight (a half-written store, leases in one
// database and workspaces keyed to another), so a reload that touches them
// is refused with the old configuration left running. Every successful
// reload is evidence — who, when, and what changed shape.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/approval"
	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/harness"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/pump"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
	"github.com/ChristopherDavenport/project-box/web"

	"gopkg.in/yaml.v3"
)

// SpendOf carries an agent's usage across the seam to the meter, whole.
//
// It exists so the conversion is written once. The adapter reports four token
// counters and a cost and deliberately knows nothing about budgets, and the
// meter is denominated in exactly those; collapsing them here — which is what
// this used to do, with u.Total() — threw the split and the money away one
// line before the only thing that could have used them, and left a charge
// record no later reader could re-price.
func SpendOf(u acp.Usage) capacity.Spend {
	s := capacity.Spend{
		Input: u.InputTokens, Output: u.OutputTokens,
		CacheRead: u.CachedReadTokens, CacheWrite: u.CachedWriteTokens,
		Total: u.Tokens, CostMicros: capacity.Micros(u.CostUSD),
		Reported: u.Reported, Aborted: u.Aborted,
	}
	if u.RateLimit != nil {
		s.Limit = &capacity.Limit{Status: u.RateLimit.Status, Window: u.RateLimit.Window,
			Utilization: u.RateLimit.Utilization, ResetsAt: u.RateLimit.ResetsAt}
	}
	return s
}

// ShellBuilder runs a configured command in the workspace — the stand-in for
// an ACP agent, and the loop cannot tell.
type ShellBuilder struct{ Cmd string }

// Build runs the command and reports the tree's head.
func (b ShellBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if b.Cmd == "" {
		return "", fmt.Errorf("daemon: no build command configured")
	}
	c := gate.Command{Label: "build", Dir: ws.Dir, Commands: [][]string{{"sh", "-c", b.Cmd}}}
	v, err := c.Evaluate(ctx, forge.ChangeSet{})
	if err != nil {
		return "", err
	}
	if v.Decision != forge.DecisionPass {
		return "", fmt.Errorf("builder command failed: %s", firstLine(v.Reason))
	}
	out, err := gitforge.RevParse(ctx, ws.Dir, "HEAD")
	return forge.Commit(out), err
}

// Daemon is a running boxd.
type Daemon struct {
	ConfigPath string

	store     *sqlite.Store
	tenant    tenant.ID
	mirror    *gitforge.Mirror
	queue     *queue.Queue
	pump      *pump.Pump
	meter     *capacity.Meter
	escalator *control.Escalator
	// live is the daemon's window into runs in flight, and belongs to the
	// daemon rather than to an assembled executor: a reload rebuilds the
	// builders and must not blind the room to the runs those builders are
	// already in the middle of.
	live      *control.Live
	compactor *evidence.Compactor
	API       *api.Server
	// planners is the judgement decomposer, registered once and filled per
	// box at every assemble — a box with an agent has a planner, a box
	// without one has none, and a declaration asking for one there is
	// refused at load.
	planners *acp.Planners
	plans    *plan.Store
	// router is the one StoreRouter every ingress-opening path shares — the
	// API's hand-run submit and the standing poller alike — so redelivery
	// keys the same way whichever door work came through.
	router *ingress.StoreRouter
	// poller reads a route's forge for its polled issues; its Lister is the
	// daemon's own GitHub reader, swappable in a test.
	poller *ingress.Poller
	// landProbe asks GitHub what scopes the land token carries, at every
	// assemble of a repo configured for pull_request landing — the same
	// swappable-in-a-test shape as poller's Lister above.
	landProbe *landTokenScopeProbe
	// escalationCloser closes the GitHub issue a resolved escalation's
	// origination named, when its box lands by pull request. The same
	// GET/comment-dedupe/PATCH sequence git.PRLander's own closeDrivingIssue
	// performs, duplicated here rather than exported across the section
	// boundary between the daemon and the forge/git packages — see
	// ghOwnerRepo's own comment for the precedent.
	escalationCloser *escalationCloser
	// routes and remotes are swapped together at every assemble (New and
	// Reload), guarded by mu below. remotes carries the repo remote a
	// route's poll targets — ingress.Route itself does not, since a route
	// is forge-agnostic and a remote is not. landPR carries which of those
	// routes' repos land by pull request — the fact [Daemon.resolveEscalatedIssue]
	// needs to know before it will close anything.
	routes  map[string]ingress.Route
	remotes map[string]string
	landPR  map[string]bool
	// lastPoll is the last time each named route was polled, so a route's
	// own Poll interval — not the daemon's tick — paces the reader. It
	// survives a reload: a route that keeps its name keeps its schedule.
	// Guarded by mu, same as routes and remotes above.
	lastPoll map[string]time.Time
	// selfDeploy remembers commits the standing self-deploy chore has already
	// refused, so a bad commit is not re-attempted every housekeeping pass —
	// the refusal itself is already on the chain.
	selfDeploy *selfDeployState
	// lastPass is this daemon's own most recent self-deploy pass, set by
	// [Daemon.recordSelfDeployPass] at the moment that pass's record is
	// appended. Guarded by mu, same as lastPoll above. Single-valued rather
	// than per-box, mirroring followBranchForAPI's own targets[0] convention
	// — /v1/status has no shape for more than one box's self-deploy story.
	lastPass selfDeployPassInfo
	// Exec re-execs the process on the same argv and environment, completing
	// a self-deploy once Run has returned [ErrSelfDeployed] and the caller
	// has done its own shutdown (closing the store, letting the API finish
	// draining). Nil is a no-op: only cmd/boxd, which owns argv and the
	// syscall, ever sets it — a Daemon nobody wired for self-deploy has
	// nothing to call.
	Exec func() error
	// exePath resolves the running binary's own path — os.Executable in
	// production. A test substitutes a path of its own so a self-deploy run
	// never renames over the test binary.
	exePath func() (string, error)
	// build reports this binary's own build fact — version.ThisBuild in
	// production. `go test` does not stamp VCS info the way `go build` does,
	// so a test substitutes a fixed fact rather than depending on how the
	// test binary itself happened to be built.
	build func() version.Build
	// bootIDPath is where the host's boot id is read from at start —
	// /proc/sys/kernel/random/boot_id in production. A test substitutes a
	// path of its own, including one that does not exist, to exercise the
	// unreadable case without depending on the real kernel file.
	bootIDPath string
	// suspend is the one [loop.SuspendWatch] this daemon owns, threaded into
	// every [loop.Loop] assemble builds so a single Observe call per tick (in
	// Run) extends every in-flight run's leases and deadlines the same way,
	// regardless of which box or repo they belong to. Survives Reload —
	// assemble reads it off d rather than constructing a fresh one — so a
	// reload never resets the wall/mono baseline mid-suspend-detection.
	suspend *loop.SuspendWatch
	// pulse is the one [loop.Heartbeat] this daemon owns, threaded into
	// every [loop.Loop] assemble builds so d.progress's per-frame call
	// renews every in-flight run's leases the same way, regardless of which
	// box or repo it belongs to. Survives Reload — assemble reads it off d
	// rather than constructing a fresh one — so a reload never resets a
	// run's renewal state mid-run (issue #211).
	pulse *loop.Heartbeat

	mu    sync.RWMutex
	every time.Duration
	cfg   boxcfg.Config
}

// unboundedClaimTTL is the claim lease TTL when build_timeout declares no
// bound. A lease has to be finite even when a build does not — Claim and
// Retain (internal/queue) have no way to express "forever" — so this is a
// generous static margin. Issue #211's renew-on-progress (d.pulse, driven
// from d.progress) now extends a run's claim on every frame it streams, but
// a gate.Command check gate streams no frames while its subprocess runs, so
// a run's silent stretches still lean on this margin rather than on Pulse.
const unboundedClaimTTL = 24 * time.Hour

// claimTTL derives the claim lease TTL from the same build_timeout the loop
// itself is bounded by, so the queue's Claim grant (New, below) and the
// assembled Loop's Retain renew (assemble, below) are always read off one
// clock. Before this they were two independent 30m literals, equal only by
// coincidence with the build timeout's own 30m default — a unit that spent
// its whole build budget reached review holding an already-expired lease,
// and queue.Recover reaped a live run out from under it (issue #267).
func claimTTL(cfg boxcfg.Config) time.Duration {
	switch d := cfg.BuildTimeoutDuration(); {
	case d > 0:
		// Headroom past the build itself for review and land, not just the
		// build — 3x is the shape #267 names.
		return 3 * d
	case d == 0:
		// Empty config: the loop falls back to loop.DefaultBuildTimeout, so
		// the lease follows it there too. Never zero — that would expire
		// the lease the instant it was granted.
		return 3 * loop.DefaultBuildTimeout
	default:
		// Negative is boxcfg's spelling of "no bound" (see Config.BuildTimeout's
		// doc). The lease still can't be infinite, so it gets the static
		// margin above instead.
		return unboundedClaimTTL
	}
}

// New assembles a daemon from its config file.
func New(path string) (*Daemon, error) {
	cfg, err := boxcfg.Load(path)
	if err != nil {
		return nil, err
	}
	t := tenant.ID(cfg.Tenant)
	s, err := sqlite.Open(cfg.DB)
	if err != nil {
		return nil, err
	}
	m, err := gitforge.NewMirror(cfg.Root, t, s.Leaser(cfg.Runner))
	if err != nil {
		s.Close()
		return nil, err
	}
	router := &ingress.StoreRouter{Store: s, Tenant: t, Runner: cfg.Runner}
	d := &Daemon{
		ConfigPath: path, store: s, tenant: t, mirror: m,
		queue:      &queue.Queue{Store: s, Runner: cfg.Runner, TTL: claimTTL(cfg)},
		meter:      &capacity.Meter{Store: s, Tenant: t, Actor: cfg.Runner},
		live:       &control.Live{},
		planners:   &acp.Planners{},
		plans:      &plan.Store{Log: s.Log(), Actor: cfg.Runner},
		selfDeploy: &selfDeployState{},
		exePath:    os.Executable,
		build:      version.ThisBuild,
		bootIDPath: defaultBootIDPath,
		suspend:    &loop.SuspendWatch{},
		pulse:      &loop.Heartbeat{},
		router:     router,
		poller: &ingress.Poller{
			Lister: newGHIssueLister(ghAuthToken), Router: router, Log: s.Log(),
			Tenant: t, Actor: cfg.Runner,
		},
		landProbe:        newLandTokenScopeProbe(ghAuthToken),
		escalationCloser: newEscalationCloser(ghAuthToken),
		lastPoll:         map[string]time.Time{},
		cfg:              cfg, every: cfg.EveryDuration(),
	}
	// The deterministic, agent-free set, plus the one judgement this daemon
	// can offer. Registering it here does not put an agent on the intake
	// path: nothing runs it until a kind declaration names it, and a box
	// that names it without an agent is refused at assemble.
	decomposers := plan.Default()
	if err := decomposers.Register(d.planners); err != nil {
		s.Close()
		return nil, err
	}
	loops, routes, remotes, landPR, kinds, replanner, err := d.assemble(context.Background(), cfg)
	if err != nil {
		s.Close()
		return nil, err
	}
	d.meter.SetBudget(capacity.Budget{MaxConcurrent: cfg.Budget.MaxConcurrent,
		MaxJudgements:      cfg.Budget.MaxJudgements,
		MaxPerWindow:       cfg.Budget.MaxPerWindow,
		MaxTokensPerWindow: cfg.Budget.MaxTokensPerWindow,
		MaxCostPerWindow:   cfg.MaxCostMicros(),
		AllowUnmetered:     cfg.Budget.AllowUnmetered, Window: cfg.WindowDuration(),
		WindowReserve: cfg.Budget.WindowReserve,
		Credential:    capacity.CredentialKind(cfg.Budget.Credential)})
	// The window's account outlives the process that spent it. Without this a
	// restart is a fresh budget, so the brake is only ever as strong as the
	// daemon's uptime.
	if err := d.meter.Restore(context.Background()); err != nil {
		s.Close()
		return nil, fmt.Errorf("daemon: restoring the capacity window: %w", err)
	}
	d.pump = &pump.Pump{Store: s, Tenant: t, Runner: cfg.Runner, Queue: d.queue, Meter: d.meter, Loops: loops,
		Kinds:       kinds,
		Decomposers: decomposers,
		Plans:       d.plans,
		Replanner:   replanner,
	}
	d.routes = routes
	d.remotes = remotes
	d.landPR = landPR
	// The exception queue ages whether or not anybody is looking, so the
	// sweep is what makes "nobody is looking" audible. The log is the weakest
	// sink there is and the only one that needs no configuration; a real
	// deployment replaces it at this seam without the daemon changing.
	d.escalator = &control.Escalator{
		Room:   &control.Room{Store: s, Tenant: t, Actor: cfg.Runner},
		Notify: control.NotifierFunc(control.LogNotice),
		Kinds:  kinds,
	}
	// Compaction is a standing chore beside the prune and the escalation
	// sweep, and it is imperative Go for the reason those are: a compactor
	// that quietly stopped is a store that grows until it is an incident.
	// Nil Retention means it does nothing, which is the configured default.
	d.compactor = &evidence.Compactor{
		Log:   s.Log(),
		Sink:  &evidence.DirSink{Dir: cfg.RetentionDir(), Log: s.Log()},
		Actor: cfg.Runner,
		Retention: evidence.Retention{
			Keep: cfg.Retention.Keep, KeepFor: cfg.Retention.KeepForDuration(),
			MinPrune: cfg.Retention.MinPrune,
		},
	}
	d.API = &api.Server{
		Store: s, Tenant: t, Routes: routes,
		Router: router,
		Queue:  d.queue, Meter: d.meter, Box: cfg.Boxes[0].Name, Runner: cfg.Runner,
		Sections:    declaredSections(cfg),
		Plans:       d.plans,
		Live:        d.live,
		Started:     time.Now(),
		Reload:      d.reloadForAPI,
		ActorClaims: cfg.ActorClaims,
		Auth:        authenticatorFromConfig(cfg.Auth),
		// A closure rather than d.build directly: the latter would capture
		// today's function value, so a later swap of d.build (only ever done
		// by a test) would leave the API reporting the production seam
		// forever.
		Build:        func() version.Build { return d.build() },
		FollowBranch: d.followBranchForAPI,
		LastPass:     d.lastPassForAPI,
		// A person resolving an escalation and a runner landing a plan's
		// last unit are the two ways an issue closes; this is the seam for
		// the first, invoked only once Plans.Resolve itself has succeeded.
		EscalationResolved: d.resolveEscalatedIssue,
	}
	if ui, err := web.Dist(); err == nil {
		d.API.UI = ui
	}
	return d, nil
}

// Close releases the daemon's resources.
func (d *Daemon) Close() error { return d.store.Close() }

// declaredSections builds GET /v1/sections' per-repo declared names from the
// box configuration (issue #200) — [api.Server.Sections] needs these so a
// section nobody has landed anything in yet still appears in the census, at
// zero, rather than being absent because nothing folded ever mentioned it.
// Keyed by repo across every box the config declares, since a repo's
// section names are its own regardless of which box builds it.
func declaredSections(cfg boxcfg.Config) map[string][]string {
	out := map[string][]string{}
	for _, bx := range cfg.Boxes {
		for _, r := range bx.Repos {
			names := make([]string, 0, len(r.Sections))
			for _, s := range r.Sections {
				names = append(names, s.Name)
			}
			out[r.Name] = names
		}
	}
	return out
}

// authenticatorFromConfig builds the API's [api.Authenticator] from a box's
// auth config — used at New and again at every Reload, the same shape as
// declaredSections above. cfg.Auth has already been through
// [boxcfg.Auth.validate] by the time Load returns it, which normalizes an
// absent method to "peer" and resolves every token's secret, so the switch
// below only ever sees one of the three known methods.
func authenticatorFromConfig(a boxcfg.Auth) api.Authenticator {
	switch a.Method {
	case boxcfg.AuthMethodToken:
		tokens := make(map[string]string, len(a.Tokens))
		for _, t := range a.Tokens {
			tokens[t.Name] = t.Secret
		}
		return api.TokenAuthenticator{Tokens: tokens}
	case boxcfg.AuthMethodOIDC:
		return &api.OIDCAuthenticator{Issuer: a.OIDC.Issuer, Audience: a.OIDC.Audience, JWKSURL: a.OIDC.JWKSURL}
	case boxcfg.AuthMethodNone:
		return api.NoneAuthenticator{}
	default: // boxcfg.AuthMethodPeer
		return api.PeerAuthenticator{}
	}
}

// assemble builds the executor, route and declaration tables from a config —
// used at New and again at every Reload.
func (d *Daemon) assemble(ctx context.Context, cfg boxcfg.Config) (map[string]*loop.Loop, map[string]ingress.Route, map[string]string, map[string]bool, kind.Sets, plan.Replanner, error) {
	loops := map[string]*loop.Loop{}
	routes := map[string]ingress.Route{}
	landPR := map[string]bool{}
	remotes := map[string]string{}
	kinds := kind.Sets{}
	// replanner is the pump's single [plan.Replanner] hook, wired from the
	// same per-box [acp.Planner] this loop registers into d.planners below —
	// see issue #182. A box with no agent never sets it; among boxes that do,
	// the field stays one value for the whole daemon, so the last one wins,
	// same as [pump.Pump.Replanner] itself being one field rather than one
	// per box.
	var replanner plan.Replanner
	for _, bx := range cfg.Boxes {
		set, err := d.declarations(ctx, bx)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		kinds[bx.Name] = set
		if len(bx.Agent) == 0 {
			// A declaration asking for judgement in a box with nobody to
			// judge would fail every origination it admitted, one exception
			// at a time. Refused here, where the operator can see it coming.
			for _, name := range set.Names() {
				if decl, err := set.Lookup(name); err == nil && decl.Decomposer() == acp.PlannerName {
					return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: box %s kind %s names the planner decomposer and the box has no agent — a build command cannot judge", bx.Name, name)
				}
			}
		}
		var agentBuilder *acp.Builder
		if len(bx.Agent) > 0 {
			agentBuilder = &acp.Builder{
				Store: d.store, Tenant: d.tenant, Box: bx.Name,
				Command:   bx.Agent,
				EnvNames:  forwardedEnv(bx),
				MaxTokens: cfg.Budget.MaxTokensPerRun,
				Composer: &agent.Composer{Source: &harness.Source{
					Base: []agent.Layer{{
						Name: "builder", Scope: agent.ScopeRole,
						Source:       agent.Source{Kind: agent.SourceInline, Digest: "boxd-default-role"},
						Instructions: "You are a build agent in an automated factory. Make the requested change precisely and minimally.",
						Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
					}},
					Box:       bx.Name,
					Inherit:   bx.Harness.Plugins == boxcfg.PluginsInherit,
					PluginMCP: bx.Harness.PluginMCP,
					Skills:    skillRefs(bx.Harness.Skills),
					MCP:       mcpRefs(bx.Harness.MCP),
					Resolver:  d.mirror,
					Model:     bx.Model,
					Session:   bx.Session,
					Roles:     roleTiers(bx.Roles),
				}},
				Seat:            &seat.Seat{Name: bx.Seat, Base: &knowledge.Base{Store: d.store, Tenant: d.tenant}},
				Mirror:          d.mirror,
				InheritSettings: bx.Harness.Plugins == boxcfg.PluginsInherit,
				// The stream is the only place the cost is visible, so it is
				// the adapter that charges the meter.
				Spend: func(ctx context.Context, run string, u acp.Usage) {
					d.meter.Charge(ctx, run, SpendOf(u))
				},
				// And the same argument for the stream: the adapter is the
				// only place a run is visible while it is running, so it is
				// where the room's live view has to be fed from.
				Progress: d.progress,
				// The kind declaration reaching the agent plane, which is the
				// seat #17 built and could not fill. Per run, because the
				// envelope belongs to the origination and one builder serves a
				// whole box.
				EnvelopeFor: d.envelopeFor(set),
			}
		}
		var repos []forge.Repo
		for _, r := range bx.Repos {
			repo := r.ForgeRepo()
			repos = append(repos, repo)
			if err := d.mirror.Fetch(ctx, repo); err != nil {
				return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: fetch %s/%s: %w", bx.Name, r.Name, err)
			}
			check := r.Check
			if r.Review == boxcfg.ReviewJudgement && agentBuilder == nil {
				// Refused at Load (boxcfg.validate): review: judgement with no
				// agent never reaches here. A nil agentBuilder in this branch
				// is this assembly disagreeing with that refusal, not a
				// config an operator could have written.
				return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: box %s repo %s: review: judgement with no agent builder — should have been refused at Load", bx.Name, r.Name)
			}
			reviews := r.Review == boxcfg.ReviewJudgement
			lp := &loop.Loop{
				Store: d.store, Runner: d.cfgRunner(), Mirror: d.mirror, Repo: repo, Branch: r.Branch,
				Lane: forge.Lane(r.Lane), Meter: d.meter, Widen: bx.Widen, Suspend: d.suspend, Pulse: d.pulse,
				Gates: func(ws forge.Workspace) []forge.Gate {
					gates := []forge.Gate{gate.Command{Label: "check", Dir: ws.Dir,
						Commands: [][]string{{"sh", "-c", check}}}}
					if reviews {
						// After the command gate: paying for an agent to read
						// a diff that did not build is what the loop's
						// stop-at-first-fail exists to avoid, but the gate
						// SET still names the reviewer second so the order is
						// on the record rather than incidental.
						gates = append(gates, &acp.Reviewer{
							Store: d.store, Tenant: d.tenant, Box: bx.Name,
							Command:         bx.Agent,
							EnvNames:        forwardedEnv(bx),
							MaxTokens:       cfg.Budget.MaxTokensPerRun,
							Composer:        agentBuilder.Composer,
							Mirror:          d.mirror,
							Seat:            &seat.Seat{Name: bx.Seat + "-gate", Base: &knowledge.Base{Store: d.store, Tenant: d.tenant}},
							Repo:            repo,
							Workspace:       ws,
							Root:            filepath.Join(cfg.Root, "reviews"),
							InheritSettings: bx.Harness.Plugins == boxcfg.PluginsInherit,
							Spend: func(ctx context.Context, run string, u acp.Usage) {
								d.meter.Charge(ctx, run, SpendOf(u))
							},
							Progress: d.progress,
						})
					}
					return gates
				},
				TTL:          claimTTL(cfg),
				BuildTimeout: cfg.BuildTimeoutDuration(),
			}
			// Repo wins over box; absent on both leaves lp.Attempts zero,
			// which the loop itself resolves to its own default — see
			// [boxcfg.Repo.Attempts].
			switch {
			case r.Attempts != nil:
				lp.Attempts = *r.Attempts
			case bx.Attempts != nil:
				lp.Attempts = *bx.Attempts
			}
			// Repo wins over box; absent on both leaves lp.GateRetries
			// zero, which the loop itself resolves to its own default —
			// see [boxcfg.Repo.GateRetries].
			switch {
			case r.GateRetries != nil:
				lp.GateRetries = *r.GateRetries
			case bx.GateRetries != nil:
				lp.GateRetries = *bx.GateRetries
			}
			// Repo wins over box; absent on both leaves lp.LandRetries
			// zero, which the loop itself resolves to its own default —
			// see [boxcfg.Repo.LandRetries].
			switch {
			case r.LandRetries != nil:
				lp.LandRetries = *r.LandRetries
			case bx.LandRetries != nil:
				lp.LandRetries = *bx.LandRetries
			}
			if r.Land == boxcfg.LandPullRequest {
				// The token round 11's #68 landing found out was missing
				// `workflow` only at the remote's rejection. Probed and
				// recorded before the wiring decides anything — the record
				// is the answer, the refusal below is a consequence of it,
				// not a replacement for it.
				scopes := d.landProbe.probe(ctx, r.Remote)
				body, _ := json.Marshal(scopes)
				if _, err := d.store.Log().Append(ctx, evidence.Record{
					Tenant: d.tenant, Scope: "control/config", Kind: "land-token-scopes",
					Subject: bx.Name + "/" + r.Name, Actor: cfg.Runner, Body: body,
				}); err != nil {
					return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: box %s repo %s: recording land-token scopes: %w", bx.Name, r.Name, err)
				}
				// Every indefinite outcome — a non-GitHub remote, a token or
				// network failure, an absent X-OAuth-Scopes header, a status
				// other than 200/404 — is recorded above as unanswered and
				// does not refuse: boxd stays local-first and keeps starting
				// offline. Only a definite gap refuses, and only here, at
				// Load-equivalent time — the wire-side assertion this same
				// plan adds on the forge side still refuses before any push
				// either way, so an unanswered probe is never the last word.
				if scopes.missingWorkflowScope() {
					return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: box %s repo %s: land token scopes (%s) lack \"workflow\", and %s has a .github/workflows directory — run: gh auth refresh -s workflow", bx.Name, r.Name, scopes.Scopes, r.Remote)
				}
				// Closing a driving issue — on plan completion, or a
				// resolved escalation — needs the same token's issues
				// write. Classic OAuth carries it on "repo" (or
				// "public_repo" for a public repository), never a
				// standalone "issues" scope, so an answered list lacking
				// both is the definite gap, asserted here for the same
				// reason the workflow one is: at Load, not at the first
				// close's own rejection.
				if scopes.missingIssuesScope() {
					return nil, nil, nil, nil, nil, nil, fmt.Errorf("daemon: box %s repo %s: land token scopes (%s) lack \"repo\" or \"public_repo\" — issue closing needs issues write — run: gh auth refresh -s repo", bx.Name, r.Name, scopes.Scopes)
				}
				// Validated at Load: pull_request landing does not reach here
				// without a known land_token, and "gh" is the only one today.
				lp.PRLander = func(inner *gitforge.Lander) forge.Lander {
					// The gh session's token is an OAuth token, and a check
					// run is a GitHub App's to create; statuses until the
					// App (Phase 7) is what signs the landing.
					return gitforge.NewPRLander(inner, ghAuthToken).StatusesOnly()
				}
				landPR[r.Route.Name] = true
			}
			if r.Approval.Count > 0 {
				// Validated at Load: a count needs land: pull_request and a
				// non-optimistic lane, so this is never reached without
				// somewhere for the review to be. A count of zero leaves
				// lp.Approval nil BY CONSTRUCTION, not by a gate that always
				// passes — today's loop must not change shape at all when no
				// human boundary is configured.
				pr := gitforge.NewPRLander(gitforge.NewLander(d.mirror, d.store.Log()), ghAuthToken).StatusesOnly()
				sources := make([]approval.Source, 0, len(r.Approval.Sources))
				for _, s := range r.Approval.Sources {
					sources = append(sources, approval.Source(s))
				}
				requirement := approval.Requirement{Count: r.Approval.Count, Sources: sources}
				runner := d.cfgRunner()
				lp.Approval = func(item queue.WorkItem) forge.Gate {
					return &humanApprovalGate{
						requirement: requirement,
						requester:   loop.RequesterActor(runner, item),
						ensure:      pr.EnsureRequest,
						reviews:     pr.Reviews,
					}
				}
			}
			if agentBuilder != nil {
				lp.Builder, lp.Handoff = agentBuilder, agentBuilder.Handoff
			} else {
				lp.Builder = ShellBuilder{Cmd: r.Build}
			}
			loops[bx.Name+"/"+r.Name] = lp
			routes[r.Route.Name] = ingress.Route{
				Name: r.Route.Name, Kind: ingress.RouteIssue, Opens: kind.SeedIssue,
				Box: bx.Name, Repo: r.Name, Requester: cfg.Runner,
				Poll: r.Route.PollDuration(), Label: r.Route.Label, Contract: r.Route.Contract,
				Require: r.Route.Require,
			}
			remotes[r.Route.Name] = r.Remote
		}
		if agentBuilder == nil {
			d.planners.Delete(bx.Name)
			continue
		}
		// The builder describes the sections the loop leases; both read the
		// same repository definition. And the planner: the same agent, the
		// same harness source under the planner role, its own seat for
		// continuity, its own read-only trees, the same meter.
		agentBuilder.Repos = repos
		boxPlanner := &acp.Planner{
			Store: d.store, Tenant: d.tenant, Box: bx.Name,
			Command: bx.Agent, EnvNames: forwardedEnv(bx), MaxTokens: cfg.Budget.MaxTokensPerRun,
			Composer: agentBuilder.Composer,
			Seat:     &seat.Seat{Name: bx.Seat + "-planner", Base: &knowledge.Base{Store: d.store, Tenant: d.tenant}},
			Mirror:   d.mirror, Repos: repos,
			Root:            filepath.Join(cfg.Root, "plans"),
			InheritSettings: bx.Harness.Plugins == boxcfg.PluginsInherit,
			Spend: func(ctx context.Context, run string, u acp.Usage) {
				d.meter.Charge(ctx, run, SpendOf(u))
			},
			Progress: d.progress,
		}
		d.planners.Set(bx.Name, boxPlanner)
		// Widen answers a structured escalation in-loop when it can; when it
		// cannot, [pump.Pump.Replan] is what asks a planner to revise the
		// unit instead of parking it for a person, and this is the only
		// planner instance a box with an agent has to offer it.
		replanner = boxPlanner
	}
	return loops, routes, remotes, landPR, kinds, replanner, nil
}

// declarations resolves one box's kind declarations: the seeds, then the
// box's own on top of them, then the whole set validated against the box's
// ceiling.
//
// A box declaration REPLACES a seed of the same name whole rather than merging
// with it. Merging would be how a default quietly becomes a standard nobody
// chose — a field the operator never wrote going on granting whatever the seed
// granted, with the diff showing nothing.
func (d *Daemon) declarations(ctx context.Context, bx boxcfg.Box) (*kind.Set, error) {
	decls := kind.Seeds()
	for _, k := range bx.Kinds {
		decl, err := d.declaration(ctx, bx, k)
		if err != nil {
			return nil, err
		}
		decls = append(decls, decl)
	}
	set, err := kind.NewSet(bx.Capabilities, decls...)
	if err != nil {
		return nil, fmt.Errorf("daemon: box %s: %w", bx.Name, err)
	}
	return set, nil
}

// declaration resolves one configured kind, inline or pinned.
func (d *Daemon) declaration(ctx context.Context, bx boxcfg.Box, k boxcfg.Kind) (kind.Declaration, error) {
	if k.Ref == nil {
		return k.Declaration(k.InlineSource(d.ConfigPath))
	}
	body, oid, err := d.mirror.ReadPinned(ctx, k.Ref.Repo, k.Ref.Commit, k.Ref.Path)
	if err != nil {
		return kind.Declaration{}, fmt.Errorf("daemon: box %s kind %s: %w", bx.Name, k.Name, err)
	}
	var pinned boxcfg.Kind
	if err := yaml.Unmarshal(body, &pinned); err != nil {
		return kind.Declaration{}, fmt.Errorf("daemon: box %s kind %s: %s at %s: %w", bx.Name, k.Name, k.Ref.Path, k.Ref.Commit, err)
	}
	if pinned.Ref != nil {
		return kind.Declaration{}, fmt.Errorf("daemon: box %s kind %s: the pinned declaration is itself a ref — a chain of pins is a pin whose content nobody reviewed at the end of it", bx.Name, k.Name)
	}
	// The NAME comes from box.yaml, not from the pinned file. Which kind a
	// declaration answers to is the operator's routing decision and belongs in
	// the file the operator controls; letting the remote choose it would mean
	// a commit in someone else's repository could silently take over "issue".
	pinned.Name = k.Name
	return pinned.Declaration(agent.Source{
		Kind: agent.SourceGit, Repo: k.Ref.Repo, Commit: k.Ref.Commit,
		Path: k.Ref.Path, Digest: oid,
	})
}

// envelopeFor resolves a run's declared data envelope through its work item's
// kind.
//
// An item with no kind resolves to no envelope rather than to an error. Items
// submitted before there was a decomposition stage carry none, and refusing
// them would stop a running factory on a field they could not have had — while
// a box that has turned classification ON still refuses them, because the
// composer's ceiling meets an undeclared envelope and says so. The permissive
// case stays exactly as permissive as it was; the strict case stays strict.
func (d *Daemon) envelopeFor(set *kind.Set) func(context.Context, string) (*agent.Envelope, error) {
	return func(ctx context.Context, run string) (*agent.Envelope, error) {
		item, err := d.queue.Load(ctx, d.tenant, run)
		if err != nil {
			return nil, err
		}
		if item.Kind == "" {
			return nil, nil
		}
		decl, err := set.Lookup(item.Kind)
		if err != nil {
			return nil, err
		}
		return decl.Envelope, nil
	}
}

// skillRefs turns configured pins into unresolved refs. Resolution — the
// commit-to-tree lookup that fills Digest — happens per run in the layer
// source, not here: a digest captured at assemble time would be a digest that
// stops matching the moment the pin is edited and reloaded.
func skillRefs(skills []boxcfg.Skill) []agent.SkillRef {
	out := make([]agent.SkillRef, 0, len(skills))
	for _, s := range skills {
		out = append(out, agent.SkillRef{Name: s.Name, Source: agent.Source{
			Kind: agent.SourceGit, Repo: s.Repo, Commit: s.Commit, Path: s.Path,
		}})
	}
	return out
}

// roleTiers is box.yaml's roles block in the harness's form. Load validates
// the block; this is what makes it count. Without it every role ran on the
// box's own tier, and the record could not say so: a session option nobody
// asked for is never asserted, so the effort the roles named was read back
// from the agent as whatever the machine's settings said.
func roleTiers(roles map[string]boxcfg.Tier) map[agent.Role]harness.Tier {
	if len(roles) == 0 {
		return nil
	}
	out := make(map[agent.Role]harness.Tier, len(roles))
	for name, t := range roles {
		out[agent.Role(name)] = harness.Tier{Model: t.Model, Session: t.Session}
	}
	return out
}

func mcpRefs(servers []boxcfg.MCPServer) []agent.MCPRef {
	out := make([]agent.MCPRef, 0, len(servers))
	for _, m := range servers {
		out = append(out, agent.MCPRef{Name: m.Name, Command: m.Command, URL: m.URL, Tools: m.Tools})
	}
	return out
}

// forwardedEnv is the set of environment variable names an agent process
// spawned for this box may see, on top of [acp.Runtime]'s own built-in base
// set: the box's own harness.env, plus a confine block's env when the box
// declares one.
//
// The confine case is not optional. Load renders bx.Confine into bx.Agent as
// `docker run --env NAME ...` (see [boxcfg.Config.validate]), and docker does
// not error when NAME is absent from ITS OWN environment — it simply starts
// the container without it. An agent wrapped in confine and left out of this
// join would lose its model credential silently, which is exactly the
// "unknown reads as zero" failure this issue exists to close.
//
// The lander's bearer token is deliberately not in this set: ghAuthToken
// shells out to `gh auth token` from the DAEMON's own process, and
// git.PRLander calls the GitHub REST API directly from Go — the token never
// crosses into an agent's environment, so there is no name here for it to
// forward.
func forwardedEnv(bx boxcfg.Box) []string {
	names := append([]string{}, bx.Harness.Env...)
	if bx.Confine != nil {
		names = append(names, bx.Confine.Env...)
	}
	return names
}

// Reload re-reads the config file and swaps the daemon's shape. Identity
// fields must not change; on any error the old configuration keeps running.
func (d *Daemon) Reload(ctx context.Context, actor string) (string, error) {
	ncfg, err := boxcfg.Load(d.ConfigPath)
	if err != nil {
		return "", fmt.Errorf("daemon: reload refused, old config still running: %w", err)
	}
	d.mu.RLock()
	old := d.cfg
	d.mu.RUnlock()
	for _, f := range []struct{ name, was, now string }{
		{"tenant", old.Tenant, ncfg.Tenant},
		{"db", old.DB, ncfg.DB},
		{"root", old.Root, ncfg.Root},
		{"listen", old.Listen, ncfg.Listen},
	} {
		if f.was != f.now {
			return "", fmt.Errorf("daemon: %s is the daemon's identity, not its configuration — changing it (%q -> %q) needs a restart; reload refused, old config still running", f.name, f.was, f.now)
		}
	}
	loops, routes, remotes, landPR, kinds, replanner, err := d.assemble(ctx, ncfg)
	if err != nil {
		return "", fmt.Errorf("daemon: reload refused, old config still running: %w", err)
	}
	// d.queue is deliberately left holding the TTL claimTTL(old) computed at
	// New — not bumped to claimTTL(ncfg) here. Every assembled Loop above
	// picks up the new TTL for its own Retain renews, but queue.Queue has no
	// lock of its own around TTL (Claim and Retain, in internal/queue, read
	// it unsynchronized), and this daemon's mu guards Daemon's own fields,
	// not a package it does not own. Writing d.queue.TTL here would race
	// against an in-flight Claim on another goroutine with nothing on the
	// read side to order against it. A reload that changes build_timeout
	// therefore keeps granting the old claim TTL until the next restart;
	// bounded, and named here so it is a decision rather than a gap.

	d.pump.SetLoops(loops)
	d.pump.SetKinds(kinds)
	// Unguarded, same as d.escalator.Kinds below: [pump.Pump.Replanner] has
	// no setter because nothing reads it under Loops/Kinds' swap lock either
	// — a run already dispatched to replan keeps asking the planner it
	// started with, and the next one sees the new box's.
	d.pump.Replanner = replanner
	d.escalator.Kinds = kinds
	d.API.SetRoutes(routes)
	d.API.SetActorClaims(ncfg.ActorClaims)
	d.API.SetAuth(authenticatorFromConfig(ncfg.Auth))
	d.meter.SetBudget(capacity.Budget{MaxConcurrent: ncfg.Budget.MaxConcurrent,
		MaxJudgements:      ncfg.Budget.MaxJudgements,
		MaxPerWindow:       ncfg.Budget.MaxPerWindow,
		MaxTokensPerWindow: ncfg.Budget.MaxTokensPerWindow,
		MaxCostPerWindow:   ncfg.MaxCostMicros(),
		AllowUnmetered:     ncfg.Budget.AllowUnmetered, Window: ncfg.WindowDuration(),
		WindowReserve: ncfg.Budget.WindowReserve,
		Credential:    capacity.CredentialKind(ncfg.Budget.Credential)})
	d.poller.Actor = ncfg.Runner
	d.mu.Lock()
	d.cfg = ncfg
	d.every = ncfg.EveryDuration()
	d.routes = routes
	d.remotes = remotes
	d.landPR = landPR
	d.mu.Unlock()

	summary := fmt.Sprintf("boxes=%d executors=%d routes=%d every=%s", len(ncfg.Boxes), len(loops), len(routes), ncfg.Every)
	d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: "control/config", Kind: "config-reloaded",
		Subject: d.ConfigPath, Actor: actor, Body: []byte(summary),
	})
	return summary, nil
}

func (d *Daemon) reloadForAPI(ctx context.Context, actor string) (string, error) {
	return d.Reload(ctx, actor)
}

// Tick is one heartbeat, exported for tests and for the run loop.
func (d *Daemon) Tick(ctx context.Context) (int, error) { return d.pump.Tick(ctx) }

// Every reads the current tick interval — hot-reloadable, so the run loop
// asks every time rather than capturing it once.
func (d *Daemon) Every() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.every
}

// progress is where a run's stream lands, for builders and the planner alike:
// on the live bus, which the room watches, and in the run log, which the room
// reads back once the run is over. One moment stamps both, so a frame seen
// live and the same frame read later are the same frame. The log is
// telemetry: a write that fails is warned about and the run goes on, because
// a run that produced a change and no window on itself is still a run.
func (d *Daemon) progress(ctx context.Context, run string, u acp.Update) {
	at := time.Now().UTC()
	d.live.Append(run, control.Frame{
		At: at, Kind: string(u.Kind), Call: u.Call, Tool: u.Tool, Text: u.Text, Reason: u.Reason, Session: u.Session,
	})
	if _, err := d.store.Runs().Append(ctx, d.tenant, runlog.Frame{
		Run: run, At: at, Kind: string(u.Kind), Call: u.Call, Tool: u.Tool, Text: u.Text, Reason: u.Reason, Session: u.Session,
	}); err != nil {
		slog.Warn("run log", "run", run, "err", err)
	}
	// A pulse never fails or delays the frame: progress is telemetry, and
	// d.pulse's own logging covers renewal trouble (issue #211). A planner
	// run's id was never Tracked, so this is a no-op for it by construction.
	d.pulse.Pulse(ctx, run)
}

// runsKeepFor is the run log's age bound under the config in force; zero
// keeps everything.
func (d *Daemon) runsKeepFor() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.Runs.KeepForDuration()
}

func (d *Daemon) cfgRunner() string {
	return d.cfg.Runner
}

// Run serves the API and ticks until ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
	d.mu.RLock()
	listen := d.cfg.Listen
	d.mu.RUnlock()
	ln, err := api.Listen(listen)
	if err != nil {
		return err
	}
	// The listener is bound: this process has come up. A marker left by a
	// self-deploy that installed THIS binary is settled here, on the one
	// path [RecoverFromFailedStart] never reaches — that seam only runs when
	// startup fails before this point, and this only runs when it does not.
	d.resolveSelfDeployMarker(ctx)
	if err := d.recordStarted(ctx); err != nil {
		slog.Error("daemon-started: recording", "err", err)
	}
	httpSrv := &http.Server{Handler: d.API.Handler(), ConnContext: api.ConnContext}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server", "err", err)
		}
	}()
	defer func() {
		shctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		httpSrv.Shutdown(shctx)
	}()
	// Work runs on its own context so shutdown can DRAIN: the signal stops
	// new claims immediately, in-flight runs get the drain window to settle,
	// and only then is their context cancelled — the dockerd stop-timeout
	// shape.
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()

	slog.Info("boxd up", "tenant", d.tenant, "api", listen, "every", d.Every(), "drain", d.drain(), "housekeep_every", d.housekeepEvery())
	lastHousekeep := time.Now()
	for {
		select {
		case <-ctx.Done():
			slog.Info("draining", "grace", d.drain())
			dctx, cancel := context.WithTimeout(context.Background(), d.drain())
			clean := d.pump.Quiesce(dctx)
			cancel()
			cancelWork()
			if !clean {
				slog.Warn("drain window elapsed with work in flight — cancelled; fencing makes re-runs safe")
			}
			return ctx.Err()
		case <-time.After(d.Every()):
			// Observed first, before anything else this tick does: a
			// detected gap renews every in-flight run's leases and
			// deadlines (see [loop.Loop.Suspend]'s Subscribe wiring)
			// synchronously, inside Observe, so PollRoutes, Tick's own
			// reaper, and dispatch all see post-renewal state rather than
			// racing a suspend's aftermath.
			d.observeSuspend(workCtx)
			d.PollRoutes(workCtx, time.Now())
			if _, err := d.Tick(workCtx); err != nil {
				slog.Error("tick", "err", err)
			}
			now := time.Now()
			if housekeepingDue(lastHousekeep, now, d.housekeepEvery()) {
				lastHousekeep = now
				if err := d.mirror.Prune(workCtx); err != nil {
					slog.Warn("prune", "err", err)
				}
				if _, err := d.escalator.Sweep(workCtx); err != nil {
					slog.Warn("escalation sweep", "err", err)
				}
				if sums, err := d.compactor.Sweep(workCtx, d.tenant, StandingScopes...); err != nil {
					slog.Warn("compaction sweep", "err", err)
				} else if len(sums) > 0 {
					slog.Info("compacted", "scopes", len(sums))
				}
				if keep := d.runsKeepFor(); keep > 0 {
					if n, err := d.store.Runs().Prune(workCtx, d.tenant, time.Now().Add(-keep)); err != nil {
						slog.Warn("run log prune", "err", err)
					} else if n > 0 {
						slog.Info("run log pruned", "frames", n, "older_than", keep)
					}
				}
				// Last: a successful pass returns ErrSelfDeployed, ending
				// this Run so the deferred httpSrv.Shutdown above still
				// fires. Ordered last so every other standing chore gets its
				// turn on this tick before the process hands off.
				if err := d.selfDeployChore(workCtx); err != nil {
					slog.Info("self-deploy: re-exec required", "err", err)
					return err
				}
			}
		}
	}
}

// housekeepingDue reports whether interval has elapsed since last, as of
// now — a pure function so the wall-clock cadence in Run is testable
// without actually waiting out an interval. A tick-counted cadence
// (ticks%N==0) let one long-blocked tick fire N housekeeping passes back to
// back the moment it finally woke; timing off the clock instead means a
// single slow tick is merely late, visible in the self-deploy pass record's
// own timestamp, rather than amplified into a burst.
func housekeepingDue(last, now time.Time, interval time.Duration) bool {
	return now.Sub(last) >= interval
}

// pollDue is one route whose poll interval has elapsed, paired with the
// remote its Lister reads.
type pollDue struct {
	route  ingress.Route
	target string
}

// duePolls reports every route whose Poll interval has elapsed as of now,
// and stamps now as that route's last poll so the same window is not
// double-counted by two close ticks. Under mu because the route table is
// swapped on reload (assemble, New and Reload) and lastPoll is read and
// written from here on every tick.
func (d *Daemon) duePolls(now time.Time) []pollDue {
	d.mu.Lock()
	defer d.mu.Unlock()
	var due []pollDue
	for name, r := range d.routes {
		if r.Poll <= 0 {
			continue
		}
		if last, polled := d.lastPoll[name]; polled && now.Sub(last) < r.Poll {
			continue
		}
		d.lastPoll[name] = now
		due = append(due, pollDue{route: r, target: d.remotes[name]})
	}
	return due
}

// PollRoutes polls every route due as of now, logging (never panicking on) a
// failure the way the other standing chores in Run do — a route with
// polling off is never in the due list, so this is a no-op for every config
// that does not ask for it. Its own method, rather than reachable only
// through Run's select, so a test can call it directly with a fake Lister
// behind d.poller.
func (d *Daemon) PollRoutes(ctx context.Context, now time.Time) {
	for _, due := range d.duePolls(now) {
		if err := d.poller.Poll(ctx, due.route, due.target, now); err != nil {
			slog.Warn("route poll", "route", due.route.Name, "err", err)
		}
	}
}

// StandingScopes are the chains that grow without bound, and therefore the
// ones the compaction sweep visits.
//
// Named rather than discovered, because the distinction is real and a scope
// enumerator would blur it. A per-entity chain — workitem/<id>, ingress/<id>,
// changeset/<id> — is bounded by the thing it is about: it stops when the
// item settles, holds tens of records, and is exactly what an auditor
// sampling that change asks for by subject. Compacting those would trade the
// evidence most likely to be READ for space that the standing chains are
// actually consuming, where one busy hour writes more rows than a hundred
// work items ever will.
//
// What this leaves open is stated rather than implied: a factory that runs
// for years accumulates unboundedly MANY bounded per-entity chains, which is
// a different problem — a per-tenant archive of settled work — and it wants
// an enumerator and a policy of its own rather than this list growing a
// wildcard.
//
// control/self-deploy is deliberately NOT here, unlike the other standing
// chains. [Compactor.CompactScope] prunes a scope's cold prefix by count and
// age alone, scope-wide across every subject at once — it has no notion of
// subject and [evidence.Summarize] leaves Subject empty on the record that
// replaces what it folds. selfDeployRefused and selfDeployException both
// depend on reading back the LATEST record for one specific head via
// BySubject, indefinitely, for as long as that head remains the target
// branch's tip — a bad head that a human has not yet fixed forward from can
// sit refused for longer than any Keep/KeepFor an operator would set. A
// scope-wide compaction cannot tell that head's record apart from one it is
// safe to fold, so folding it away would silently turn a durable refusal
// back into an unmemoried one, retried every housekeeping pass forever —
// exactly the failure the durable refusal exists to prevent. This chain's
// volume is bounded by how often boxes actually self-deploy, which is
// orders of magnitude below control/claims or index/ingress, so leaving it
// out costs unbounded-but-slow growth in exchange for a correctness
// property compaction cannot currently preserve.
//
// That growth is no longer merely slow. Every housekeeping pass — not just
// a deploy or a refusal — now appends its own self-deploy-pass record here
// too (issue #152), so a box with self_deploy on writes one every
// HousekeepEvery regardless of whether anything moved: at the 5m default
// that is ~288 records/day/box, steadily, forever. Those records are safe
// to fold on their own — their Subject is "pass/<box>", never a head, so
// none of them is the LATEST-by-head answer selfDeployRefused or
// selfDeployException depend on — but a scope-wide compaction cannot tell
// them apart from the head-subject records already reasoned about above,
// so they are stuck uncompactable beside those for the same reason. Kept on
// this scope anyway, deliberately, rather than split onto a sibling
// StandingScopes chain: a pass record's evidentiary value (what did the box
// see, right beside the deploy or refusal it explains) is highest exactly
// where it already lives, and control/self-deploy for a fleet of boxes is
// still far below control/claims' volume. Revisit if that stops being true.
var StandingScopes = []string{
	"control/capacity",
	"control/claims",
	"control/config",
	"control/daemon",
	"control/escalations",
	"control/review",
	"index/ingress",
	"index/pending",
}

func (d *Daemon) drain() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.DrainDuration()
}

// housekeepEvery reads the current wall-clock gap between housekeeping
// passes — hot-reloadable, like drain and Every above.
func (d *Daemon) housekeepEvery() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.HousekeepEveryDuration()
}

// defaultBootIDPath is the kernel's own record of this boot, regenerated
// every time the host boots — the production value of [Daemon.bootIDPath].
const defaultBootIDPath = "/proc/sys/kernel/random/boot_id"

// daemonStartedScope is the standing chain a daemon-started record is
// appended to. One record per process start grows without bound, so it is
// in [StandingScopes] and swept like the rest.
const daemonStartedScope = "control/daemon"

// daemonStartedBody is the JSON body of a daemon-started record. BootIDKnown
// and BuildKnown carry the fact of unknown-ness explicitly, mirroring
// [version.Build] itself: an unreadable boot-id file or an unstamped build
// must never be presented as a zero value indistinguishable from a real one.
type daemonStartedBody struct {
	BootID      string `json:"boot_id,omitempty"`
	BootIDKnown bool   `json:"boot_id_known"`
	BuildCommit string `json:"build_commit,omitempty"`
	BuildDirty  bool   `json:"build_dirty,omitempty"`
	BuildKnown  bool   `json:"build_known"`
}

// readBootID reads the host's boot id from path, trimmed of the trailing
// newline the kernel file carries. Any read failure — missing file, denied
// permission, a path that is not this kernel's boot-id file at all — reports
// unknown rather than guessing from an empty read.
func readBootID(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", false
	}
	return id, true
}

// recordStarted appends the daemon-started record: this process came up,
// identified by the host's boot id and the binary's own build. Called from
// Run once the listener is bound — never from New, which every test
// constructs without running — so a gap between a stop-with-reason and the
// next daemon-started record is the record of the box being down, not an
// inference from log silence.
func (d *Daemon) recordStarted(ctx context.Context) error {
	body := daemonStartedBody{}
	bootID, ok := readBootID(d.bootIDPath)
	body.BootID, body.BootIDKnown = bootID, ok
	build := d.build()
	if build.Known {
		body.BuildCommit, body.BuildDirty, body.BuildKnown = build.Commit, build.Dirty, true
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("daemon: encoding daemon-started record: %w", err)
	}
	subject := bootID
	if !ok {
		subject = "unknown"
	}
	_, err = d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: daemonStartedScope, Kind: "daemon-started",
		Subject: subject, Actor: d.cfgRunner(), Body: encoded,
	})
	return err
}

// observeSuspend calls d.suspend.Observe with this daemon's own tick
// interval as threshold and, when it reports a gap, records it. Its own
// method rather than inlined in Run's select, the same shape as PollRoutes
// above, so a test can drive one tick's suspend observation directly against
// a fake-clocked d.suspend without spinning up Run's listener and select
// loop.
func (d *Daemon) observeSuspend(ctx context.Context) {
	gap := d.suspend.Observe(d.Every())
	if gap <= 0 {
		return
	}
	if err := d.recordResumed(ctx, gap); err != nil {
		slog.Error("daemon-resumed: recording", "err", err)
	}
}

// daemonResumedBody is the JSON body of a daemon-resumed record. GapNS
// carries the machine form a consumer can compute with; Gap carries
// [time.Duration.String]'s human form beside it, the same say-it-both-ways
// shape [SpendOf] and its kin use elsewhere in this package, so a person
// reading the chain does not have to do the division themselves.
type daemonResumedBody struct {
	GapNS int64  `json:"gap_ns"`
	Gap   string `json:"gap"`
}

// recordResumed appends the daemon-resumed record: [loop.SuspendWatch.Observe]
// found a wall-vs-monotonic gap this tick, meaning the host was suspended for
// roughly that long. Only called when a gap was detected — unlike
// daemon-started, there is no "resumed with a zero gap" case to distinguish
// from absence, so the record's mere existence is the fact.
//
// Subject is the host's own boot id, read the same way [Daemon.recordStarted]
// does, rather than the gap itself: a boot id lets every daemon-resumed
// record for one uptime be found beside the daemon-started record that began
// it, which the gap's own string value cannot do (two suspends of the same
// length are indistinguishable by gap alone).
func (d *Daemon) recordResumed(ctx context.Context, gap time.Duration) error {
	body := daemonResumedBody{GapNS: int64(gap), Gap: gap.String()}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("daemon: encoding daemon-resumed record: %w", err)
	}
	bootID, ok := readBootID(d.bootIDPath)
	subject := bootID
	if !ok {
		subject = "unknown"
	}
	_, err = d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: daemonStartedScope, Kind: "daemon-resumed",
		Subject: subject, Actor: d.cfgRunner(), Body: encoded,
	})
	return err
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
