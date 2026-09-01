package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/pump"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// runConfigured is boxd from box.yaml: every box, every repo, one daemon.
// Repositories share the tenant, the store, the mirror set, and the budget;
// they share NO leases and no workspaces — that is the whole point.
func runConfigured(path string) error {
	cfg, err := boxcfg.Load(path)
	if err != nil {
		return err
	}
	t := tenant.ID(cfg.Tenant)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := telemetry.Setup(ctx, cfg.OTLP, "boxd")
	if err != nil {
		return err
	}
	defer shutdown(context.Background())

	s, err := sqlite.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer s.Close()
	m, err := gitforge.NewMirror(cfg.Root, t, s.Leaser(cfg.Runner))
	if err != nil {
		return err
	}
	q := &queue.Queue{Store: s, Runner: cfg.Runner, TTL: 30 * time.Minute}

	loops := map[string]*loop.Loop{}
	routes := map[string]ingress.Route{}
	for _, bx := range cfg.Boxes {
		var agentBuilder *acp.Builder
		if bx.Agent != "" {
			agentBuilder = &acp.Builder{
				Store: s, Tenant: t, Box: bx.Name,
				Command: []string{bx.Agent},
				Composer: &agent.Composer{Source: agent.StaticLayers{{
					Name: "builder", Scope: agent.ScopeRole,
					Source:       agent.Source{Kind: agent.SourceInline, Digest: "boxd-default-role"},
					Instructions: "You are a build agent in an automated factory. Make the requested change precisely and minimally.",
					Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
				}}},
				Seat:   &seat.Seat{Name: bx.Seat, Base: &knowledge.Base{Store: s, Tenant: t}},
				Mirror: m,
			}
		}
		for _, r := range bx.Repos {
			repo := forge.Repo{Name: r.Name, Remote: r.Remote, Role: forge.Role(r.Role), DefaultBranch: r.Branch}
			if err := m.Fetch(ctx, repo); err != nil {
				return err
			}
			check := r.Check
			lp := &loop.Loop{
				Store: s, Runner: cfg.Runner, Mirror: m, Repo: repo, Branch: r.Branch,
				Lane: forge.Lane(r.Lane),
				Gates: func(ws forge.Workspace) []forge.Gate {
					return []forge.Gate{gate.Command{Label: "check", Dir: ws.Dir,
						Commands: [][]string{{"sh", "-c", check}}}}
				},
				TTL: 30 * time.Minute,
			}
			if agentBuilder != nil {
				lp.Builder, lp.Handoff = agentBuilder, agentBuilder.Handoff
			} else {
				lp.Builder = shellBuilder{cmd: r.Build}
			}
			loops[bx.Name+"/"+r.Name] = lp
			routes[r.Route] = ingress.Route{
				Name: r.Route, Kind: ingress.RouteIssue, Opens: ingress.KindIssue,
				Box: bx.Name, Repo: r.Name, Requester: cfg.Runner,
			}
		}
	}

	p := &pump.Pump{
		Store: s, Tenant: t, Runner: cfg.Runner, Queue: q, Loops: loops,
		Meter: &capacity.Meter{Store: s, Tenant: t, Actor: cfg.Runner,
			Budget: capacity.Budget{MaxConcurrent: cfg.Budget.MaxConcurrent,
				MaxPerWindow: cfg.Budget.MaxPerWindow, Window: cfg.WindowDuration()}},
	}
	apiSrv := &api.Server{
		Store: s, Tenant: t, Routes: routes,
		Router: &ingress.StoreRouter{Store: s, Tenant: t, Runner: cfg.Runner},
		Queue:  q, Token: cfg.Token, Box: cfg.Boxes[0].Name, Runner: cfg.Runner, Started: time.Now(),
	}
	ln, err := api.Listen(cfg.Listen, cfg.Token)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: apiSrv.Handler()}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		httpSrv.Shutdown(shctx)
	}()

	slog.Info("boxd up (configured)", "tenant", t, "boxes", len(cfg.Boxes), "executors", len(loops), "routes", len(routes), "api", cfg.Listen)
	d := &pump.Daemon{Every: cfg.EveryDuration(), Pump: p,
		Errs: func(task string, err error) { slog.Error("task failed", "task", task, "err", err) },
	}
	err = d.Run(ctx)
	slog.Info("boxd down", "reason", err)
	return nil
}
