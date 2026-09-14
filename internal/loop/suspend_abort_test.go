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

// suspendAbortingBuilder returns &Aborted for its first failFor calls, each
// time first advancing the fake watcher's clocks and asking it to Observe —
// standing in for a daemon tick that lands mid-build and detects a suspend —
// then builds for real.
type suspendAbortingBuilder struct {
	scriptedBuilder
	watch   *SuspendWatch
	advance func()
	n       int
	failFor int
	reason  string
}

func (b *suspendAbortingBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.n++
	if b.n <= b.failFor {
		if b.advance != nil {
			b.advance()
			b.watch.Observe(time.Millisecond)
		}
		return "", &Aborted{Reason: b.reason}
	}
	return b.scriptedBuilder.Build(ctx, ws, task)
}

// TestSuspendCausedAbortDoesNotCountAgainstAbortedTries: an abort whose
// window saw a suspend gap (the watcher's cumulative Total moved between
// this attempt's baseline and its classification) is the suspend's doing,
// not the transport's on its own — AbortedTries must not move, the item
// waits with a note naming the suspend and its gap, and it is marked to
// resume the session next attempt. A clean build that follows consumes the
// marker.
func TestSuspendCausedAbortDoesNotCountAgainstAbortedTries(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()

	wall, mono := time.Now(), time.Now()
	watch := &SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	watch.Observe(time.Millisecond) // baseline reading, reports nothing yet
	e.loop.Suspend = watch

	builder := &suspendAbortingBuilder{
		watch:   watch,
		failFor: 1,
		reason:  "Server error mid-response",
		advance: func() {
			wall = wall.Add(10 * time.Minute)
			mono = mono.Add(time.Millisecond)
		},
	}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-suspend-1"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-suspend-1", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a suspend-caused abort should re-queue as waiting, got %s (%s)", item.State, item.Note)
	}
	if item.AbortedTries != 0 {
		t.Fatalf("AbortedTries = %d, want 0 — a suspend must never spend a transport retry", item.AbortedTries)
	}
	if !item.SuspendResume {
		t.Fatal("item was not marked to resume the session on the next attempt")
	}
	if !strings.Contains(item.Note, "suspend") || !strings.Contains(item.Note, "Server error mid-response") {
		t.Fatalf("waiting note should name the suspend, its gap, and the transport reason: %q", item.Note)
	}

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a build that stops aborting should proceed to land, got %s (%s)", item.State, item.Note)
	}
	if item.AbortedTries != 0 {
		t.Fatalf("AbortedTries = %d, want still 0", item.AbortedTries)
	}
	if item.SuspendResume {
		t.Fatal("the resume marker must be consumed once the next attempt has run")
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2", builder.n)
	}
}

// TestAbortWithoutSuspendGapCountsAsTodayEvenWithAWatcher: a watcher being
// configured at all must not change behavior for an ordinary transport
// abort that saw no suspend in its own window — AbortedTries still
// increments exactly as it does with no watcher configured.
func TestAbortWithoutSuspendGapCountsAsTodayEvenWithAWatcher(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Suspend = &SuspendWatch{}
	builder := &abortingBuilder{failFor: 1, reason: "Server error mid-response"}
	e.loop.Builder = builder

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-suspend-2"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-suspend-2", "do a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a first aborted build should re-queue as waiting, got %s (%s)", item.State, item.Note)
	}
	if item.AbortedTries != 1 {
		t.Fatalf("AbortedTries = %d, want 1 — a watcher with no observed gap changes nothing", item.AbortedTries)
	}
	if item.SuspendResume {
		t.Fatal("an ordinary abort must not mark the item to resume a session")
	}
}
