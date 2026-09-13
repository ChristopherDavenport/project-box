package acp

import (
	"context"
	"strings"
	"testing"
)

// TestADirtyTurnIsNudgedIntoItsOwnCommit is issue #130's agent half: a
// builder that ends its turn with an uncommitted change, no escalation, and
// no handoff gets one more prompt on the same session before the adapter's
// placeholder ever runs. When that nudge actually commits, HEAD carries the
// agent's own message — the placeholder trailer never lands at all.
func TestADirtyTurnIsNudgedIntoItsOwnCommit(t *testing.T) {
	s, lp, router, _, _ := rig(t, "dirty-resume")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-nudge-commits", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if head == ws.Base {
		t.Fatal("the nudge's commit did not move the workspace")
	}

	msg := mustSh(t, ws.Dir, "git", "log", "-1", "--format=%B")
	if !strings.Contains(msg, "add the widget, since you asked") {
		t.Fatalf("HEAD's message is not the agent's own: %q", msg)
	}
	if strings.Contains(msg, doneTrailer) {
		t.Fatalf("the placeholder trailer landed even though the nudge committed: %q", msg)
	}

	runs, sessions, _ := runRecords(t, ctx, s, item.ID)
	if len(runs) != 2 || len(sessions) != 2 {
		t.Fatalf("want one agent-run and one agent-session record for the attempt and one each for the nudge, got %d runs and %d sessions", len(runs), len(sessions))
	}
	if strings.Contains(runs[0], `"continues"`) {
		t.Errorf("the cold attempt must not claim a continuation:\n%s", runs[0])
	}
	if !strings.Contains(runs[1], `"continues":"sess-1"`) {
		t.Errorf("the nudge's own run record does not say which session it continued:\n%s", runs[1])
	}
}

// TestANudgeThatAnswersWithoutCommittingStillFallsBack: the nudge is one
// chance, not a guarantee — when the resumed session answers but leaves the
// tree dirty, the adapter's placeholder commit still lands so the change is
// never lost.
func TestANudgeThatAnswersWithoutCommittingStillFallsBack(t *testing.T) {
	s, lp, router, _, _ := rig(t, "dirty-resume-nocommit")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-nudge-nocommit", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if head == ws.Base {
		t.Fatal("the placeholder fallback did not move the workspace")
	}

	msg := mustSh(t, ws.Dir, "git", "log", "-1", "--format=%B")
	if !strings.Contains(msg, doneTrailer) {
		t.Fatalf("the placeholder commit did not land after the nudge answered without committing: %q", msg)
	}
	content := mustSh(t, ws.Dir, "cat", "widget.go")
	if !strings.Contains(content, "built but never committed") {
		t.Fatalf("the agent's change was lost: %q", content)
	}
}

// TestANonResumableAgentStillFallsBackWithoutError: an agent that never
// advertises loadSession cannot be nudged at all — the resume is refused
// before anything is sent — and that refusal must not surface as a build
// failure. The fallback commit lands exactly as it would have before this
// unit existed.
func TestANonResumableAgentStillFallsBackWithoutError(t *testing.T) {
	s, lp, router, _, _ := rig(t, "dirty")
	ctx := context.Background()
	b := lp.Builder.(*Builder)

	item := submitSectioned(t, ctx, s, router, "issue-nudge-noresume", "add the widget package", nil)
	ws := checkoutWorkspace(t, ctx, s, lp, item.ID)
	head, err := b.Build(ctx, ws, item.Task)
	if err != nil {
		t.Fatalf("a refused nudge must still let the build fall back: %v", err)
	}
	if head == ws.Base {
		t.Fatal("the placeholder fallback did not move the workspace")
	}

	msg := mustSh(t, ws.Dir, "git", "log", "-1", "--format=%B")
	if !strings.Contains(msg, doneTrailer) {
		t.Fatalf("the placeholder commit did not land: %q", msg)
	}
}
