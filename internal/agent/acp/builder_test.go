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
	"github.com/ChristopherDavenport/project-box/internal/kind"
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
	// api and cmd/api exist so a unit naming the "api" section (see
	// TestTheSectionReachesThePromptAndTheRef and the context-narrowing
	// tests below) has real paths to materialize: `git archive` fails its
	// whole call on a pathspec that matches nothing, and a section's own
	// declared paths are never filtered around that the way the always-set
	// is.
	os.MkdirAll(filepath.Join(seedDir, "api"), 0o755)
	os.WriteFile(filepath.Join(seedDir, "api", "handler.go"), []byte("package api\n"), 0o644)
	os.MkdirAll(filepath.Join(seedDir, "cmd", "api"), 0o755)
	os.WriteFile(filepath.Join(seedDir, "cmd", "api", "main.go"), []byte("package main\n"), 0o644)
	// db and extra exist for the context-narrowing tests: db is a second
	// section's exclusive path (the union case), extra belongs to neither
	// declared section and must never be materialized for a sectioned run.
	os.MkdirAll(filepath.Join(seedDir, "db"), 0o755)
	os.WriteFile(filepath.Join(seedDir, "db", "schema.sql"), []byte("-- schema\n"), 0o644)
	os.MkdirAll(filepath.Join(seedDir, "extra"), 0o755)
	os.WriteFile(filepath.Join(seedDir, "extra", "only.txt"), []byte("neither section\n"), 0o644)
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
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
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
		Name: "red-main", Kind: ingress.RouteRedMain, Opens: kind.SeedIssue,
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

// TestTheEnvelopeInForceIsOnTheRecord is issue #17's record property, run
// through the whole loop rather than at the resolver.
//
// "This run was permitted to see class X and ran against endpoint Y" is the
// question a vendor-management or data-handling review asks, and it is
// unanswerable for any run that does not carry it. Both halves are here: the
// envelope on the chain, and — the half that costs nothing because the bundle
// is already content-addressed — a different envelope being a different agent.
func TestTheEnvelopeInForceIsOnTheRecord(t *testing.T) {
	// One classified run, start to landing, reporting what its agent-run
	// record said. A fresh rig per envelope, because the same agent making the
	// same change twice against one remote changes nothing the second time —
	// and a run that changed nothing is an error here, correctly.
	run := func(t *testing.T, readable []agent.Class) (ref, envelope string) {
		t.Helper()
		s, lp, router, _, _ := rig(t, "worker")
		b := lp.Builder.(*Builder)
		ls := layers()
		ls[0].Model = "vendor-inregion"
		b.Composer = &agent.Composer{Source: ls, Ceiling: &agent.Envelope{
			Endpoints: []string{"vendor-inregion"},
			Context:   readable,
			Payload:   agent.PayloadText,
		}}
		// What the origination declared. Nothing in box.yaml sets this: it
		// belongs on the governed kind declaration, which does not exist yet.
		b.Envelope = &agent.Envelope{
			Class: "embargoed", Endpoints: []string{"vendor-inregion", "kb"},
			Context: readable, Payload: agent.PayloadText,
		}

		item := runItem(t, s, lp, router, "issue-class", "add the widget package")
		if item.State != queue.StateDone {
			t.Fatalf("classified item: %+v", item)
		}
		var rec struct {
			AgentRef string `json:"agentref"`
			Envelope string `json:"envelope"`
		}
		for r, err := range s.Log().Since(context.Background(), btn, "workitem/wi-issue-class", 1) {
			if err != nil {
				t.Fatal(err)
			}
			if r.Kind == "agent-run" {
				if err := json.Unmarshal(r.Body, &rec); err != nil {
					t.Fatal(err)
				}
			}
		}
		return rec.AgentRef, rec.Envelope
	}

	// Unclassified is in the readable set alongside the declared class: the
	// run's own workspace is now a real context grant (issue #55) and it
	// carries no class of its own, so an envelope that cannot read
	// unclassified material could never read the repository it is building.
	ref, envelope := run(t, []agent.Class{"embargoed", agent.Unclassified})
	// The intersection is what is recorded, not the declaration: the run was
	// permitted one endpoint, and "kb" never reached it.
	want := "class=embargoed endpoints=[vendor-inregion] context=[embargoed ] redaction=all payload=text"
	if envelope != want {
		t.Fatalf("envelope on the record = %q, want %q", envelope, want)
	}

	// The same work under a wider envelope is a different agent on the record.
	widerRef, wider := run(t, []agent.Class{"embargoed", "public", agent.Unclassified})
	if wider == envelope {
		t.Fatalf("a widened envelope recorded identically: %q", wider)
	}
	if widerRef == "" || widerRef == ref {
		t.Fatalf("a widened envelope did not change the agentref (%q)", widerRef)
	}
}

// TestUnclassifiedWorkNeverReachesAnAgent is the fail-closed half at the
// loop's own boundary: a Box that declares a ceiling and work that declares no
// envelope parks before a token is spent. Unclassified is the state every
// mis-scoped request arrives in, so it is the state that has to be safe.
func TestUnclassifiedWorkNeverReachesAnAgent(t *testing.T) {
	s, lp, router, _, remote := rig(t, "worker")
	b := lp.Builder.(*Builder)
	b.Composer = &agent.Composer{Source: layers(), Ceiling: &agent.Envelope{
		Endpoints: []string{"vendor-inregion"}, Context: []agent.Class{"embargoed"}, Payload: agent.PayloadText,
	}}
	b.Envelope = nil // the mis-scoped request

	item := runItem(t, s, lp, router, "issue-unclassified", "add the widget package")
	if item.State != queue.StateParked || !strings.Contains(item.Note, "unclassified is refused") {
		t.Fatalf("unclassified work was not refused: %+v", item)
	}
	if out := mustSh(t, remote, "git", "rev-list", "--count", "refs/heads/main"); strings.TrimSpace(out) != "1" {
		t.Fatal("unclassified work moved the remote")
	}
}

// TestTheSessionIsOnTheRecord. The agent-run record says what was asked, as
// a digest. This says what was answered, in the clear: the model that ran,
// the mode in force, every option and every command the agent offered — the
// difference between "the skill was granted" and "the skill was delivered",
// which round 4 of the dogfood could not tell apart from its own chain.
func TestTheSessionIsOnTheRecord(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ls := layers()
	ls[0].Model = "sonnet"
	ls[0].Session = map[string]string{"effort": "low"}
	lp.Builder.(*Builder).Composer = &agent.Composer{Source: ls}
	ctx := context.Background()
	item := runItem(t, s, lp, router, "issue-session", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("item: %+v", item)
	}
	var session, result string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-session", 1) {
		if err != nil {
			t.Fatal(err)
		}
		switch r.Kind {
		case "agent-session":
			session = string(r.Body)
		case "agent-result":
			result = string(r.Body)
		}
	}
	if session == "" {
		t.Fatal("no agent-session on the chain")
	}
	var rec struct {
		Model    string                         `json:"model"`
		Modes    struct{ Current string }       `json:"modes"`
		Commands []string                       `json:"commands"`
		Config   []struct{ ID, Current string } `json:"config"`
	}
	if err := json.Unmarshal([]byte(session), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Model != "sonnet" || rec.Modes.Current != "acceptEdits" {
		t.Fatalf("session record: %s", session)
	}
	if !strings.Contains(strings.Join(rec.Commands, ","), "box-skills:go") {
		t.Fatalf("the delivered skills are not on the record: %s", session)
	}
	effort := ""
	for _, c := range rec.Config {
		if c.ID == "effort" {
			effort = c.Current
		}
	}
	if effort != "low" {
		t.Fatalf("session option not on the record: %s", session)
	}
	// And the result names the model beside the tokens it explains.
	if !strings.Contains(result, `"model":"sonnet"`) {
		t.Fatalf("agent-result does not say which model ran:\n%s", result)
	}
}

// TestTheSessionRecordCarriesEnvNamesNoValues: the names a run's child
// process was actually given ride the agent-session record, and never a
// value — an evidence chain that carried the value would be a credential
// store with extra steps. Not t.Parallel: t.Setenv forbids it.
func TestTheSessionRecordCarriesEnvNamesNoValues(t *testing.T) {
	t.Setenv("ACP_ENV_CANARY", "super-secret-value")
	s, lp, router, _, _ := rig(t, "worker")
	b := lp.Builder.(*Builder)
	b.EnvNames = []string{"ACP_ENV_CANARY"}
	ctx := context.Background()
	item := runItem(t, s, lp, router, "issue-envrecord", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("item: %+v", item)
	}
	var session string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-envrecord", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-session" {
			session = string(r.Body)
		}
	}
	if session == "" {
		t.Fatal("no agent-session on the chain")
	}
	if strings.Contains(session, "super-secret-value") {
		t.Fatalf("a credential value reached the evidence chain:\n%s", session)
	}
	var rec struct {
		Env []string `json:"env"`
	}
	if err := json.Unmarshal([]byte(session), &rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rec.Env, ","), "ACP_ENV_CANARY") {
		t.Fatalf("the delivered name is not on the record: %v", rec.Env)
	}
}

// TestTheSectionReachesThePromptAndTheRef. A section is a layer: the agent is
// told where it works and what to run while there, the layer is on the run
// record, and a plan's tier for the unit reaches the session and the result.
func TestTheSectionReachesThePromptAndTheRef(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	// The loop leases the section and the builder describes it: both read
	// the same repository definition, as the daemon hands them the same one.
	lp.Repo.Sections = []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"api", "cmd/api"}, Check: "go test ./api/..."},
	}
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{lp.Repo}
	ctx := context.Background()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-section"})
	if err != nil {
		t.Fatal(err)
	}
	item := loop.ItemFor(ing, "wi-issue-section", "add the widget package")
	item.Sections, item.Model = []string{"api"}, "haiku"
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	if item, err = q.Submit(ctx, item); err != nil {
		t.Fatal(err)
	}
	if item, err = lp.Run(ctx, item); err != nil || item.State != queue.StateDone {
		t.Fatalf("run: %v %+v", err, item)
	}
	var prompt, run, result string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-section", 1) {
		if err != nil {
			t.Fatal(err)
		}
		switch r.Kind {
		case "agent-prompt":
			var p struct{ Prompt string }
			json.Unmarshal(r.Body, &p)
			prompt = p.Prompt
		case "agent-run":
			run = string(r.Body)
		case "agent-result":
			result = string(r.Body)
		}
	}
	for _, want := range []string{"## Section: api", "the HTTP surface", "api, cmd/api", "go test ./api/..."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(run, `"name":"section-api"`) || !strings.Contains(run, `"scope":"section"`) {
		t.Errorf("the section layer is not on the run record:\n%s", run)
	}
	if !strings.Contains(result, `"model":"haiku"`) {
		t.Errorf("the plan's tier did not reach the session:\n%s", result)
	}
	// A unit that names a section is scoped to it, not to the whole
	// repository — the two layers describe different agents.
	if strings.Contains(run, `"name":"repo-app"`) {
		t.Errorf("a sectioned unit also got the whole-repo layer:\n%s", run)
	}
}

// TestThePromptNamesTheWorkspace: issue #73. A builder inferred the
// operator's checkout from its own cwd because nothing ever told it where it
// actually was; the prompt now says so, in the words that back the runtime's
// own path judgement of file-shaped tool calls.
func TestThePromptNamesTheWorkspace(t *testing.T) {
	got := prompt(agent.Bundle{}, "the task", "", "/work/tree-1")
	for _, want := range []string{"/work/tree-1", "repository root", "refused"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
	// A Build with no workspace path (a probe, an experiment) gets no
	// section naming one, the same way an empty contextDir gets no
	// "Reference material" section.
	got = prompt(agent.Bundle{}, "the task", "", "")
	if strings.Contains(got, "## Where you are") {
		t.Errorf("an empty workspace path still produced a section naming one:\n%s", got)
	}
}

// TestAnItemNamingAnUndeclaredSectionIsRefused: running it unscoped would be
// the silent widening a section exists to prevent.
func TestAnItemNamingAnUndeclaredSectionIsRefused(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: "issue-nowhere"})
	if err != nil {
		t.Fatal(err)
	}
	item := loop.ItemFor(ing, "wi-issue-nowhere", "add the widget package")
	item.Sections = []string{"db"}
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	if item, err = q.Submit(ctx, item); err != nil {
		t.Fatal(err)
	}
	// The loop's repo declares no sections either, so the loop parks it
	// before the builder is reached; the builder's own refusal is asserted
	// directly.
	if _, err := lp.Builder.Build(ctx, forge.Workspace{Run: item.ID, Repo: "app", Dir: t.TempDir()}, item.Task); err == nil || !strings.Contains(err.Error(), `"db"`) {
		t.Fatalf("an undeclared section was not refused: %v", err)
	}
}

// TestAnUnsectionedUnitGetsTheRepositoryCheck: a unit that names no section
// is still the whole repository, and the whole repository still has a check
// to run while iterating — the gap issue #46 named (docs/PLAN.md's "Units of
// expression": "an unsectioned unit is the whole repository; the section
// reaches the builder as a layer on the agentref").
func TestAnUnsectionedUnitGetsTheRepositoryCheck(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{{Name: "app", Check: "go test ./..."}}
	ctx := context.Background()
	item := runItemCtx(t, ctx, s, lp, router, "issue-repo-check", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("run: %+v", item)
	}
	var prompt, run string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-repo-check", 1) {
		if err != nil {
			t.Fatal(err)
		}
		switch r.Kind {
		case "agent-prompt":
			var p struct{ Prompt string }
			json.Unmarshal(r.Body, &p)
			prompt = p.Prompt
		case "agent-run":
			run = string(r.Body)
		}
	}
	if !strings.Contains(prompt, "go test ./...") {
		t.Errorf("the prompt does not carry the repository's check:\n%s", prompt)
	}
	if !strings.Contains(run, `"name":"repo-app"`) || !strings.Contains(run, `"scope":"repo"`) {
		t.Errorf("the whole-repo layer is not on the run record:\n%s", run)
	}
}

// TestAnUnsectionedUnitWithNoCheckIsToldPlainly: silence here would read as
// "nothing to run" and "nobody configured one" identically — the layer must
// say which.
func TestAnUnsectionedUnitWithNoCheckIsToldPlainly(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{{Name: "app"}}
	ctx := context.Background()
	item := runItemCtx(t, ctx, s, lp, router, "issue-repo-nocheck", "add the widget package")
	if item.State != queue.StateDone {
		t.Fatalf("run: %+v", item)
	}
	var prompt string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-repo-nocheck", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-prompt" {
			var p struct{ Prompt string }
			json.Unmarshal(r.Body, &p)
			prompt = p.Prompt
		}
	}
	if !strings.Contains(prompt, "No check is configured") {
		t.Errorf("the prompt does not say plainly that no check is configured:\n%s", prompt)
	}
}

// checkoutWorkspace materializes a real workspace directly — the same lease
// and Mirror.Checkout call loop.Loop.Run makes internally — so a test can
// hold ws.Dir and inspect its materialized context sibling after Build
// returns. A direct Build call tears down neither: reclaiming a workspace is
// the mirror's Prune, which nothing here calls.
func checkoutWorkspace(t *testing.T, ctx context.Context, s *sqlite.Store, lp *loop.Loop, run string) forge.Workspace {
	t.Helper()
	base, err := lp.Mirror.Resolve(ctx, lp.Repo, "refs/heads/"+lp.Branch)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: btn, Repo: lp.Repo.Name, Ref: "run/" + run, Run: run, TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := lp.Mirror.Checkout(ctx, lease, base)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// submitSectioned opens and submits a work item naming the given sections,
// returning it for a direct Build call — the loop's claim/gate/land
// machinery is not what these tests are exercising.
func submitSectioned(t *testing.T, ctx context.Context, s *sqlite.Store, router *ingress.StoreRouter, key, task string, sections []string) queue.WorkItem {
	t.Helper()
	ing, _, err := router.Open(ctx, ingress.Route{
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "chris",
	}, ingress.Delivery{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	item := loop.ItemFor(ing, "wi-"+key, task)
	item.Sections = sections
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	item, err = q.Submit(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestASectionedUnitMaterializesItsPathsPlusTheAlwaysSet is issue #55's core
// property: a unit naming one section gets that section's paths and the
// (filtered) always-set under its context directory, nothing else — and the
// workspace itself stays the complete checkout regardless, since narrowing
// applies to the context grant, not to what the agent's own tool calls can
// still reach in ws.Dir.
func TestASectionedUnitMaterializesItsPathsPlusTheAlwaysSet(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	lp.Repo.Sections = []forge.Section{
		{Name: "api", Paths: []string{"api", "cmd/api"}},
		{Name: "db", Paths: []string{"db"}},
	}
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{lp.Repo}

	item := submitSectioned(t, ctx, s, router, "issue-ctx-one", "touch the api section", []string{"api"})
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(forge.ContextDir(ws.Dir), lp.Repo.Name)
	for _, want := range []string{"api/handler.go", "cmd/api/main.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("the materialized context is missing %q: %v", want, err)
		}
	}
	for _, absent := range []string{"db", "extra", "CLAUDE.md", "go.mod", "go.sum", "docs"} {
		if _, err := os.Stat(filepath.Join(root, absent)); err == nil {
			t.Errorf("the materialized context carries %q, which the unit was not granted", absent)
		}
	}

	// The workspace is the complete checkout either way: narrowing the
	// context grant does not shrink what the agent's own tool calls reach.
	for _, want := range []string{"db/schema.sql", "extra/only.txt", "api/handler.go"} {
		if _, err := os.Stat(filepath.Join(ws.Dir, want)); err != nil {
			t.Errorf("the workspace is missing %q though narrowing must not touch it: %v", want, err)
		}
	}
}

// TestTwoNamedSectionsMaterializeTheUnion is the regression
// TestTheSectionReachesThePromptAndTheRef cannot catch: Composer.Resolve
// merges Layer.Context.Repos by repo name with the LAST layer winning
// (resolve.go:179-184), so emitting one layer per section would silently
// keep only the last section's paths. The union has to be computed once, in
// builder.go, and carried as a single layer.
func TestTwoNamedSectionsMaterializeTheUnion(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	lp.Repo.Sections = []forge.Section{
		{Name: "api", Paths: []string{"api", "cmd/api"}},
		{Name: "db", Paths: []string{"db"}},
	}
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{lp.Repo}

	item := submitSectioned(t, ctx, s, router, "issue-ctx-two", "touch api and db", []string{"api", "db"})
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(forge.ContextDir(ws.Dir), lp.Repo.Name)
	for _, want := range []string{"api/handler.go", "cmd/api/main.go", "db/schema.sql", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("the union is missing %q: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "extra")); err == nil {
		t.Error("the materialized context carries \"extra\", which neither named section covers")
	}
}

// TestAnUnnarrowedRunMaterializesTheWholeTree covers both ways a unit runs
// unnarrowed: naming no section, and naming no item at all (a probe or an
// experiment, per builder.go's own doc comment on the item lookup).
func TestAnUnnarrowedRunMaterializesTheWholeTree(t *testing.T) {
	cases := []struct {
		name string
		item bool
	}{
		{"unsectioned item", true},
		{"no item at all", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, lp, router, _, _ := rig(t, "worker")
			ctx := context.Background()
			lp.Repo.Sections = []forge.Section{{Name: "api", Paths: []string{"api"}}}
			b := lp.Builder.(*Builder)
			b.Repos = []forge.Repo{lp.Repo}

			run := "wi-ctx-whole-" + strings.ReplaceAll(c.name, " ", "-")
			task := "touch nothing in particular"
			if c.item {
				item := submitSectioned(t, ctx, s, router, "issue-ctx-whole-"+c.name, task, nil)
				run = item.ID
			}
			ws := checkoutWorkspace(t, ctx, s, lp, run)
			if _, err := b.Build(ctx, ws, task); err != nil {
				t.Fatal(err)
			}

			root := filepath.Join(forge.ContextDir(ws.Dir), lp.Repo.Name)
			for _, want := range []string{"api/handler.go", "db/schema.sql", "extra/only.txt", "README.md"} {
				if _, err := os.Stat(filepath.Join(root, want)); err != nil {
					t.Errorf("an unnarrowed run is missing %q from the whole tree: %v", want, err)
				}
			}
		})
	}
}

// TestASectionedAgentrefDiffersFromAnUnsectionedOne confirms the context
// grant's digest reaches the agentref through the machinery that already
// exists for it (resolve.go's ref over r.Paths) — a sectioned unit and an
// otherwise-identical unsectioned one are different agents on the record,
// not just different prompts.
func TestASectionedAgentrefDiffersFromAnUnsectionedOne(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	lp.Repo.Sections = []forge.Section{{Name: "api", Paths: []string{"api"}}}
	b := lp.Builder.(*Builder)
	b.Repos = []forge.Repo{lp.Repo}
	task := "touch the api section"

	sectioned := submitSectioned(t, ctx, s, router, "issue-ref-sectioned", task, []string{"api"})
	sectionedWS := checkoutWorkspace(t, ctx, s, lp, sectioned.ID)
	if _, err := b.Build(ctx, sectionedWS, task); err != nil {
		t.Fatal(err)
	}

	unsectioned := submitSectioned(t, ctx, s, router, "issue-ref-unsectioned", task, nil)
	unsectionedWS := checkoutWorkspace(t, ctx, s, lp, unsectioned.ID)
	if _, err := b.Build(ctx, unsectionedWS, task); err != nil {
		t.Fatal(err)
	}

	agentref := func(item string) string {
		var ref string
		for r, err := range s.Log().Since(ctx, btn, "workitem/"+item, 1) {
			if err != nil {
				t.Fatal(err)
			}
			if r.Kind == "agent-run" {
				var rec struct {
					AgentRef string `json:"agentref"`
				}
				if err := json.Unmarshal(r.Body, &rec); err != nil {
					t.Fatal(err)
				}
				ref = rec.AgentRef
			}
		}
		return ref
	}

	sectionedRef, unsectionedRef := agentref(sectioned.ID), agentref(unsectioned.ID)
	if sectionedRef == "" || unsectionedRef == "" {
		t.Fatalf("missing agentref: sectioned=%q unsectioned=%q", sectionedRef, unsectionedRef)
	}
	if sectionedRef == unsectionedRef {
		t.Fatalf("a sectioned run and an unsectioned run of the same task shared an agentref: %q", sectionedRef)
	}
}
