package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// retryPrior is the item's Prior after a gate refusal within the same claim:
// attempt 2 of a 3-attempt budget, carrying the refusal a continuation is
// sent instead of the whole composed prompt.
func retryPrior(attempt, of int) *agent.Prior {
	return &agent.Prior{
		Attempt: attempt, Of: of,
		Verdicts: []agent.PriorVerdict{
			{Gate: "review", Decision: "fail", Reason: "the widget defaults to green", Attempt: 1},
		},
	}
}

// retainPrior sets prior directly on the item's chain without going through
// [queue.Queue.Retain] — Retain renews a real lease and this test's workspace
// already holds the one lease it needs, so a second "queued" record (the
// same public API [queue.Queue.Submit] this package's other builder tests
// already use) is enough to change what [Builder.item] reads back. The
// loop-side unit that calls Retain at its own retry site is a separate,
// dependent unit; this only has to prove what the builder does once Prior
// says so.
func retainPrior(t *testing.T, ctx context.Context, s *sqlite.Store, item queue.WorkItem, prior *agent.Prior) queue.WorkItem {
	t.Helper()
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	loaded, err := q.Load(ctx, btn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Prior = prior
	updated, err := q.Submit(ctx, loaded)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

// runRecords collects one work item's agent-run and agent-session record
// bodies, oldest first, plus whether an agent-resume note appears.
func runRecords(t *testing.T, ctx context.Context, s *sqlite.Store, run string) (runs, sessions []string, resumed bool) {
	t.Helper()
	for r, err := range s.Log().Since(ctx, btn, "workitem/"+run, 1) {
		if err != nil {
			t.Fatal(err)
		}
		switch r.Kind {
		case "agent-run":
			runs = append(runs, string(r.Body))
		case "agent-session":
			sessions = append(sessions, string(r.Body))
		case "agent-resume":
			resumed = true
		}
	}
	return
}

// TestInClaimRetryContinuesThePriorSession: attempt 2 of a 3-attempt budget
// finds the session the first attempt made and continues it — session/load,
// not session/new — landing its own commit on top of the first attempt's,
// with the record stating explicitly which session it continued.
func TestInClaimRetryContinuesThePriorSession(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-cont-ok", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	first, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	retainPrior(t, ctx, s, item, retryPrior(2, 3))
	b.Env = []string{"ACP_FAKE=resume"}
	second, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("continuation attempt: %v", err)
	}
	if second == first {
		t.Fatal("the continuation did not land its own commit")
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "resumed.go")); err != nil {
		t.Fatalf("the continuation's own change is missing from the workspace: %v", err)
	}

	runs, sessions, resumed := runRecords(t, ctx, s, item.ID)
	if resumed {
		t.Fatal("a successful continuation must not write an agent-resume refusal")
	}
	if len(runs) != 2 || len(sessions) != 2 {
		t.Fatalf("want one agent-run and one agent-session record per attempt, got %d runs and %d sessions", len(runs), len(sessions))
	}
	if strings.Contains(runs[0], `"continues"`) {
		t.Errorf("the cold first attempt must not claim a continuation:\n%s", runs[0])
	}
	if !strings.Contains(runs[1], `"continues":"sess-1"`) {
		t.Errorf("the continuation's own run record does not say what it continued:\n%s", runs[1])
	}
	if !strings.Contains(sessions[1], `"session":"sess-1"`) || !strings.Contains(sessions[1], `"role":"builder"`) {
		t.Errorf("the continuation's session record lost its id or role:\n%s", sessions[1])
	}
}

// TestFramesCarryTheAttemptsOwnAgentref: a frame reaching Progress names the
// session (agentref) of the attempt that produced it, and two attempts on
// the same item — the cold first build and the continuation that follows a
// gate refusal — carry two different agentrefs, because each composes its
// own bundle rather than inheriting the run's first one.
func TestFramesCarryTheAttemptsOwnAgentref(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	var updates []Update
	b.Progress = func(_ context.Context, _ string, u Update) { updates = append(updates, u) }

	item := submitSectioned(t, ctx, s, router, "issue-cont-refs", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	first := updates
	updates = nil

	retainPrior(t, ctx, s, item, retryPrior(2, 3))
	b.Env = []string{"ACP_FAKE=resume"}
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatalf("continuation attempt: %v", err)
	}
	second := updates

	if len(first) == 0 || len(second) == 0 {
		t.Fatalf("want updates from both attempts, got %d and %d", len(first), len(second))
	}
	ref1 := first[0].Session
	if ref1 == "" {
		t.Fatal("the first attempt's frames carry no session")
	}
	for _, u := range first {
		if u.Session != ref1 {
			t.Errorf("first attempt update %+v carries session %q, want the one attempt's agentref %q", u, u.Session, ref1)
		}
	}
	ref2 := second[0].Session
	if ref2 == "" {
		t.Fatal("the continuation's frames carry no session")
	}
	if ref2 == ref1 {
		t.Fatal("the continuation composed its own bundle and must carry a different agentref than the cold attempt")
	}
	for _, u := range second {
		if u.Session != ref2 {
			t.Errorf("continuation update %+v carries session %q, want the continuation's own agentref %q", u, u.Session, ref2)
		}
	}
}

// TestInClaimRetryFallsBackColdOnFailedContinuation: a continuation that
// cannot actually continue — session/load refuses — costs a note on the
// chain, never the attempt: the build still succeeds, cold, and the note
// names the session and why it was refused.
func TestInClaimRetryFallsBackColdOnFailedContinuation(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-cont-fallback", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	retainPrior(t, ctx, s, item, retryPrior(2, 3))
	b.Env = []string{"ACP_FAKE=resume-load-fails"}
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("a failed continuation must still land the cold build: %v", err)
	}
	if head == "" {
		t.Fatal("no commit from the cold fallback")
	}

	runs, sessions, resumed := runRecords(t, ctx, s, item.ID)
	if !resumed {
		t.Fatal("a failed continuation must leave an agent-resume note on the chain")
	}
	// Three agent-run records: the first cold build, the failed
	// continuation attempt, and the cold fallback that followed it. Only two
	// agent-session records: session/load never got far enough to open a
	// session on the failed continuation, so it never wrote one — the
	// refusal is the agent-resume note instead.
	if len(runs) != 3 || len(sessions) != 2 {
		t.Fatalf("want 3 agent-run records (cold, failed continuation, cold fallback) and 2 agent-session records (the two that opened), got %d and %d", len(runs), len(sessions))
	}
	if !strings.Contains(runs[1], `"continues":"sess-1"`) {
		t.Errorf("the failed continuation's own run record must still say what it tried to continue:\n%s", runs[1])
	}
	if strings.Contains(runs[2], `"continues"`) {
		t.Errorf("the cold fallback must not claim a continuation of its own:\n%s", runs[2])
	}
	var note struct {
		Session string `json:"session"`
		Refused string `json:"refused"`
	}
	found := false
	for r, err := range s.Log().Since(ctx, btn, "workitem/"+item.ID, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-resume" {
			if err := json.Unmarshal(r.Body, &note); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found || note.Session != "sess-1" || !strings.Contains(note.Refused, "acp: session resume failed") {
		t.Fatalf("agent-resume note lost the session or the reason: found=%v %+v", found, note)
	}
}

// TestLastAttemptNeverContinues: an item at the last attempt under its budget
// (Attempt == Of) goes cold exactly as it always has — a prior session on
// the chain is never even looked up, let alone continued, because
// Prior.Verdicts already carries the whole history for a run this close to
// its budget.
func TestLastAttemptNeverContinues(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-cont-last", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	retainPrior(t, ctx, s, item, retryPrior(3, 3))
	// A scenario with no session/load handler at all: if the builder tried
	// to continue anyway, the fake agent would never answer it and the
	// second Build call would hang until this deadline — the test is written
	// so a regression here fails loudly rather than silently doing nothing.
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := b.Build(ctx2, ws, item.Task); err != nil {
		t.Fatalf("the last attempt under budget must still build, cold: %v", err)
	}
	if ctx2.Err() != nil {
		t.Fatal("the last attempt hung as if it tried to continue a session")
	}
	runs, _, resumed := runRecords(t, ctx, s, item.ID)
	if resumed {
		t.Fatal("the last attempt must never even try a continuation, so no agent-resume note either")
	}
	if len(runs) != 2 || strings.Contains(runs[1], `"continues"`) {
		t.Fatalf("the last attempt's own run record must not claim a continuation: %v", runs)
	}
}

// TestPriorBuilderSessionSkipsALaterReviewerRecord: the newest BUILDER
// session is what a continuation resumes, never a reviewer's — the two share
// the workitem/<id> scope, and a continuation that picked up the reviewer's
// session id would resume the wrong agent entirely.
func TestPriorBuilderSessionSkipsALaterReviewerRecord(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	const run = "wi-skip-reviewer"
	append_ := func(kind, body string) {
		if _, err := s.Log().Append(ctx, evidence.Record{
			Tenant: btn, Scope: "workitem/" + run, Kind: kind, Body: []byte(body),
		}); err != nil {
			t.Fatal(err)
		}
	}
	append_("agent-session", `{"session":"sess-builder-1","role":"builder"}`)
	append_("agent-session", `{"session":"sess-reviewer-1","role":"reviewer"}`)

	b := &Builder{Store: s, Tenant: btn}
	id, ok, err := b.priorBuilderSession(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || id != "sess-builder-1" {
		t.Fatalf("want the builder session sess-builder-1, got id=%q ok=%v", id, ok)
	}
}

// TestPriorBuilderSessionFindsNoneOnAFreshItem: an item with no builder
// session on its chain at all has nothing to continue — the ordinary case
// for every item that has never been built before.
func TestPriorBuilderSessionFindsNoneOnAFreshItem(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Builder{Store: s, Tenant: btn}
	if _, ok, err := b.priorBuilderSession(context.Background(), "wi-nothing-yet"); err != nil || ok {
		t.Fatalf("want ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}
