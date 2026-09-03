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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/harness"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/pump"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/web"
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
	return capacity.Spend{
		Input: u.InputTokens, Output: u.OutputTokens,
		CacheRead: u.CachedReadTokens, CacheWrite: u.CachedWriteTokens,
		Total: u.Tokens, CostMicros: capacity.Micros(u.CostUSD),
		Reported: u.Reported,
	}
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
	live *control.Live
	API  *api.Server

	mu    sync.RWMutex
	every time.Duration
	cfg   boxcfg.Config
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
	d := &Daemon{
		ConfigPath: path, store: s, tenant: t, mirror: m,
		queue: &queue.Queue{Store: s, Runner: cfg.Runner, TTL: 30 * time.Minute},
		meter: &capacity.Meter{Store: s, Tenant: t, Actor: cfg.Runner},
		live:  &control.Live{},
		cfg:   cfg, every: cfg.EveryDuration(),
	}
	loops, routes, err := d.assemble(context.Background(), cfg)
	if err != nil {
		s.Close()
		return nil, err
	}
	d.meter.SetBudget(capacity.Budget{MaxConcurrent: cfg.Budget.MaxConcurrent,
		MaxPerWindow:       cfg.Budget.MaxPerWindow,
		MaxTokensPerWindow: cfg.Budget.MaxTokensPerWindow,
		MaxCostPerWindow:   cfg.MaxCostMicros(),
		AllowUnmetered:     cfg.Budget.AllowUnmetered, Window: cfg.WindowDuration()})
	// The window's account outlives the process that spent it. Without this a
	// restart is a fresh budget, so the brake is only ever as strong as the
	// daemon's uptime.
	if err := d.meter.Restore(context.Background()); err != nil {
		s.Close()
		return nil, fmt.Errorf("daemon: restoring the capacity window: %w", err)
	}
	d.pump = &pump.Pump{Store: s, Tenant: t, Runner: cfg.Runner, Queue: d.queue, Meter: d.meter, Loops: loops}
	// The exception queue ages whether or not anybody is looking, so the
	// sweep is what makes "nobody is looking" audible. The log is the weakest
	// sink there is and the only one that needs no configuration; a real
	// deployment replaces it at this seam without the daemon changing.
	d.escalator = &control.Escalator{
		Room:   &control.Room{Store: s, Tenant: t, Actor: cfg.Runner},
		Notify: control.NotifierFunc(control.LogNotice),
	}
	d.API = &api.Server{
		Store: s, Tenant: t, Routes: routes,
		Router: &ingress.StoreRouter{Store: s, Tenant: t, Runner: cfg.Runner},
		Queue:  d.queue, Meter: d.meter, Box: cfg.Boxes[0].Name, Runner: cfg.Runner,
		Live:    d.live,
		Started: time.Now(),
		Reload:  d.reloadForAPI,
	}
	if ui, err := web.Dist(); err == nil {
		d.API.UI = ui
	}
	return d, nil
}

// Close releases the daemon's resources.
func (d *Daemon) Close() error { return d.store.Close() }

// assemble builds the executor and route tables from a config — used at New
// and again at every Reload.
func (d *Daemon) assemble(ctx context.Context, cfg boxcfg.Config) (map[string]*loop.Loop, map[string]ingress.Route, error) {
	loops := map[string]*loop.Loop{}
	routes := map[string]ingress.Route{}
	for _, bx := range cfg.Boxes {
		var agentBuilder *acp.Builder
		if len(bx.Agent) > 0 {
			agentBuilder = &acp.Builder{
				Store: d.store, Tenant: d.tenant, Box: bx.Name,
				Command:   bx.Agent,
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
				Progress: func(_ context.Context, run string, u acp.Update) {
					d.live.Append(run, control.Frame{
						Kind: string(u.Kind), Tool: u.Tool, Text: u.Text, Reason: u.Reason,
					})
				},
			}
		}
		for _, r := range bx.Repos {
			repo := forge.Repo{Name: r.Name, Remote: r.Remote, Role: forge.Role(r.Role), DefaultBranch: r.Branch}
			if err := d.mirror.Fetch(ctx, repo); err != nil {
				return nil, nil, fmt.Errorf("daemon: fetch %s/%s: %w", bx.Name, r.Name, err)
			}
			check := r.Check
			lp := &loop.Loop{
				Store: d.store, Runner: d.cfgRunner(), Mirror: d.mirror, Repo: repo, Branch: r.Branch,
				Lane: forge.Lane(r.Lane),
				Gates: func(ws forge.Workspace) []forge.Gate {
					return []forge.Gate{gate.Command{Label: "check", Dir: ws.Dir,
						Commands: [][]string{{"sh", "-c", check}}}}
				},
				TTL:          30 * time.Minute,
				BuildTimeout: cfg.BuildTimeoutDuration(),
			}
			if agentBuilder != nil {
				lp.Builder, lp.Handoff = agentBuilder, agentBuilder.Handoff
			} else {
				lp.Builder = ShellBuilder{Cmd: r.Build}
			}
			loops[bx.Name+"/"+r.Name] = lp
			routes[r.Route] = ingress.Route{
				Name: r.Route, Kind: ingress.RouteIssue, Opens: ingress.KindIssue,
				Box: bx.Name, Repo: r.Name, Requester: cfg.Runner,
			}
		}
	}
	return loops, routes, nil
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

func mcpRefs(servers []boxcfg.MCPServer) []agent.MCPRef {
	out := make([]agent.MCPRef, 0, len(servers))
	for _, m := range servers {
		out = append(out, agent.MCPRef{Name: m.Name, Command: m.Command, URL: m.URL, Tools: m.Tools})
	}
	return out
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
	loops, routes, err := d.assemble(ctx, ncfg)
	if err != nil {
		return "", fmt.Errorf("daemon: reload refused, old config still running: %w", err)
	}

	d.pump.SetLoops(loops)
	d.API.SetRoutes(routes)
	d.meter.SetBudget(capacity.Budget{MaxConcurrent: ncfg.Budget.MaxConcurrent,
		MaxPerWindow:       ncfg.Budget.MaxPerWindow,
		MaxTokensPerWindow: ncfg.Budget.MaxTokensPerWindow,
		MaxCostPerWindow:   ncfg.MaxCostMicros(),
		AllowUnmetered:     ncfg.Budget.AllowUnmetered, Window: ncfg.WindowDuration()})
	d.mu.Lock()
	d.cfg = ncfg
	d.every = ncfg.EveryDuration()
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
	httpSrv := &http.Server{Handler: d.API.Handler()}
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

	slog.Info("boxd up", "tenant", d.tenant, "api", listen, "every", d.Every(), "drain", d.drain())
	ticks := 0
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
			if _, err := d.Tick(workCtx); err != nil {
				slog.Error("tick", "err", err)
			}
			ticks++
			if ticks%housekeepingEvery == 0 {
				if err := d.mirror.Prune(workCtx); err != nil {
					slog.Warn("prune", "err", err)
				}
				if _, err := d.escalator.Sweep(workCtx); err != nil {
					slog.Warn("escalation sweep", "err", err)
				}
			}
		}
	}
}

// housekeepingEvery spaces the standing chores: every Nth tick, the mirror
// reaps workspaces whose leases no longer answer — the `docker system
// prune` job as a standing task rather than a remembered chore — and the
// room tells somebody about exceptions that have gone stale.
const housekeepingEvery = 20

func (d *Daemon) drain() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.DrainDuration()
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
