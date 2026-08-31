package ladder

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/approval"
	"github.com/ChristopherDavenport/project-box/internal/authz"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/render"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

// builder lands whatever the task says: "fix" writes the flag the post-land
// check wants; "break" does not.
type builder struct{}

func (builder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if strings.Contains(task, "fix") {
		if err := os.WriteFile(filepath.Join(ws.Dir, "ok.txt"), []byte("green\n"), 0o644); err != nil {
			return "", err
		}
	}
	os.WriteFile(filepath.Join(ws.Dir, "change.txt"), []byte(task+"\n"), 0o644)
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", task); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

type world struct {
	store           *sqlite.Store
	advance         func(time.Duration)
	now             func() time.Time
	app, stag, prod string // remotes
	mirror          *gitforge.Mirror
	router          *ingress.StoreRouter
	svcLoop         *loop.Loop
	approvals       []approval.Approval
	candidate       string
	mu              sync.Mutex
	lad             *Ladder
}

func bare(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name+".git")
	mustSh(t, "", "git", "init", "--bare", dir)
	mustSh(t, dir, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), name+"-seed")
	mustSh(t, "", "git", "clone", dir, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte(name+"\n"), 0o644)
	mustSh(t, seed, "git", "add", ".")
	mustSh(t, seed, "git", "commit", "-m", "seed")
	mustSh(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")
	return dir
}

func setup(t *testing.T, soak time.Duration, prodApprovals int) *world {
	t.Helper()
	var mu sync.Mutex
	offset := time.Duration(0)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return time.Now().Add(offset) }
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	w := &world{store: s, now: now,
		advance: func(d time.Duration) { mu.Lock(); defer mu.Unlock(); offset += d },
		app:     bare(t, "app"), stag: bare(t, "env-staging"), prod: bare(t, "env-prod"),
	}
	appRepo := forge.Repo{Name: "app", Remote: w.app, Role: forge.RoleService, DefaultBranch: "main"}
	stagRepo := forge.Repo{Name: "env-staging", Remote: w.stag, Role: forge.RoleEnv, DefaultBranch: "main"}
	prodRepo := forge.Repo{Name: "env-prod", Remote: w.prod, Role: forge.RoleEnv, DefaultBranch: "main"}

	m, err := gitforge.NewMirror(t.TempDir(), tn, s.Leaser("runner-1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, r := range []forge.Repo{appRepo, stagRepo, prodRepo} {
		if err := m.Fetch(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	w.mirror = m
	w.router = &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "runner-1"}

	w.svcLoop = &loop.Loop{
		Store: s, Runner: "runner-1", Mirror: m, Repo: appRepo, Branch: "main",
		Builder: builder{}, Lane: forge.LaneOptimistic,
		Gates: func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: "post-land", Dir: ws.Dir, Commands: [][]string{{"test", "-f", "ok.txt"}}}}
		},
		Red: loop.OpenRepair(w.router, ingress.Route{
			Name: "red-main", Kind: ingress.RouteRedMain, Opens: ingress.KindIssue,
			Box: "shop", Requester: "gargoyle",
		}),
		TTL: time.Hour,
	}

	machineGate := func(name string) loop.Gates {
		return func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: name, Dir: ws.Dir, Commands: [][]string{{"test", "-s", "services/shop-api.yaml"}}}}
		}
	}
	prodGates := func(ws forge.Workspace) []forge.Gate {
		gs := machineGate("prod-manifest")(ws)
		if prodApprovals > 0 {
			gs = append(gs, approval.GateAdapter{
				Label:       "prod-approval",
				Requirement: approval.Requirement{Count: prodApprovals, Sources: []approval.Source{approval.SourceDirect}},
				Requester:   authz.Actor{Principal: authz.Principal{Subject: "runner-1", Kind: authz.KindAgent, Tenant: tn}},
				Head: func(cs forge.ChangeSet) string {
					w.mu.Lock()
					defer w.mu.Unlock()
					return w.candidate // approval binds to the CANDIDATE, not the env commit
				},
				List: func(_ context.Context, head string) ([]approval.Approval, error) {
					w.mu.Lock()
					defer w.mu.Unlock()
					return append([]approval.Approval(nil), w.approvals...), nil
				},
			})
		}
		return gs
	}

	w.lad = &Ladder{
		Store: s, Tenant: tn, Runner: "runner-1", Router: w.router, Mirror: m,
		Source:  appRepo,
		Service: render.Service{Name: "shop-api", Box: "shop", Image: "gcr.io/acme/shop-api", Port: 80, Replicas: 1},
		Staging: Env{Name: "staging", Namespace: "shop", Repo: stagRepo, Gates: machineGate("staging-manifest")},
		Prod:    Env{Name: "prod", Namespace: "shop", Repo: prodRepo, Gates: prodGates},
		Soak:    soak, Now: now, TTL: time.Hour,
	}
	return w
}

// candidate is stashed on world so the approval gate can bind to it.
func (w *world) setCandidate(sha string) { w.mu.Lock(); w.candidate = sha; w.mu.Unlock() }

func (w *world) land(t *testing.T, id, task string) queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	ing, _, err := w.router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: id})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: w.store, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, loop.ItemFor(ing, id, task))
	if err != nil {
		t.Fatal(err)
	}
	item, err = w.svcLoop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func remoteLog(t *testing.T, remote string) string {
	t.Helper()
	out, err := sh(remote, "git", "log", "--format=%H %s", "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTheLadder is Phase 9 end to end: red lands and stays, repair opens,
// promotion refuses red, staging takes green, prod refuses unsoaked and
// unapproved, and prod provably never sees the red commit.
func TestTheLadder(t *testing.T) {
	w := setup(t, 2*time.Hour, 1)
	ctx := context.Background()

	// 1. A breaking commit LANDS on the optimistic main — and stays.
	red := w.land(t, "wi-red", "break the build")
	if red.State != queue.StateDone {
		t.Fatalf("optimistic red landing: %+v", red)
	}
	if !strings.Contains(red.Note, "repair opened") {
		t.Fatalf("red landing opened no repair: %q", red.Note)
	}
	if !strings.Contains(remoteLog(t, w.app), red.Head) {
		t.Fatal("the red commit is not on main — something rolled back, and nothing should")
	}
	// The repair is idempotent: the same red sha names one ingress.
	_, opened, err := w.router.Open(ctx, ingress.Route{
		Name: "red-main", Kind: ingress.RouteRedMain, Opens: ingress.KindIssue, Box: "shop", Requester: "gargoyle",
	}, ingress.Delivery{Key: "red-" + red.Head})
	if err != nil || opened {
		t.Fatalf("repair not idempotent: opened=%v err=%v", opened, err)
	}

	// 2. No green candidate yet: staging refuses by not advancing.
	if _, err := w.lad.PromoteToStaging(ctx); !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("staging with only a red landing: %v", err)
	}

	// 3. A green commit lands; staging takes IT, skipping the red one.
	green := w.land(t, "wi-green", "fix the build")
	if green.State != queue.StateDone || green.Note != "" {
		t.Fatalf("green landing: %+v", green)
	}
	w.setCandidate(green.Head)
	cand, err := w.lad.PromoteToStaging(ctx)
	if err != nil || cand != green.Head {
		t.Fatalf("staging promotion: %s, %v", cand, err)
	}
	stagLog := remoteLog(t, w.stag)
	if !strings.Contains(stagLog, green.Head[:12]) {
		t.Fatalf("staging does not carry the green candidate:\n%s", stagLog)
	}
	if strings.Contains(stagLog, red.Head[:12]) {
		t.Fatal("the red commit reached staging")
	}
	if _, err := w.lad.PromoteToStaging(ctx); !errors.Is(err, ErrUpToDate) {
		t.Fatalf("re-promotion should be up to date: %v", err)
	}

	// 4. Prod refuses the unsoaked candidate.
	if _, err := w.lad.PromoteToProd(ctx); !errors.Is(err, ErrNotSoaked) {
		t.Fatalf("prod before soak: %v", err)
	}
	if strings.Contains(remoteLog(t, w.prod), "shop-api") {
		t.Fatal("prod received something while refusing")
	}

	// 5. Soaked — but the production boundary wants a human, and approvals
	// that are stale (wrong commit) or self (the requester's own) do not
	// count.
	w.advance(3 * time.Hour)
	w.mu.Lock()
	w.approvals = []approval.Approval{
		{Subject: "reviewer", Commit: forge.Commit(red.Head), Source: approval.SourceDirect},   // stale: approves the wrong commit
		{Subject: "runner-1", Commit: forge.Commit(green.Head), Source: approval.SourceDirect}, // self: the requester
	}
	w.mu.Unlock()
	if _, err := w.lad.PromoteToProd(ctx); !errors.Is(err, ErrAwaitingApproval) {
		t.Fatalf("prod without a countable approval: %v", err)
	}

	// 6. A real approval of the candidate: prod takes it.
	w.mu.Lock()
	w.approvals = append(w.approvals, approval.Approval{
		Subject: "reviewer", Commit: forge.Commit(green.Head), Source: approval.SourceDirect,
	})
	w.mu.Unlock()
	cand, err = w.lad.PromoteToProd(ctx)
	if err != nil || cand != green.Head {
		t.Fatalf("prod promotion: %s, %v", cand, err)
	}
	prodLog := remoteLog(t, w.prod)
	if !strings.Contains(prodLog, green.Head[:12]) {
		t.Fatalf("prod does not carry the candidate:\n%s", prodLog)
	}
	// The exit criterion, literally: prod provably never received the red.
	if strings.Contains(prodLog, red.Head[:12]) {
		t.Fatal("prod received a commit that never soaked green")
	}
}

// TestFencingIsIdenticalInTheOptimisticLane: a superseded token is refused
// at an optimistic land exactly as at a gated one. The supersession happens
// mid-run, between build and land, which is exactly the window a stalled or
// preempted runner occupies.
func TestFencingIsIdenticalInTheOptimisticLane(t *testing.T) {
	w := setup(t, time.Hour, 0)
	ctx := context.Background()
	before := remoteLog(t, w.app)

	preempting := &loop.Loop{
		Store: w.store, Runner: "runner-1", Mirror: w.mirror,
		Repo: forge.Repo{Name: "app", Remote: w.app, Role: forge.RoleService, DefaultBranch: "main"}, Branch: "main",
		Lane:    forge.LaneOptimistic,
		Builder: preemptBuilder{store: w.store},
		Gates: func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: "post-land", Dir: ws.Dir, Commands: [][]string{{"true"}}}}
		},
		TTL: time.Hour,
	}
	ing, _, err := w.router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "wi-fence"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: w.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, loop.ItemFor(ing, "wi-fence", "racing change"))
	item, err = preempting.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "fencing refused") {
		t.Fatalf("optimistic land with a superseded token must park on the fence: %+v", item)
	}
	if remoteLog(t, w.app) != before {
		t.Fatal("a fenced optimistic land still moved the remote")
	}
}

// preemptBuilder makes a change, then loses its lease mid-run: an incident
// preempts the branch between build and land.
type preemptBuilder struct{ store *sqlite.Store }

func (b preemptBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	head, err := builder{}.Build(ctx, ws, task)
	if err != nil {
		return "", err
	}
	_, err = b.store.Leaser("incident-runner").Preempt(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "incident", TTL: time.Hour,
	})
	return head, err
}

// TestEnvReposRefuseTheOptimisticLane: their landings ARE deployments.
func TestEnvReposRefuseTheOptimisticLane(t *testing.T) {
	w := setup(t, time.Hour, 0)
	bad := &loop.Loop{
		Store: w.store, Runner: "runner-1", Mirror: w.mirror,
		Repo:   forge.Repo{Name: "env-staging", Remote: w.stag, Role: forge.RoleEnv, DefaultBranch: "main"},
		Branch: "main", Lane: forge.LaneOptimistic, Builder: builder{},
		Gates: func(forge.Workspace) []forge.Gate { return nil }, TTL: time.Hour,
	}
	_, err := bad.Run(context.Background(), queue.WorkItem{ID: "wi-x", Tenant: tn, Box: "shop", Ingress: "ing-x"})
	if err == nil || !strings.Contains(err.Error(), "never run the optimistic lane") {
		t.Fatalf("env repo accepted the optimistic lane: %v", err)
	}
}

// TestOptimisticLaneRefusesHumanGates: below the production boundary there
// are no humans in any loop.
func TestOptimisticLaneRefusesHumanGates(t *testing.T) {
	w := setup(t, time.Hour, 0)
	w.svcLoop.Gates = func(ws forge.Workspace) []forge.Gate {
		return []forge.Gate{approval.GateAdapter{
			Requirement: approval.Requirement{Count: 1},
			List:        func(context.Context, string) ([]approval.Approval, error) { return nil, nil },
		}}
	}
	ctx := context.Background()
	ing, _, _ := w.router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "wi-human"})
	q := &queue.Queue{Store: w.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, loop.ItemFor(ing, "wi-human", "fix something"))
	_, err := w.svcLoop.Run(ctx, item)
	if err == nil || !strings.Contains(err.Error(), "binds at the production boundary") {
		t.Fatalf("a human gate ran in the optimistic lane: %v", err)
	}
}

func sh(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustSh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := sh(dir, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return strings.TrimSpace(out)
}
