package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// abortingBuilder returns &Aborted for its first failFor calls — the
// transport dropping the call the way acp.ErrAborted reports it — then
// builds for real.
type abortingBuilder struct {
	scriptedBuilder
	n       int
	failFor int
	reason  string
}

func (b *abortingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.n++
	if b.n <= b.failFor {
		return "", &Aborted{Reason: b.reason}
	}
	return b.scriptedBuilder.Build(ctx, ws, task)
}

// TestAbortedBuildRequeuesUnderTheBound: a build that aborted once goes back
// to waiting with the counter at 1 and no park (issue #69) — nobody decided
// anything, so it is neither a park nor a silent retry inside the same
// claim, and a build that stops aborting proceeds to land with the counter
// reset.
func TestAbortedBuildRequeuesUnderTheBound(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	builder := &abortingBuilder{failFor: 1, reason: "Server error mid-response"}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-abort-1"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-abort-1", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a first aborted build should re-queue as waiting, got %s (%s)", item.State, item.Note)
	}
	if item.AbortedTries != 1 {
		t.Fatalf("AbortedTries = %d, want 1", item.AbortedTries)
	}
	if !strings.Contains(item.Note, "aborted") || !strings.Contains(item.Note, "Server error mid-response") {
		t.Fatalf("waiting note should name the abort and its reason: %q", item.Note)
	}

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a build that stops aborting should proceed to land, got %s (%s)", item.State, item.Note)
	}
	if item.AbortedTries != 0 {
		t.Fatalf("a clean build should reset the counter, got %d", item.AbortedTries)
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2", builder.n)
	}
}

// TestAbortedBuildParksAtTheBound: a build that keeps aborting past
// MaxAbortedRetries parks — a park still happens, never a silent loop — and
// the note carries how many times it aborted and the last reason.
func TestAbortedBuildParksAtTheBound(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	builder := &abortingBuilder{failFor: 1000, reason: "io.ErrUnexpectedEOF"}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-abort-2"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-abort-2", "do a thing"))

	for i := 0; i <= MaxAbortedRetries; i++ {
		item, err = e.loop.Run(ctx, item)
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	}
	if item.State != queue.StateParked {
		t.Fatalf("aborting past the bound should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "aborted") || !strings.Contains(item.Note, "io.ErrUnexpectedEOF") {
		t.Fatalf("park note should name the abort and its last reason: %q", item.Note)
	}
	if want := MaxAbortedRetries + 1; builder.n != want {
		t.Fatalf("builder ran %d times, want %d (one ask plus %d retries)", builder.n, want, MaxAbortedRetries)
	}
}
