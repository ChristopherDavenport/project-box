package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// markSuspendResume sets item.SuspendResume directly on the item's chain,
// the way the loop's Aborted classification persists it via [queue.Queue.Wait]
// (internal/loop/loop.go) — without going through Wait itself, since these
// tests already hold the one lease [checkoutWorkspace] took and only need to
// change what [Builder.item] reads back, the same shortcut retainPrior takes
// for item.Prior.
func markSuspendResume(t *testing.T, ctx context.Context, s *sqlite.Store, item queue.WorkItem) queue.WorkItem {
	t.Helper()
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	loaded, err := q.Load(ctx, btn, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.SuspendResume = true
	updated, err := q.Submit(ctx, loaded)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

// TestAbortedRunStillWritesItsSessionRecord: an aborted run's session id
// must land on the chain even though the run errored, or nothing gives a
// later resumed attempt a session to find at all — priorBuilderSession
// (builder.go) can only pick up what an agent-session record leaves behind.
// builder.go's runOnce writes it unconditionally on res.Session.Opened,
// before it even looks at the run's error, and the transport dropping
// session/prompt after session/new already answered must not lose that.
func TestAbortedRunStillWritesItsSessionRecord(t *testing.T) {
	s, lp, router, _, _ := rig(t, "aborted")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-suspend-session", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	_, err := b.Build(ctx, ws, item.Task)
	var ab *loop.Aborted
	if !errors.As(err, &ab) {
		t.Fatalf("want *loop.Aborted, got %v", err)
	}

	id, ok, err := b.priorBuilderSession(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || id != "sess-1" {
		t.Fatalf("an aborted run left no builder session to resume: id=%q ok=%v", id, ok)
	}
}

// TestSuspendMarkedRetryResumesTheAbortedSession: a fresh claim whose only
// prior attempt aborted (item.SuspendResume, set the way the loop's Aborted
// classification sets it on a suspend) resumes that same session —
// session/load, not session/new — rather than paying for a cold attempt,
// even with no [agent.Prior] on the item at all (a fresh claim carries no
// in-claim retry budget to continue within, so ordinary continuation would
// never have fired here). The chain names the suspend as the reason even
// though this resume succeeded.
func TestSuspendMarkedRetryResumesTheAbortedSession(t *testing.T) {
	s, lp, router, _, _ := rig(t, "aborted")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-suspend-resume", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err == nil {
		t.Fatal("first attempt: want an abort, got none")
	}

	item = markSuspendResume(t, ctx, s, item)
	b.Env = []string{"ACP_FAKE=resume"}
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("resumed attempt: %v", err)
	}
	if head == "" {
		t.Fatal("no commit from the resumed attempt")
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "resumed.go")); err != nil {
		t.Fatalf("the resumed session's own change is missing from the workspace: %v", err)
	}

	runs, sessions, resumed := runRecords(t, ctx, s, item.ID)
	if len(runs) != 2 || len(sessions) != 2 {
		t.Fatalf("want one agent-run and one agent-session record per attempt (the aborted one and the resume), got %d runs and %d sessions", len(runs), len(sessions))
	}
	if !strings.Contains(runs[1], `"continues":"sess-1"`) {
		t.Errorf("the resumed attempt's own run record does not say what it continued:\n%s", runs[1])
	}
	if !resumed {
		t.Fatal("a suspend-driven resume must leave an agent-resume note on the chain even when it succeeded")
	}
	var note struct {
		Session string `json:"session"`
		Reason  string `json:"reason"`
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
	if !found {
		t.Fatal("no agent-resume record")
	}
	if note.Session != "sess-1" || note.Reason != "suspend" || note.Refused != "" {
		t.Fatalf("agent-resume note did not name the suspend as the reason for a successful resume: %+v", note)
	}
}

// TestSuspendMarkedRetryFallsBackColdOnFailedResume: session/load refusing
// still degrades to a cold build exactly as the ordinary in-claim
// continuation's fallback does — the suspend marker changes what is tried,
// never how a failed attempt is handled — and the agent-resume note left
// behind still names the suspend as why a resume was tried at all.
func TestSuspendMarkedRetryFallsBackColdOnFailedResume(t *testing.T) {
	s, lp, router, _, _ := rig(t, "aborted")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-suspend-fallback", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err == nil {
		t.Fatal("first attempt: want an abort, got none")
	}

	item = markSuspendResume(t, ctx, s, item)
	b.Env = []string{"ACP_FAKE=resume-load-fails"}
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("a failed resume must still land the cold build: %v", err)
	}
	if head == "" {
		t.Fatal("no commit from the cold fallback")
	}

	var note struct {
		Session string `json:"session"`
		Reason  string `json:"reason"`
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
	if !found || note.Session != "sess-1" || note.Reason != "suspend" || !strings.Contains(note.Refused, "acp: session resume failed") {
		t.Fatalf("agent-resume note lost the session, the suspend reason, or the refusal: found=%v %+v", found, note)
	}
}

// TestResumeRefusedFallsBackColdWithoutSpecialCasing: an agent that does not
// advertise loadSession must still degrade to a cold build on a
// suspend-marked retry, using the same ErrResume fallback every other
// resume path already relies on — nothing about the suspend marker should
// special-case a refused capability.
func TestResumeRefusedFallsBackColdWithoutSpecialCasing(t *testing.T) {
	s, lp, router, _, _ := rig(t, "aborted")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-suspend-norefuse", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	if _, err := b.Build(ctx, ws, item.Task); err == nil {
		t.Fatal("first attempt: want an abort, got none")
	}

	item = markSuspendResume(t, ctx, s, item)
	// "worker" does not advertise loadSession (absent from acp_test.go's
	// resumeScenarios), so the wire itself refuses the resume before a
	// session/load is even sent.
	b.Env = []string{"ACP_FAKE=worker"}
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("a refused resume capability must still fall back cold: %v", err)
	}
	if head == "" {
		t.Fatal("no commit from the cold fallback")
	}
}
