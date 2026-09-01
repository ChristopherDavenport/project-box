// Command boxd is the factory's heartbeat: intake, drain, and the standing
// tasks, on a tick, until stopped. Step up ingress and step back — the
// system builds itself, and the human surface is cmd/box (exceptions, the
// stop button, review sampling), not this daemon.
//
// One Box, one service repo, flag-configured: the deliberately small
// bootstrap shape. Multi-Box wiring arrives with box.yaml when a real
// deployment needs it — a config format invented before then would be
// guessed, not derived.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/api"
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
	"github.com/ChristopherDavenport/project-box/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("boxd " + version.String())
		os.Exit(0)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "boxd:", err)
		os.Exit(1)
	}
}

func run() error {
	db := flag.String("db", "box.db", "store path")
	tn := flag.String("tenant", "", "tenant id")
	box := flag.String("box", "", "box name")
	runner := flag.String("runner", "boxd", "runner identity")
	remote := flag.String("repo", "", "service repo remote")
	branch := flag.String("branch", "main", "integration branch")
	lane := flag.String("lane", "optimistic", "landing lane: gated|optimistic")
	root := flag.String("root", "forge", "mirror/workspace root")
	every := flag.Duration("every", 15*time.Second, "tick interval")
	maxConcurrent := flag.Int("max-concurrent", 2, "concurrent runs")
	maxPerHour := flag.Int("max-per-hour", 0, "run starts per hour, 0 = unlimited")
	buildCmd := flag.String("build", "", "builder command run in the workspace (commits its own change)")
	agentBin := flag.String("agent", "", "ACP agent binary (e.g. claude-agent-acp); overrides -build with a real agent")
	seatName := flag.String("seat", "mason", "seat identity for the agent's continuity")
	checkCmd := flag.String("check", "go test ./...", "gate/check command")
	otlp := flag.String("otlp", "", "collector endpoint (host:port); empty = telemetry off")
	listen := flag.String("listen", "unix:boxd.sock", "the daemon's unix socket (network exposure is boxgate's job)")
	cfgPath := flag.String("config", "", "box.yaml — multi-box, multi-repo configuration; overrides the single-repo flags")
	flag.Parse()

	if *cfgPath != "" {
		return runConfigured(*cfgPath)
	}

	t := tenant.ID(*tn)
	if err := t.Validate(); err != nil {
		return err
	}
	if *box == "" || *remote == "" {
		return fmt.Errorf("boxd needs -box and -repo")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Setup(ctx, *otlp, "boxd")
	if err != nil {
		return err
	}
	defer shutdown(context.Background())

	s, err := sqlite.Open(*db)
	if err != nil {
		return err
	}
	defer s.Close()

	repo := forge.Repo{Name: "app", Remote: *remote, Role: forge.RoleService, DefaultBranch: *branch}
	m, err := gitforge.NewMirror(*root, t, s.Leaser(*runner))
	if err != nil {
		return err
	}
	if err := m.Fetch(ctx, repo); err != nil {
		return err
	}

	var builder loop.Builder = shellBuilder{cmd: *buildCmd}
	var handoff func(context.Context, queue.WorkItem) error
	if *agentBin != "" {
		ab := &acp.Builder{
			Store: s, Tenant: t, Box: *box,
			Command: []string{*agentBin},
			Composer: &agent.Composer{Source: agent.StaticLayers{{
				Name: "builder", Scope: agent.ScopeRole,
				Source:       agent.Source{Kind: agent.SourceInline, Digest: "boxd-default-role"},
				Instructions: "You are a build agent in an automated factory. Make the requested change precisely and minimally.",
				Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
			}}},
			Seat:   &seat.Seat{Name: *seatName, Base: &knowledge.Base{Store: s, Tenant: t}},
			Mirror: m,
		}
		builder, handoff = ab, ab.Handoff
	}

	q := &queue.Queue{Store: s, Runner: *runner, TTL: 30 * time.Minute}
	p := &pump.Pump{
		Store: s, Tenant: t, Runner: *runner, Queue: q,
		Meter: &capacity.Meter{Store: s, Tenant: t, Actor: *runner,
			Budget: capacity.Budget{MaxConcurrent: *maxConcurrent, MaxPerWindow: *maxPerHour, Window: time.Hour}},
		Loops: map[string]*loop.Loop{*box: {
			Store: s, Runner: *runner, Mirror: m, Repo: repo, Branch: *branch,
			Lane:    forge.Lane(*lane),
			Builder: builder, Handoff: handoff,
			Gates: func(ws forge.Workspace) []forge.Gate {
				return []forge.Gate{gate.Command{Label: "check", Dir: ws.Dir,
					Commands: [][]string{{"sh", "-c", *checkCmd}}}}
			},
			TTL: 30 * time.Minute,
		}},
	}

	routes := map[string]ingress.Route{
		"issues": {Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue,
			Box: *box, Requester: *runner},
	}
	apiSrv := &api.Server{
		Store: s, Tenant: t, Routes: routes,
		Router: &ingress.StoreRouter{Store: s, Tenant: t, Runner: *runner},
		Queue:  q, Box: *box, Runner: *runner, Started: time.Now(),
	}
	ln, err := api.Listen(*listen)
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
	slog.Info("boxd up", "tenant", t, "box", *box, "lane", *lane, "every", *every, "api", *listen)
	d := &pump.Daemon{Every: *every, Pump: p,
		Errs: func(task string, err error) { slog.Error("task failed", "task", task, "err", err) },
	}
	err = d.Run(ctx)
	slog.Info("boxd down", "reason", err)
	return nil
}

// shellBuilder runs a configured command in the workspace; the command makes
// and commits the change (a script today, an ACP agent adapter tomorrow —
// the loop cannot tell, which is the point of the seam).
type shellBuilder struct{ cmd string }

func (b shellBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if b.cmd == "" {
		return "", fmt.Errorf("boxd: no -build command configured")
	}
	c := gate.Command{Label: "build", Dir: ws.Dir, Commands: [][]string{{"sh", "-c", b.cmd}}}
	v, err := c.Evaluate(ctx, forge.ChangeSet{})
	if err != nil {
		return "", err
	}
	if v.Decision != forge.DecisionPass {
		return "", fmt.Errorf("builder command failed: %s", firstLine(v.Reason))
	}
	out, err := gitforge.RevParse(ctx, ws.Dir, "HEAD")
	if err != nil {
		return "", err
	}
	return forge.Commit(out), nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
