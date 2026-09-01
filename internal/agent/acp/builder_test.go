package acp

import (
	"context"
	"encoding/json"
	"io/fs"
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
	mirrorRoot := t.TempDir()
	m, err := gitforge.NewMirror(mirrorRoot, btn, s.Leaser("runner-1"))
	if err != nil {
		t.Fatal(err)
	}
	// A materialized context set is 0555 all the way down, and a 0555 directory
	// denies the unlink — so t.TempDir's own cleanup fails on it and reports a
	// permission error that has nothing to do with the test. Production reclaims
	// these through RemoveContext, which restores write on the way down for
	// exactly this reason; cleanups run last-registered-first, so this one runs
	// before the removal it is clearing the way for.
	t.Cleanup(func() {
		filepath.WalkDir(mirrorRoot, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o755)
			}
			return nil
		})
	})
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

// TestRepairWakesWithTheAttemptBeforeIt is issue #11's property: a repair
// run's bundle carries its predecessor's handoff, and its agentref differs
// from the same repair with no predecessor.
//
// The second half is what makes the first auditable. Repair opens through a
// route as new work, so without this the repair agent wakes cold and the
// record cannot say otherwise; carried as a layer, "this run was woken
// knowing X" is answerable from the digest afterwards, exactly the way seat
// continuity is.
func TestRepairWakesWithTheAttemptBeforeIt(t *testing.T) {
	s, lp, router, st, _ := rig(t, "worker")
	ctx := context.Background()

	// The run that went red. It lands, and it hands off what it learned.
	first := runItem(t, s, lp, router, "issue-prior", "add the widget package")
	if first.State != queue.StateDone {
		t.Fatalf("the predecessor did not land: %+v", first)
	}

	// What the repair would wake with if all it had were the seat: the same
	// continuity, and no account of the attempt it follows. This is the
	// control, captured before the repair adds its own notes to the seat.
	wake, ok, err := st.WakeLayer(ctx, 8)
	if err != nil || !ok {
		t.Fatalf("wake: ok=%v err=%v", ok, err)
	}
	task := "fix the red on main"
	cold, err := (&agent.Composer{Source: layers()}).Resolve(ctx, agent.Request{
		Tenant: btn, Role: agent.RoleBuilder, Box: "shop", Repo: "app",
		Task: task, Extra: []agent.Layer{wake},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The repair the factory opens for that red, carrying the attempt.
	lp.Builder.(*Builder).Env = []string{"ACP_FAKE=repair"}
	prior := &agent.Prior{
		Run: first.ID, Repo: "app", Branch: "main", Sha: first.Head,
		Checks: []string{"check"}, Reason: "check: fail (widget defaults to green)",
	}
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "red-main", Kind: ingress.RouteRedMain, Opens: ingress.KindIssue,
		Box: "shop", Repo: "app", Requester: "gargoyle",
	}, ingress.Delivery{Key: "red-" + first.Head, Payload: []byte(task), Prior: prior})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	repair, err := q.Submit(ctx, loop.ItemFor(ing, "wi-repair", task))
	if err != nil {
		t.Fatal(err)
	}
	if repair, err = lp.Run(ctx, repair); err != nil {
		t.Fatal(err)
	}
	if repair.State != queue.StateDone {
		t.Fatalf("the repair did not land: %+v", repair)
	}

	// What the repair was actually sent, from the record rather than from
	// this test's idea of it.
	var sent promptRecord
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-repair", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-prompt" {
			if err := json.Unmarshal(r.Body, &sent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if sent.AgentRef == "" {
		t.Fatal("no prompt on the repair's record")
	}
	if !strings.Contains(sent.Bundle, "prior-attempt") {
		t.Fatalf("the bundle does not carry a prior-attempt layer:\n%s", sent.Bundle)
	}
	_, carried, found := strings.Cut(sent.Prompt, "## The attempt before this one")
	if !found {
		t.Fatalf("the repair woke with no account of what preceded it:\n%s", sent.Prompt)
	}
	for _, want := range []string{
		first.ID,                   // whose attempt it was
		first.Head,                 // the sha that went red
		"main",                     // the branch that still carries it
		"widget defaults to green", // what the check said
		"expects blue",             // and the predecessor's own handoff
	} {
		if !strings.Contains(carried, want) {
			t.Fatalf("the carried attempt lost %q:\n%s", want, carried)
		}
	}
	// The same repair woken with continuity alone is a different agent, and
	// the agentref is where that difference is legible.
	if sent.AgentRef == cold.Ref {
		t.Fatal("carrying the prior attempt left no trace in the agentref")
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
