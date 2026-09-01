package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const btn = tenant.ID("acme")

func layers() agent.StaticLayers {
	return agent.StaticLayers{{
		Name: "builder", Scope: agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: "d-role"},
		Instructions: "You build changes for the shop box.",
		Tools:        agent.ToolPolicy{Deny: []string{"DeleteProd"}},
	}}
}

func rig(t *testing.T, scenario string) (*sqlite.Store, *loop.Loop, *ingress.StoreRouter, *seat.Seat, string) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	remote := filepath.Join(t.TempDir(), "app.git")
	mustSh(t, "", "git", "init", "--bare", remote)
	mustSh(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	seedDir := filepath.Join(t.TempDir(), "seed")
	mustSh(t, "", "git", "clone", remote, seedDir)
	os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("app\n"), 0o644)
	mustSh(t, seedDir, "git", "add", ".")
	mustSh(t, seedDir, "git", "commit", "-m", "seed")
	mustSh(t, seedDir, "git", "push", "origin", "HEAD:refs/heads/main")

	repo := forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
	m, err := gitforge.NewMirror(t.TempDir(), btn, s.Leaser("runner-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Fetch(context.Background(), repo); err != nil {
		t.Fatal(err)
	}

	st := &seat.Seat{Name: "mason", Base: &knowledge.Base{Store: s, Tenant: btn}}
	b := &Builder{
		Store: s, Tenant: btn, Box: "shop",
		Command:  []string{os.Args[0]},
		Env:      []string{"ACP_FAKE=" + scenario},
		Composer: &agent.Composer{Source: layers()},
		Seat:     st, Mirror: m,
	}
	lp := &loop.Loop{
		Store: s, Runner: "runner-1", Mirror: m, Repo: repo, Branch: "main",
		Builder: b, Handoff: b.Handoff, Lane: forge.LaneGated,
		Gates: func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: "check", Dir: ws.Dir,
				Commands: [][]string{{"sh", "-c", "test -f widget.go || test -f hello.txt"}}}}
		},
		TTL: time.Hour,
	}
	return s, lp, &ingress.StoreRouter{Store: s, Tenant: btn, Runner: "runner-1"}, st, remote
}

func runItem(t *testing.T, s *sqlite.Store, lp *loop.Loop, router *ingress.StoreRouter, key, task string) queue.WorkItem {
	t.Helper()
	return runItemCtx(t, context.Background(), s, lp, router, key, task)
}

func runItemCtx(t *testing.T, ctx context.Context, s *sqlite.Store, lp *loop.Loop, router *ingress.StoreRouter, key, task string) queue.WorkItem {
	t.Helper()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, loop.ItemFor(ing, "wi-"+key, task))
	if err != nil {
		t.Fatal(err)
	}
	item, err = lp.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestAgentBuildsThroughTheWholeLoop: a (fake but protocol-real) agent in
// the Builder seat — resolve, run, commit, gate, land — with the agentref on
// the record and the handoff on the seat.
func TestAgentBuildsThroughTheWholeLoop(t *testing.T) {
	s, lp, router, st, remote := rig(t, "worker")
	ctx := context.Background()
	item := runItem(t, s, lp, router, "issue-1", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("agent-built item: %+v", item)
	}
	// The change is really on the remote.
	if out := mustSh(t, remote, "git", "log", "--format=%s", "refs/heads/main"); !strings.Contains(out, "add widget") {
		t.Fatalf("agent's commit not on main:\n%s", out)
	}
	// The agentref was recorded BEFORE the run, on the item's chain.
	sawRef := false
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-1", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-run" && len(r.Subject) == 64 && r.Actor == "mason" {
			sawRef = true
		}
	}
	if !sawRef {
		t.Fatal("no agentref on the record")
	}
	// The handoff reached the seat, and the NEXT run wakes with it.
	wake, ok, err := st.WakeLayer(ctx, 5)
	if err != nil || !ok {
		t.Fatalf("wake: %v %v", ok, err)
	}
	if !strings.Contains(wake.Instructions, "expects blue") {
		t.Fatalf("handoff lost:\n%s", wake.Instructions)
	}
}

// TestLazyAgentCannotLookDone: declaring victory over an untouched tree is
// an error, never a landing.
func TestLazyAgentCannotLookDone(t *testing.T) {
	s, lp, router, _, remote := rig(t, "lazy")
	item := runItem(t, s, lp, router, "issue-2", "add the widget package")
	if item.State != queue.StateParked || !strings.Contains(item.Note, "without changing anything") {
		t.Fatalf("lazy agent: %+v", item)
	}
	if out := mustSh(t, remote, "git", "rev-list", "--count", "refs/heads/main"); strings.TrimSpace(out) != "1" {
		t.Fatal("a lazy run moved the remote")
	}
	_ = s
}

// TestAgentEscalationParksAttributed: the verb travels the whole path —
// agent to runtime to adapter to loop to evidence.
func TestAgentEscalationParksAttributed(t *testing.T) {
	s, lp, router, _, _ := rig(t, "escalate")
	ctx := context.Background()
	item := runItem(t, s, lp, router, "issue-3", "do the impossible")
	if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("escalation: %+v", item)
	}
	saw := false
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-3", 1) {
		if err == nil && r.Kind == "escalated" && r.Actor == "mason" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("escalation evidence missing")
	}
}

func mustSh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return string(out)
}

// TestTheRecordSaysWhichToolsRan: a count cannot tell an ordinary run from a
// runaway — both are a number — and "the agent made 340 tool calls" is a fact
// nobody can act on while "it ran Bash 340 times" is. The names were on the
// Result all along and were dropped at the record site.
func TestTheRecordSaysWhichToolsRan(t *testing.T) {
	s, lp, router, _, _ := rig(t, "toolsy")
	ctx := context.Background()
	item := runItem(t, s, lp, router, "issue-tools", "use a tool")
	if item.State != queue.StateDone {
		t.Fatalf("item: %+v", item)
	}
	body := ""
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-tools", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-result" {
			body = string(r.Body)
		}
	}
	if body == "" {
		t.Fatal("no agent-result on the chain")
	}
	for _, want := range []string{`"tools":{"Bash":1}`, `"denied_tools":{"DeleteProd":1}`, `"metered":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("agent-result missing %s:\n%s", want, body)
		}
	}
}
