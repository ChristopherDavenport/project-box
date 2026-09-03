package capacity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tnr = tenant.ID("acme")

// meters returns two meters over ONE store: the first spends, the second is
// the restart. Sharing the store is the whole point — the account has to live
// somewhere a new process can find it.
func meters(t *testing.T, b Budget, now func() time.Time) (*Meter, func() *Meter) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "box.db")
	open := func() *Meter {
		// The store stamps the records the restore reads, so it must be on
		// the same clock as the meter or "this window" means two things.
		s, err := sqlite.Open(path, sqlite.WithClock(now))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return &Meter{Store: s, Tenant: tnr, Actor: "boxd", Budget: b, Now: now}
	}
	return open(), open
}

// TestTheBrakeSurvivesARestart. The meter wrote every charge to
// control/capacity and read none of them back, so the window lived in memory
// alone and a restart cleared it. Measured live: refusing at 25,419,130
// tokens against a 6,000,000 ceiling, boxd restarted, next status
// tokens=0 refusing=false. A crash loop was an unbounded budget.
func TestTheBrakeSurvivesARestart(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	release, err := m.Acquire(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	m.Charge(ctx, "run-1", Spend{Total: 20_076_747, Reported: true})
	release()

	if _, err := m.Acquire(ctx, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("the meter did not brake before the restart: %v", err)
	}

	// The restart.
	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m2.Report().Tokens; got != 20_076_747 {
		t.Fatalf("restored spend = %d, want the window's actual 20,076,747", got)
	}
	if _, err := m2.Acquire(ctx, "run-3"); !errors.Is(err, ErrBudget) {
		t.Fatal("a restart cleared the brake: the factory would spend the window again")
	}
}

// TestRestoreForgetsTheWindowBefore. The account is the CURRENT window's, not
// all of history — otherwise a factory that ever spent would never run again.
func TestRestoreForgetsTheWindowBefore(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	release, _ := m.Acquire(ctx, "run-1")
	m.Charge(ctx, "run-1", Spend{Total: 20_076_747, Reported: true})
	release()

	now = now.Add(time.Hour) // into the next aligned bucket
	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m2.Report().Tokens; got != 0 {
		t.Fatalf("restored spend = %d across a window boundary, want 0", got)
	}
	if _, err := m2.Acquire(ctx, "run-2"); err != nil {
		t.Fatalf("the new window would not start: %v", err)
	}
}

// TestUnmeteredSurvivesARestartToo. An unmetered run stops claims until the
// window turns; forgetting that on restart would be the same hole with a
// worse failure mode, since the window's total is not merely stale but known
// to be wrong.
func TestUnmeteredSurvivesARestart(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	release, _ := m.Acquire(ctx, "run-1")
	m.Charge(ctx, "run-1", Spend{}) // said nothing about its cost
	release()

	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Acquire(ctx, "run-2"); !errors.Is(err, ErrUnmetered) {
		t.Fatalf("a restart forgot an unmetered run: err = %v", err)
	}
}

// TestReloadCannotClearTheAccount. SetBudget used to restart the window,
// which made the brake resettable by editing a file and sending SIGHUP.
func TestReloadCannotClearTheAccount(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, _ := meters(t, budget, clock)
	ctx := context.Background()

	release, _ := m.Acquire(ctx, "run-1")
	m.Charge(ctx, "run-1", Spend{Total: 20_076_747, Reported: true})
	release()

	m.SetBudget(budget) // the reload
	if got := m.Report().Tokens; got != 20_076_747 {
		t.Fatalf("a config reload cleared the window's spend (%d): the brake is resettable by hand", got)
	}
	if _, err := m.Acquire(ctx, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatal("a config reload lifted the brake")
	}
}

// TestRefusingMeansWouldRefuseNow. The room's "refusing" flag reported
// m.refused, which answers a different question: whether a refusal record has
// already been written this window. The two came apart the moment the ceiling
// was raised — the budget was lifted, a run started, and the status endpoint
// went on saying refusing=true with work visibly in flight.
func TestRefusingMeansWouldRefuseNow(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, _ := meters(t, budget, clock)
	ctx := context.Background()

	release, _ := m.Acquire(ctx, "run-1")
	m.Charge(ctx, "run-1", Spend{Total: 20_076_747, Reported: true})
	release()
	if _, err := m.Acquire(ctx, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatal("setup: the meter should be braking")
	}
	if !m.Report().Refusing {
		t.Fatal("over budget and not reporting it")
	}

	// The operator raises the ceiling above what has been spent.
	budget.MaxTokensPerWindow = 30_000_000
	m.SetBudget(budget)

	if m.Report().Refusing {
		t.Fatal("still reporting refusing after the ceiling was raised above the spend")
	}
	if _, err := m.Acquire(ctx, "run-3"); err != nil {
		t.Fatalf("the raised ceiling did not admit work: %v", err)
	}
}

// TestReportRollsTheWindowForward. Reading is not side-effect free: the
// counters belong to whichever window the clock is in, so a report taken
// after the hour turns must show the new window rather than the old total.
func TestReportRollsTheWindowForward(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	m, _ := meters(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}, clock)
	ctx := context.Background()

	release, _ := m.Acquire(ctx, "run-1")
	m.Charge(ctx, "run-1", Spend{Total: 20_076_747, Reported: true})
	release()
	if !m.Report().Refusing {
		t.Fatal("setup: should be braking")
	}

	now = now.Add(time.Hour) // the hour turns; nobody has claimed since
	r := m.Report()
	if r.Tokens != 0 || r.Refusing {
		t.Fatalf("a stale window was reported after the turn: %+v", r)
	}
}
