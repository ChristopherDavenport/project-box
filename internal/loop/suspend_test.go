package loop

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// (a) wall advances ten minutes, monotonic one tick: Observe reports the
// gap and the cumulative total carries it.
func TestObserveReportsAWallJumpUnmatchedByMonotonicAndAccumulates(t *testing.T) {
	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mono := time.Unix(0, 0)
	w := &SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	if gap := w.Observe(time.Second); gap != 0 {
		t.Fatalf("first observe (baseline): gap = %v, want 0", gap)
	}

	wall = wall.Add(10 * time.Minute)
	mono = mono.Add(time.Second) // one tick

	want := 10*time.Minute - time.Second
	if gap := w.Observe(time.Second); gap != want {
		t.Fatalf("gap = %v, want %v", gap, want)
	}
	if total := w.Total(); total != want {
		t.Fatalf("total = %v, want %v", total, want)
	}
}

// (d) the same jump with nothing subscribed changes nothing else: the total
// still accumulates and Observe neither panics nor requires a subscriber.
func TestObserveWithNoSubscribersOnlyAccumulatesTheTotal(t *testing.T) {
	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mono := time.Unix(0, 0)
	w := &SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	w.Observe(time.Second)

	wall = wall.Add(10 * time.Minute)
	mono = mono.Add(time.Second)

	gap := w.Observe(time.Second)
	if gap <= 0 {
		t.Fatalf("expected a detected gap, got %v", gap)
	}
	if total := w.Total(); total != gap {
		t.Fatalf("total = %v, want %v", total, gap)
	}
}

// Subscribe's detach must actually stop notifications, not merely allow the
// caller to stop caring about them.
func TestSubscribeDetachStopsNotifications(t *testing.T) {
	wall := time.Now()
	mono := time.Now()
	w := &SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	w.Observe(time.Second)

	var got int
	detach := w.Subscribe(func(time.Duration) { got++ })

	wall = wall.Add(time.Minute)
	mono = mono.Add(time.Second)
	w.Observe(time.Second)
	if got != 1 {
		t.Fatalf("got %d notifications, want 1", got)
	}

	detach()
	wall = wall.Add(time.Minute)
	mono = mono.Add(time.Second)
	w.Observe(time.Second)
	if got != 1 {
		t.Fatalf("got %d notifications after detach, want still 1", got)
	}
}

// Observe, Subscribe and detach must be race-free under -race: a run in
// flight subscribes and detaches while the daemon's tick goroutine calls
// Observe concurrently.
func TestObserveSubscribeDetachAreRaceFree(t *testing.T) {
	var wallMu sync.Mutex
	wall := time.Now()
	mono := time.Now() // never advances: every Observe finds a gap
	w := &SuspendWatch{
		Wall: func() time.Time {
			wallMu.Lock()
			defer wallMu.Unlock()
			return wall
		},
		Mono: func() time.Time { return mono },
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detach := w.Subscribe(func(time.Duration) {})
			defer detach()
			for j := 0; j < 20; j++ {
				wallMu.Lock()
				wall = wall.Add(time.Second)
				wallMu.Unlock()
				w.Observe(0)
			}
		}()
	}
	wg.Wait()
}

// (b) a build whose deadline was five minutes (here, scaled down to a short
// real duration) away is still running after a suspend gap larger than the
// remaining budget arrives before the timer would otherwise fire — the
// bound extends instead of cancelling — and only cancels once nothing new
// is left to grant.
func TestSuspendAwareDeadlineExtendsByAnUngrantedGap(t *testing.T) {
	const budget = 150 * time.Millisecond
	const gap = time.Second

	l := &Loop{Suspend: &SuspendWatch{}, BuildTimeout: budget}
	bctx, cancel := l.buildDeadline(context.Background())
	defer cancel()

	wall, mono := time.Now(), time.Now()
	l.Suspend.Wall = func() time.Time { return wall }
	l.Suspend.Mono = func() time.Time { return mono }
	l.Suspend.Observe(time.Millisecond) // baseline for the watcher's own clocks

	// A suspend of `gap`, observed well inside the original budget: this is
	// what a resumed daemon's next tick would report.
	wall = wall.Add(gap)
	mono = mono.Add(time.Millisecond)
	l.Suspend.Observe(time.Millisecond)

	// Past the ORIGINAL budget, but well before budget+gap: a plain
	// context.WithTimeout would already have cancelled here.
	select {
	case <-bctx.Done():
		t.Fatal("build context cancelled past its original budget, before the suspend gap's extension should have taken effect")
	case <-time.After(budget + 100*time.Millisecond):
	}

	// Once budget+gap has genuinely elapsed with nothing new to grant, the
	// bound does cancel.
	select {
	case <-bctx.Done():
	case <-time.After(budget + gap + 500*time.Millisecond):
		t.Fatal("build context never cancelled once its extended budget ran out")
	}
}

// (e) a Loop with a nil watcher behaves byte-for-byte as today: a plain
// context.WithTimeout deadline, and a renewOnResume tracker nobody ever
// notifies.
func TestNilSuspendLeavesBuildDeadlinePlain(t *testing.T) {
	l := &Loop{}
	bctx, cancel := l.buildDeadline(context.Background())
	defer cancel()
	dl, ok := bctx.Deadline()
	if !ok {
		t.Fatal("expected buildDeadline to set a deadline")
	}
	want := time.Now().Add(l.buildTimeout())
	if diff := dl.Sub(want); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("deadline %v not near expected %v (diff %v)", dl, want, diff)
	}
}

func TestNilSuspendRenewOnResumeIsANoop(t *testing.T) {
	l := &Loop{}
	held, detach := l.renewOnResume(context.Background())
	defer detach()
	// Nothing subscribes when l.Suspend is nil, so setting a lease and
	// detaching must not panic or do anything observable.
	held.set("claim", forge.Lease{})
}

// (c) a lease that would have expired across the jump is renewed, by the
// resume notification alone, and still held by the same token.
func TestResumeNotificationRenewsHeldLeasesWithTheSameToken(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	const ttl = 40 * time.Millisecond
	l := &Loop{Store: s, Runner: "runner-1", TTL: ttl, Suspend: &SuspendWatch{}}
	leaser := s.Leaser("runner-1")
	ctx := context.Background()

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: "app", Ref: "run/x", Run: "x", TTL: ttl,
	})
	if err != nil {
		t.Fatal(err)
	}

	held, detach := l.renewOnResume(ctx)
	defer detach()
	held.set("workspace", lease)

	wall, mono := time.Now(), time.Now()
	l.Suspend.Wall = func() time.Time { return wall }
	l.Suspend.Mono = func() time.Time { return mono }
	l.Suspend.Observe(time.Millisecond) // baseline

	// The suspend outlasts the lease's TTL: without the resume-driven
	// renewal, this lease would already be gone by the time the machine
	// wakes up.
	wall = wall.Add(10 * ttl)
	mono = mono.Add(time.Millisecond)
	l.Suspend.Observe(time.Millisecond) // detects the gap, renews synchronously

	got, ok, err := leaser.Inspect(ctx, tn, "app", "run/x")
	if err != nil || !ok {
		t.Fatalf("inspect after resume: ok=%v err=%v", ok, err)
	}
	// The fake wall/mono clocks used to trigger Observe never move real
	// time, so an unrenewed lease's Expires would still read as "after
	// time.Now()" here too — that comparison alone cannot tell a renewed
	// lease from an untouched one. Comparing against the lease's own
	// original Expires can: a real renewal moves it forward from a
	// call to Renew made after this point in the test, an untouched
	// lease leaves it exactly where Acquire set it.
	if !got.Expires.After(lease.Expires) {
		t.Fatalf("lease was not renewed across the resume: got.Expires=%v, original lease.Expires=%v", got.Expires, lease.Expires)
	}
	if got.Token != lease.Token {
		t.Fatalf("token changed across renew: got %v, want %v", got.Token, lease.Token)
	}
	if !got.Expires.After(time.Now()) {
		t.Fatalf("lease expired despite the resume renewal: expires %v, now %v", got.Expires, time.Now())
	}
}
