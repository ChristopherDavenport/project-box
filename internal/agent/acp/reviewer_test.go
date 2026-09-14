package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// gateLayers is a base layer that states acceptEdits for everything, which is
// what the daemon's own base layer states today. The reviewer's contract layer
// has to out-restrict it; see TestTheGateJudgesInPlanMode.
func gateLayers() agent.StaticLayers {
	return agent.StaticLayers{{
		Name: "base", Scope: agent.ScopeBase,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: "d-base"},
		Instructions: "You work in the shop box.",
		Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
	}}
}

// reviewerRig builds a workspace holding both ends of a real change, a work
// item that says what was asked, and a Reviewer over them.
func reviewerRig(t *testing.T, scenario string) (*Reviewer, forge.ChangeSet, *sqlite.Store, *seat.Seat) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// The workspace: a tree with a base commit and a built head, the way the
	// loop hands one to a gate. The mirror could not supply either end of
	// this — the head was never pushed anywhere.
	ws := filepath.Join(t.TempDir(), "app")
	mustSh(t, "", "git", "init", ws)
	mustSh(t, ws, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	os.MkdirAll(filepath.Join(ws, "docs"), 0o755)
	os.WriteFile(filepath.Join(ws, "docs", "PLAN.md"), []byte(
		"## 5. Testing strategy\n\nrun the tests\n\n"+
			"## 6. Decisions already settled\n\n- The tenant is explicit on every key.\n- Unknown never reads as zero.\n\n"+
			"## 7. Deferred decisions\n\nnot yet\n"), 0o644)
	os.MkdirAll(filepath.Join(ws, "db"), 0o755)
	os.WriteFile(filepath.Join(ws, "db", "schema.sql"), []byte("-- seed schema\n"), 0o644)
	mustSh(t, ws, "git", "add", ".")
	mustSh(t, ws, "git", "commit", "-m", "seed")
	base := strings.TrimSpace(mustSh(t, ws, "git", "rev-parse", "HEAD"))

	os.MkdirAll(filepath.Join(ws, "api"), 0o755)
	os.WriteFile(filepath.Join(ws, "api", "widget.go"), []byte("package api // the widget, built by the unit\n"), 0o644)
	mustSh(t, ws, "git", "add", ".")
	mustSh(t, ws, "git", "commit", "-m", "add the widget")
	head := strings.TrimSpace(mustSh(t, ws, "git", "rev-parse", "HEAD"))

	repo := forge.Repo{Name: "app", Remote: ws, DefaultBranch: "main", Sections: []forge.Section{
		{Name: "api", Purpose: "the HTTP surface", Paths: []string{"api"}},
		{Name: "db", Purpose: "the schema", Paths: []string{"db"}},
	}}

	ctx := context.Background()
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-widget", Tenant: btn, Box: "shop", Ingress: "ing-1", Requester: "chris",
		Repo: "app", Task: "add the widget package under api/", Summary: "The widget package",
		Plan: "plan-abc", Node: "api-endpoint", Sections: []string{"api"},
	})
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	// A sealed tree denies its own unlink, so t.TempDir's cleanup fails on
	// one a failing test left behind. Production reclaims through
	// RemoveContext, which restores write on the way down for exactly this
	// reason; cleanups run last-registered-first, so this one runs before the
	// removal it clears the way for.
	t.Cleanup(func() {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o755)
			}
			return nil
		})
	})

	st := &seat.Seat{Name: "mason-gate", Base: &knowledge.Base{Store: s, Tenant: btn}}
	rv := &Reviewer{
		Store: s, Tenant: btn, Box: "shop",
		Command:   []string{os.Args[0]},
		Env:       []string{"ACP_FAKE=" + scenario},
		Composer:  &agent.Composer{Source: gateLayers()},
		Seat:      st,
		Repo:      repo,
		Workspace: forge.Workspace{Repo: "app", Run: item.ID, Dir: ws, Base: forge.Commit(base)},
		Root:      root,
	}
	cs := forge.ChangeSet{
		ID: "cs-" + item.ID, Tenant: btn, Box: "shop", Run: item.ID, Intent: item.Task,
		Members: []forge.Member{{Repo: "app", Branch: "main",
			Base: forge.Commit(base), Head: forge.Commit(head), Summary: item.Summary}},
	}
	return rv, cs, s, st
}

// agentText is the agent's own message, out of the record that carries it.
// The record body is JSON, so the text has to come back through the shape
// that put it there rather than off the raw bytes.
func agentText(t *testing.T, s *sqlite.Store, run string) string {
	t.Helper()
	var tr textRecord
	if err := json.Unmarshal([]byte(chain(t, s, run)["agent-text"]), &tr); err != nil {
		t.Fatalf("agent-text: %v", err)
	}
	return tr.Text
}

// gateRecord appends a "gate" record to run's chain the way the loop does: a
// [forge.Verdict], marshalled with its own default field names, subject to
// the head sha it judged — the shape [queue.PriorVerdicts] reads back.
func gateRecord(t *testing.T, s *sqlite.Store, run, sha string, v forge.Verdict) {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log().Append(context.Background(), evidence.Record{
		Tenant: btn, Scope: "workitem/" + run, Kind: "gate", Subject: sha,
		Actor: "runner-1", Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// chain folds a work item's evidence by kind.
func chain(t *testing.T, s *sqlite.Store, run string) map[string]string {
	t.Helper()
	kinds := map[string]string{}
	for r, err := range s.Log().Since(context.Background(), btn, "workitem/"+run, 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind] = string(r.Body)
	}
	return kinds
}

// TestTheGateJudgesTheDiff: the reviewer reads base against head, never the
// base alone, so a rebase that replays the identical patch onto a moved base
// may carry this gate's verdict forward. gate.Command and the human-approval
// gate deliberately do not implement forge.DiffJudge — a check re-runs
// against whatever tree it is handed, and an approval binds to the exact
// commit a person looked at, never to a patch shape.
func TestTheGateJudgesTheDiff(t *testing.T) {
	var g forge.Gate = &Reviewer{}
	dj, ok := g.(forge.DiffJudge)
	if !ok {
		t.Fatal("*Reviewer does not satisfy forge.DiffJudge")
	}
	if !dj.JudgesDiff() {
		t.Fatal("the reviewer must report that it judges the diff")
	}
}

// TestTheGateReadsAChangeAndPasses: the gate role's seat, occupied. The
// verdict carries this gate's name and its reason, and the whole account of
// the judgement — the agentref before the run, the model that judged, the
// answer — is on the WORK ITEM's chain beside the build it reviewed.
func TestTheGateReadsAChangeAndPasses(t *testing.T) {
	rv, cs, s, st := reviewerRig(t, "review-pass")
	ctx := context.Background()
	v, err := rv.Evaluate(ctx, cs)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Gate != ReviewerName || v.Decision != forge.DecisionPass {
		t.Fatalf("verdict: %+v", v)
	}
	if !strings.Contains(v.Reason, "inside the section it declared") {
		t.Fatalf("the reason did not survive: %+v", v)
	}
	kinds := chain(t, s, cs.Run)
	for _, want := range []string{"agent-run", "agent-prompt", "agent-session", "agent-result", "agent-text"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("no %s on the work item's chain (have %v)", want, keysOf(kinds))
		}
	}
	if !strings.Contains(kinds["agent-run"], `"role":"gate"`) {
		t.Errorf("the reviewer ran as something else:\n%s", kinds["agent-run"])
	}
	// The agentref is on the record before the run, and the model that judged
	// is on it after: the run record cannot carry the model, because the
	// session that reports it does not exist yet when the run record lands.
	sawRef, sawModel := false, false
	for r, err := range s.Log().Since(ctx, btn, "workitem/"+cs.Run, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-run" && len(r.Subject) == 64 && r.Actor == "mason-gate" && r.OnBehalfOf == "chris" {
			sawRef = true
		}
		if r.Kind == "agent-session" && strings.Contains(string(r.Body), `"model":"opus"`) {
			sawModel = true
		}
	}
	if !sawRef {
		t.Error("no agentref on the work item's chain, attributed to the seat and the requester")
	}
	if !sawModel {
		t.Errorf("the model that judged is not on the record:\n%s", kinds["agent-session"])
	}
	// The session record names its own role and carries the session id — a
	// continuation looking for the newest BUILDER session must be able to
	// tell a reviewer's record apart from one on the same workitem/<id>
	// scope, even though both ride the same sessionRecord shape.
	if !strings.Contains(kinds["agent-session"], `"session":"sess-1"`) || !strings.Contains(kinds["agent-session"], `"role":"reviewer"`) {
		t.Errorf("the session record does not carry its id and role:\n%s", kinds["agent-session"])
	}
	// The reviewer was shown the origination, its section and its purpose.
	for _, want := range []string{"add the widget package under api/", "the HTTP surface", "plan-abc"} {
		if !strings.Contains(kinds["agent-prompt"], want) {
			t.Errorf("the reviewer's material lost %q", want)
		}
	}
	// Continuity, bound to the commit it judged rather than to nothing.
	notes, err := st.HandoffFrom(ctx, cs.Run)
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0], "default is blue") {
		t.Fatalf("the review's handoff did not reach its seat: %v %q", err, notes)
	}
	recs, err := st.Base.Retrieve(ctx, knowledge.Query{Granted: []string{st.Scope()}})
	if err != nil || len(recs) != 1 || recs[0].Commit != string(cs.Members[0].Head) {
		t.Fatalf("the note is not bound to the commit it judged: %v %+v", err, recs)
	}
}

// TestTheGateSessionRecordCarriesEnvNamesNoValues: the reviewer writes the
// same sessionRecord shape a build does, from its own call site — a gate's
// record must not read "unknown" just because only the builder was wired.
// Not t.Parallel: t.Setenv forbids it.
func TestTheGateSessionRecordCarriesEnvNamesNoValues(t *testing.T) {
	t.Setenv("ACP_ENV_CANARY", "super-secret-value")
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	rv.EnvNames = []string{"ACP_ENV_CANARY"}
	ctx := context.Background()
	if _, err := rv.Evaluate(ctx, cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	session := chain(t, s, cs.Run)["agent-session"]
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

// TestTheGateRunnerRidesEveryRecordOnlyWhenDeclared: the same runner-carrying
// contract [TestTheRunnerRidesEveryRecordOnlyWhenDeclared] asserts for a
// build, from the reviewer's own call site.
func TestTheGateRunnerRidesEveryRecordOnlyWhenDeclared(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	rv.Runner = "gha-runner-1"
	ctx := context.Background()
	if _, err := rv.Evaluate(ctx, cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	kinds := chain(t, s, cs.Run)
	for _, name := range []string{"agent-run", "agent-session", "agent-result"} {
		if !strings.Contains(kinds[name], `"runner":"gha-runner-1"`) {
			t.Fatalf("the declared runner is not on %s:\n%s", name, kinds[name])
		}
	}
}

// TestTheGateFailsWithItsReason: a rejection travels as a rejection, with the
// words a builder has to act on.
func TestTheGateFailsWithItsReason(t *testing.T) {
	rv, cs, _, _ := reviewerRig(t, "review-fail")
	v, err := rv.Evaluate(context.Background(), cs)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Gate != ReviewerName || v.Decision != forge.DecisionFail {
		t.Fatalf("verdict: %+v", v)
	}
	if !strings.Contains(v.Reason, "db/schema.sql") {
		t.Fatalf("the reason did not survive: %+v", v)
	}
}

// TestTheGateWindowSpentRefusalCarriesTheReset: the run's own account report
// that the window is exhausted, met at the gate rather than at a build, comes
// back from Evaluate as *loop.WindowSpent — loop.evaluateGate errors.As-matches
// this to defer the gate to the reset rather than failing it outright
// (issue #112).
func TestTheGateWindowSpentRefusalCarriesTheReset(t *testing.T) {
	rv, cs, _, _ := reviewerRig(t, "windowspent")
	_, err := rv.Evaluate(context.Background(), cs)
	var spent *loop.WindowSpent
	if !errors.As(err, &spent) {
		t.Fatalf("a rejected window was not classified as *loop.WindowSpent: %v", err)
	}
	if spent.Window != "five_hour" {
		t.Fatalf("the window name did not survive: %+v", spent)
	}
	if want := time.Unix(1788491400, 0); !spent.ResetsAt.Equal(want) {
		t.Fatalf("the reset did not survive: got %v want %v", spent.ResetsAt, want)
	}
}

// TestAnAnswerWithNoJudgementIsTrouble: the reviewer said nothing about the
// change. [forge.Gate]'s contract is that infrastructure trouble is never
// recorded as a rejected change, and an unread answer is not a "no" — it is
// not an answer.
func TestAnAnswerWithNoJudgementIsTrouble(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		want     string
	}{
		{"review-silent", "no judgement"},
		{"review-neither", `"maybe"`},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			rv, cs, _, _ := reviewerRig(t, tc.scenario)
			v, err := rv.Evaluate(context.Background(), cs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %s, got %v", tc.want, err)
			}
			if v.Gate != "" || v.Decision != "" || v.Reason != "" {
				t.Fatalf("trouble was recorded as a verdict: %+v", v)
			}
		})
	}
}

// TestAFailWithNoReasonIsTrouble: a rejection nobody can act on is not a
// rejection, and an empty reason is more likely a truncated answer than a
// considered one.
func TestAFailWithNoReasonIsTrouble(t *testing.T) {
	if _, err := parseVerdict("```json\n"+`{"verdict":"fail","reason":"  "}`+"\n```", nil); err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Fatalf("a reasonless rejection was accepted: %v", err)
	}
	// And a pass with no reason is a pass: there is nobody to act on it.
	v, err := parseVerdict(`{"verdict":"pass","reason":""}`, nil)
	if err != nil || v.Decision != forge.DecisionPass {
		t.Fatalf("a terse pass was refused: %+v %v", v, err)
	}
}

// TestAnUnreadableWorkItemIsTrouble: Sections, Plan and Node are not on the
// ChangeSet, so a review that cannot read the item does not know what was
// asked. Unknown never reads as zero, and here it does not read as a fail
// either.
func TestAnUnreadableWorkItemIsTrouble(t *testing.T) {
	rv, cs, _, _ := reviewerRig(t, "review-pass")
	cs.Run = "wi-never-submitted"
	v, err := rv.Evaluate(context.Background(), cs)
	if err == nil || !strings.Contains(err.Error(), "wi-never-submitted") {
		t.Fatalf("an unreadable item was not refused by name: %v", err)
	}
	if v.Gate != "" || v.Decision != "" || v.Reason != "" {
		t.Fatalf("an unreadable item became a verdict: %+v", v)
	}
}

// TestTheGateJudgesInPlanMode: roleDefaultMode applies only when NO layer
// states a mode, and the daemon's base layer states acceptEdits for every
// role. The reviewer's own layer states plan, which out-ranks it — so the
// mode is on the record as a fact rather than inherited by accident.
func TestTheGateJudgesInPlanMode(t *testing.T) {
	ctx := context.Background()
	// Without the reviewer's layer, the gate role inherits acceptEdits: this
	// is the thing being defended against, asserted so the defence cannot
	// quietly stop being needed.
	c := &agent.Composer{Source: gateLayers()}
	loose, err := c.Resolve(ctx, agent.Request{Tenant: btn, Role: agent.RoleGate, Box: "shop", Run: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if loose.Tools.PermissionMode != "acceptEdits" {
		t.Fatalf("the premise has changed: a bare gate bundle resolves to %q", loose.Tools.PermissionMode)
	}
	tight, err := c.Resolve(ctx, agent.Request{Tenant: btn, Role: agent.RoleGate, Box: "shop", Run: "r",
		Extra: []agent.Layer{reviewerLayer()}})
	if err != nil {
		t.Fatal(err)
	}
	if tight.Tools.PermissionMode != "plan" {
		t.Fatalf("the reviewer's stated mode lost to a broader layer: %q", tight.Tools.PermissionMode)
	}
	// And through a whole run, on the record.
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	if _, err := rv.Evaluate(ctx, cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !strings.Contains(chain(t, s, cs.Run)["agent-run"], `"mode":"plan"`) {
		t.Errorf("the record does not say the review was read-only:\n%s", chain(t, s, cs.Run)["agent-run"])
	}
}

// TestTheGateDoesNotStandInTheWorkspace: the material is a tree of its own,
// and it will not take a write. A gate judging from inside the tree it judges
// could edit its way to a pass.
func TestTheGateDoesNotStandInTheWorkspace(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	if _, err := rv.Evaluate(context.Background(), cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	text := agentText(t, s, cs.Run)
	if strings.Contains(text, "cwd:"+rv.Workspace.Dir+" ") {
		t.Fatalf("the reviewer stood in the workspace:\n%s", text)
	}
	if !strings.Contains(text, "writable:no") {
		t.Fatalf("the reviewer's reading material took a write:\n%s", text)
	}
	// And it is reclaimed: a sealed tree left behind is a tree nothing sweeps.
	if _, err := os.Stat(filepath.Join(rv.Root, cs.Run)); !os.IsNotExist(err) {
		t.Fatalf("the material outlived the review: %v", err)
	}
}

// TestTheDiffIsHeadAgainstBase: what the reviewer reads is the change, not the
// repository — the file the unit added, and nothing the base already had.
func TestTheDiffIsHeadAgainstBase(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	if _, err := rv.Evaluate(context.Background(), cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	text := agentText(t, s, cs.Run)
	patch := between(t, text, "--- diff.patch ---\n", "--- end of diff.patch ---")
	for _, want := range []string{"b/api/widget.go", "+package api // the widget, built by the unit"} {
		if !strings.Contains(patch, want) {
			t.Errorf("the diff lost %q:\n%s", want, patch)
		}
	}
	if strings.Contains(patch, "seed schema") {
		t.Errorf("the diff carried the base's own content, so it is not head against base:\n%s", patch)
	}
	if strings.Contains(patch, "TRUNCATED") {
		t.Errorf("a whole diff claimed to be truncated:\n%s", patch)
	}
}

// TestATruncatedDiffSaysSo: a reviewer that silently read half a change and
// passed it is the failure this gate exists to prevent, so the cut is stated
// where the agent is reading and again in the prompt.
func TestATruncatedDiffSaysSo(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	was := maxDiffBytes
	maxDiffBytes = 120
	t.Cleanup(func() { maxDiffBytes = was })

	if _, err := rv.Evaluate(context.Background(), cs); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	kinds := chain(t, s, cs.Run)
	patch := between(t, agentText(t, s, cs.Run), "--- diff.patch ---\n", "--- end of diff.patch ---")
	if !strings.Contains(patch, "*** TRUNCATED") || !strings.Contains(patch, "you have NOT been shown the whole change") {
		t.Fatalf("the cut was not stated in the material:\n%s", patch)
	}
	if !strings.Contains(kinds["agent-prompt"], "THE PATCH IS TRUNCATED") {
		t.Fatalf("the cut was not stated in the prompt:\n%s", kinds["agent-prompt"])
	}
	// Cut at a line boundary, and bounded: the banner is the only thing past
	// the cap.
	body, _, _ := strings.Cut(patch, "\n*** TRUNCATED")
	if len(body) > maxDiffBytes {
		t.Fatalf("the cap did not hold: %d bytes shown", len(body))
	}
}

// TestTheSettledDecisionsComeFromTheBuiltTree: §6 as it stood in the tree the
// change was made against, not as it stands wherever the review runs. A
// document with no such heading is stated as having none — an absent section
// read as an empty one would let a change contradict every decision there is
// and be told it contradicted nothing.
func TestTheSettledDecisionsComeFromTheBuiltTree(t *testing.T) {
	rv, cs, _, _ := reviewerRig(t, "review-pass")
	got, ok, err := rv.settled(context.Background(), string(cs.Members[0].Head))
	if err != nil || !ok {
		t.Fatalf("settled: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(got, "Unknown never reads as zero") {
		t.Fatalf("the decisions did not arrive:\n%s", got)
	}
	if strings.Contains(got, "Deferred decisions") || strings.Contains(got, "Testing strategy") {
		t.Fatalf("the section was not bounded by the next heading:\n%s", got)
	}
	if _, ok := settledSection("## 7. Deferred decisions\n\nnot yet\n"); ok {
		t.Fatal("a document with no settled decisions reported some")
	}
	// A repository with no PLAN.md at all says so rather than reading empty.
	absent, ok, err := rv.settled(context.Background(), string(cs.Members[0].Base)+":")
	if err != nil || ok || !strings.Contains(absent, "no docs/PLAN.md") {
		t.Fatalf("an unreadable plan document did not say so: ok=%v err=%v %q", ok, err, absent)
	}
}

// between cuts the text a fake agent bracketed for the test to read back.
func between(t *testing.T, text, open, close string) string {
	t.Helper()
	_, after, ok := strings.Cut(text, open)
	if !ok {
		t.Fatalf("no %q in:\n%s", open, text)
	}
	body, _, ok := strings.Cut(after, close)
	if !ok {
		t.Fatalf("no %q in:\n%s", close, text)
	}
	return body
}

// skillRepo is a skill repository with one pinned skill, for a reviewer to
// be granted: the shape box.yaml's `harness.skills` names.
func skillRepo(t *testing.T) (remote, commit string) {
	t.Helper()
	remote = t.TempDir()
	mustSh(t, remote, "git", "init", "-q", "-b", "main")
	mustSh(t, remote, "git", "config", "user.email", "skills@example.com")
	mustSh(t, remote, "git", "config", "user.name", "Skills")
	os.MkdirAll(filepath.Join(remote, "skills", "go"), 0o755)
	os.WriteFile(filepath.Join(remote, "skills", "go", "SKILL.md"), []byte("---\nname: go\n---\nwrap with %w\n"), 0o644)
	mustSh(t, remote, "git", "add", "-A")
	mustSh(t, remote, "git", "commit", "-qm", "the go skill")
	return remote, strings.TrimSpace(mustSh(t, remote, "git", "rev-parse", "HEAD"))
}

// TestTheGateIsHandedTheSkillsItWasGranted: the box grants its skills to
// every role, and the reviewer was the one role with no mirror to lay them
// out from — the first landing through it on the dogfood found the refusal
// and nothing behind it. A granted skill now reaches the wire the way a
// build's does: as the plugin session/new is told to load, at the skills
// directory beside the reading material. Asserted from what the agent was
// actually sent, which the harness scenario echoes back as its answer.
func TestTheGateIsHandedTheSkillsItWasGranted(t *testing.T) {
	ctx := context.Background()
	remote, commit := skillRepo(t)
	m, err := gitforge.NewMirror(t.TempDir(), btn, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := m.ResolveSkills(ctx, []agent.SkillRef{{Name: "go", Source: agent.Source{
		Kind: agent.SourceGit, Repo: remote, Commit: commit, Path: "skills/go"}}})
	if err != nil {
		t.Fatal(err)
	}
	rv, cs, s, _ := reviewerRig(t, "harness")
	rv.Mirror = m
	layers := gateLayers()
	layers[0].Skills = refs
	rv.Composer = &agent.Composer{Source: layers}
	// The harness scenario answers with session/new's _meta rather than a
	// judgement, so Evaluate cannot conclude; what was delivered is on the
	// chain regardless, and that is the fact under test.
	if _, err := rv.Evaluate(ctx, cs); err == nil {
		t.Fatal("the harness scenario gave no judgement and the gate concluded anyway")
	}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				Plugins []struct {
					Type string `json:"type"`
					Path string `json:"path"`
				} `json:"plugins"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	text := agentText(t, s, cs.Run)
	if err := json.Unmarshal([]byte(text), &meta); err != nil {
		t.Fatalf("session/new carried no readable _meta (%q): %v", text, err)
	}
	// Under this run's directory and beside the material, never inside the
	// tree under review: the skills directory is a sibling of the agent's
	// cwd, as forge.SkillsDir names it.
	under := filepath.Join(rv.Root, cs.Run) + string(filepath.Separator)
	var paths []string
	for _, p := range meta.ClaudeCode.Options.Plugins {
		paths = append(paths, p.Path)
		if p.Type == "local" && strings.HasPrefix(p.Path, under) && strings.HasSuffix(p.Path, ".skills") {
			return
		}
	}
	t.Fatalf("the granted skill reached the agent nowhere: plugins %v, want a .skills directory under %s", paths, under)
}

// TestTheGateWithNoMirrorRefusesGrantedSkills: the refusal the dogfood hit
// stays for a reviewer assembled without a mirror. Running without the
// skills would be the silent divergence the agentref exists to prevent, so
// nothing is judged and nothing is spent.
func TestTheGateWithNoMirrorRefusesGrantedSkills(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	layers := gateLayers()
	layers[0].Skills = []agent.SkillRef{{Name: "go", Source: agent.Source{
		Kind: agent.SourceGit, Repo: "https://example/skills.git", Commit: "0123456789abcdef", Path: "skills/go", Digest: "tree-1"}}}
	rv.Composer = &agent.Composer{Source: layers}
	_, err := rv.Evaluate(context.Background(), cs)
	if err == nil || !strings.Contains(err.Error(), "no mirror") {
		t.Fatalf("err = %v, want the missing-mirror refusal", err)
	}
	if got := chain(t, s, cs.Run)["agent-result"]; got != "" {
		t.Fatalf("an agent ran without the skills it was granted:\n%s", got)
	}
}

// TestTheGateIsToldItsOwnEarlierRefusal: issue #105. A unit refused once
// before gets an inline layer carrying that refusal and a prompt that names
// it, and an answer that resolves it while finding something new is a fail
// carrying both facts — the earlier finding fixed, the new one why.
func TestTheGateIsToldItsOwnEarlierRefusal(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-resolved")
	gateRecord(t, s, cs.Run, "sha-attempt-1", forge.Verdict{
		Gate: ReviewerName, Decision: forge.DecisionFail,
		Reason: "the change edits db/schema.sql, which the unit never declared",
	})
	ctx := context.Background()
	v, err := rv.Evaluate(ctx, cs)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Gate != ReviewerName || v.Decision != forge.DecisionFail {
		t.Fatalf("verdict: %+v", v)
	}
	if len(v.Resolutions) != 1 || v.Resolutions[0].Attempt != 1 || v.Resolutions[0].Gate != ReviewerName || !v.Resolutions[0].Resolved {
		t.Fatalf("the resolution did not reach the verdict: %+v", v.Resolutions)
	}
	kinds := chain(t, s, cs.Run)
	if !strings.Contains(kinds["agent-run"], "prior-refusals") {
		t.Fatalf("the bundle's layers did not carry the prior refusal:\n%s", kinds["agent-run"])
	}
	if !strings.Contains(kinds["agent-prompt"], "attempt 1, "+ReviewerName) {
		t.Fatalf("the prompt did not name the earlier finding:\n%s", kinds["agent-prompt"])
	}
}

// TestAnAnswerThatSkipsAnEarlierFindingIsTrouble: a unit with a prior whose
// answer never says whether the named finding was fixed has not answered —
// the reviewer was told and stayed silent on it, which is trouble, not a
// verdict either way.
func TestAnAnswerThatSkipsAnEarlierFindingIsTrouble(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-resolutions-missing")
	gateRecord(t, s, cs.Run, "sha-attempt-1", forge.Verdict{
		Gate: ReviewerName, Decision: forge.DecisionFail,
		Reason: "the change edits db/schema.sql, which the unit never declared",
	})
	v, err := rv.Evaluate(context.Background(), cs)
	if err == nil || !strings.Contains(err.Error(), "attempt 1, "+ReviewerName) {
		t.Fatalf("want an error naming the skipped finding, got %v", err)
	}
	if v.Gate != "" || v.Decision != "" || v.Reason != "" {
		t.Fatalf("a skipped finding was recorded as a verdict: %+v", v)
	}
}

// TestAnUnresolvedFindingCannotPass: marking an earlier finding unresolved
// while passing the change is self-contradictory, and is trouble rather than
// a silent pass.
func TestAnUnresolvedFindingCannotPass(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-unresolved-pass")
	gateRecord(t, s, cs.Run, "sha-attempt-1", forge.Verdict{
		Gate: ReviewerName, Decision: forge.DecisionFail,
		Reason: "the change edits db/schema.sql, which the unit never declared",
	})
	v, err := rv.Evaluate(context.Background(), cs)
	if err == nil || !strings.Contains(err.Error(), "self-contradictory") {
		t.Fatalf("want a self-contradiction error, got %v", err)
	}
	if v.Gate != "" || v.Decision != "" || v.Reason != "" {
		t.Fatalf("a self-contradictory answer was recorded as a verdict: %+v", v)
	}
}

// TestAUnitWithNoPriorGetsTheOldShape: no refusal on the chain means nothing
// changes — no prior layer, no resolutions demanded, the plain {"verdict",
// "reason"} answer parses exactly as it always has. TestTheGateReadsAChange-
// AndPasses already exercises the pass path on a fresh item; this asserts
// the absence directly.
func TestAUnitWithNoPriorGetsTheOldShape(t *testing.T) {
	rv, cs, s, _ := reviewerRig(t, "review-pass")
	v, err := rv.Evaluate(context.Background(), cs)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Decision != forge.DecisionPass || len(v.Resolutions) != 0 {
		t.Fatalf("verdict: %+v", v)
	}
	if strings.Contains(chain(t, s, cs.Run)["agent-run"], "prior-refusals") {
		t.Fatal("a unit with no prior refusal was handed a prior-refusals layer")
	}
}
