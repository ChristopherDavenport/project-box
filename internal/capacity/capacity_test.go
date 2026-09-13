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

const tn = tenant.ID("acme")

func meter(t *testing.T, b Budget) (*Meter, *time.Time) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	m := &Meter{Store: s, Tenant: tn, Actor: "runner-1", Now: func() time.Time { return now }}
	m.SetBudget(b)
	return m, &now
}

// take runs one full acquire/release cycle, reporting the acquire's error.
func take(ctx context.Context, m *Meter, what string) error {
	release, err := m.Acquire(ctx, what)
	if err != nil {
		return err
	}
	release()
	return nil
}

// TestTokenBudgetStopsTheWindow: the volume brake. Counting run STARTS cannot
// tell a cheap factory from an expensive one — a start is one number whether
// the run behind it reads three files or the whole repository — so the run
// count stays unlimited here and the tokens do all the stopping.
//
// Volume and not the invoice, which this comment used to claim it was: see
// TestACostCeilingBrakesWhatATokenCeilingCannot for the currency that bills.
func TestTokenBudgetStopsTheWindow(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 10_000, Window: time.Hour})

	if err := take(ctx, m, "wi-1"); err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	m.Charge(ctx, "wi-1", Spend{Total: 4_000, Reported: true})
	if err := take(ctx, m, "wi-2"); err != nil {
		t.Fatalf("under budget but refused: %v", err)
	}
	m.Charge(ctx, "wi-2", Spend{Total: 7_000, Reported: true})

	// 11k spent against a 10k window.
	if err := take(ctx, m, "wi-3"); !errors.Is(err, ErrBudget) {
		t.Fatalf("over budget should refuse, got %v", err)
	}
	if r := m.Report(); r.Tokens != 11_000 || r.Unmetered != 0 {
		t.Fatalf("spend accounting: %d tokens, %d unmetered", r.Tokens, r.Unmetered)
	}
}

// TestUnmeteredRunClosesTheWindow is the fail-closed rule. An operator who set
// a token budget asked to be protected from spend; a run that will not say
// what it cost leaves the window's total unknowable, and a meter that reads
// unknown as zero is how a budget stays green while the money leaves.
func TestUnmeteredRunClosesTheWindow(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 1_000_000, Window: time.Hour})

	if err := take(ctx, m, "wi-1"); err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	m.Charge(ctx, "wi-1", Spend{}) // the agent said nothing

	err := take(ctx, m, "wi-2")
	if !errors.Is(err, ErrUnmetered) {
		t.Fatalf("want ErrUnmetered, got %v", err)
	}
	// Nowhere near the token ceiling: the refusal is about not knowing, and
	// it must report that rather than a total it cannot trust.
	if errors.Is(err, ErrBudget) {
		t.Fatal("an unmetered window reported itself as over budget")
	}
	if r := m.Report(); r.Unmetered != 1 {
		t.Fatalf("unmetered runs not counted: %d", r.Unmetered)
	}
}

// TestAbortedUnreportedRunDoesNotCloseTheWindow: a build the transport
// dropped before anything came back (issue #69) spent a KNOWN nothing — the
// harness never had a number to price — which is the opposite failure from
// TestUnmeteredRunClosesTheWindow's silent success. It must not raise
// Unmetered, and the window keeps admitting.
func TestAbortedUnreportedRunDoesNotCloseTheWindow(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 1_000_000, Window: time.Hour})

	if err := take(ctx, m, "wi-1"); err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	m.Charge(ctx, "wi-1", Spend{Aborted: true, Reported: false})

	if err := take(ctx, m, "wi-2"); err != nil {
		t.Fatalf("an aborted-unreported charge should still admit the next run: %v", err)
	}
	if r := m.Report(); r.Unmetered != 0 {
		t.Fatalf("a known zero must not raise unmetered: %d", r.Unmetered)
	}
}

// TestUnmeteredStopsEvenWithoutATokenBudget: measurement is the precondition
// for a budget, not a feature of one. Without a token budget there is no
// ceiling to enforce — and equally no way to notice a run costing anything at
// all, which is the worse of the two states to be in quietly.
func TestUnmeteredStopsEvenWithoutATokenBudget(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour})

	if err := take(ctx, m, "wi-1"); err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	m.Charge(ctx, "wi-1", Spend{})
	if err := take(ctx, m, "wi-2"); !errors.Is(err, ErrUnmetered) {
		t.Fatalf("an unmeasurable factory kept running: %v", err)
	}
}

// TestAllowUnmeteredIsAnExplicitChoice: an operator may fly blind, and the
// saying so is on the record. What must not happen is arriving there by
// omission.
func TestAllowUnmeteredIsAnExplicitChoice(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, AllowUnmetered: true, Window: time.Hour})

	for i := range 3 {
		if err := take(ctx, m, "wi"); err != nil {
			t.Fatalf("run %d refused after opting in: %v", i, err)
		}
		m.Charge(ctx, "wi", Spend{})
	}
	if r := m.Report(); r.Unmetered != 3 {
		t.Fatalf("opting in should still COUNT what it could not measure: %d", r.Unmetered)
	}
}

// TestWindowRolloverForgives: budgets are per window, and both the spend and
// the not-knowing expire with it.
func TestWindowRolloverForgives(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 5_000, Window: time.Hour})

	take(ctx, m, "wi-1")
	m.Charge(ctx, "wi-1", Spend{Total: 9_000, Reported: true})
	m.Charge(ctx, "wi-1", Spend{})
	if err := take(ctx, m, "wi-2"); err == nil {
		t.Fatal("an exhausted, unmetered window admitted a run")
	}

	*now = now.Add(time.Hour + time.Minute)
	if err := take(ctx, m, "wi-3"); err != nil {
		t.Fatalf("the new window should admit work: %v", err)
	}
	if r := m.Report(); r.Tokens != 0 || r.Unmetered != 0 {
		t.Fatalf("rollover kept the old window's account: %d tokens, %d unmetered", r.Tokens, r.Unmetered)
	}
}

// TestAcquireAloneIsNotAStart: a claim is not a run. Acquire took the slot —
// Report().RunningBuilds says so — but nothing has begun building, and
// nothing should say otherwise: not the started count, not the evidence
// chain.
func TestAcquireAloneIsNotAStart(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2})

	release, err := m.Acquire(ctx, "wi-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	if r := m.Report(); r.RunningBuilds != 1 || r.Started != 0 {
		t.Fatalf("acquire alone should hold the slot without starting it: %+v", r)
	}
	for rec, err := range m.Store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "spend" {
			t.Fatalf("acquire wrote a spend record before the run ever began: %+v", rec)
		}
	}
}

// TestStartRecordsTheBeginning: the moment a run actually begins is the
// moment charged, and it is evidence — the same "spend" record Acquire used
// to write, now written where the fact is true.
func TestStartRecordsTheBeginning(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2})

	release, err := m.Acquire(ctx, "wi-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()
	m.Start(ctx, "wi-1")

	if r := m.Report(); r.Started != 1 {
		t.Fatalf("start should record one begun run, got %d", r.Started)
	}
	found := false
	for rec, err := range m.Store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "spend" && rec.Subject == "wi-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("start wrote no spend record")
	}
}

// TestMaxPerWindowCountsCompletedStartsNotAcquireAttempts: the brake this
// issue is about. A window that never saw a real start must not close, no
// matter how many times the same run is claimed; a window that saw one must
// close on the very next claim.
func TestMaxPerWindowCountsCompletedStartsNotAcquireAttempts(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, MaxPerWindow: 1, Window: time.Hour})

	// Claimed and released twice without ever starting: the window is
	// untouched, because no run actually began.
	for i := 0; i < 2; i++ {
		release, err := m.Acquire(ctx, "wi-1")
		if err != nil {
			t.Fatalf("acquire %d with no starts yet: %v", i, err)
		}
		release()
	}
	if r := m.Report(); r.Started != 0 {
		t.Fatalf("no start happened yet, got %d", r.Started)
	}

	release, err := m.Acquire(ctx, "wi-1")
	if err != nil {
		t.Fatalf("acquire before the one real start: %v", err)
	}
	m.Start(ctx, "wi-1")
	release()

	if _, err := m.Acquire(ctx, "wi-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("one completed start should close a MaxPerWindow=1 window, got %v", err)
	}
}

// TestJudgementNeverWaitsOnBuilds: a judgement (the planner's run) draws
// from its own seat pool, so three build seats held (issue #202's whole
// point) must not make a judgement wait.
func TestJudgementNeverWaitsOnBuilds(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 3, MaxJudgements: 1})

	for i := 0; i < 3; i++ {
		if _, err := m.Acquire(ctx, "wi-build"); err != nil {
			t.Fatalf("build acquire %d: %v", i, err)
		}
	}

	if _, err := m.AcquireJudgement(ctx, "origination-1"); err != nil {
		t.Fatalf("a judgement should never wait on builds, got %v", err)
	}
}

// TestBuildNeverWaitsOnAJudgement: the reverse of
// TestJudgementNeverWaitsOnBuilds. With the single judgement seat held, a
// second AcquireJudgement refuses ErrBusy while a build's Acquire still
// succeeds.
func TestBuildNeverWaitsOnAJudgement(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 3, MaxJudgements: 1})

	if _, err := m.AcquireJudgement(ctx, "origination-1"); err != nil {
		t.Fatalf("first judgement acquire: %v", err)
	}

	if _, err := m.AcquireJudgement(ctx, "origination-2"); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second judgement with MaxJudgements=1 held should refuse ErrBusy, got %v", err)
	}

	if _, err := m.Acquire(ctx, "wi-build"); err != nil {
		t.Fatalf("a build should never wait because a judgement is running, got %v", err)
	}
}

// TestAcquireJudgementRefusesOverBudget: AcquireJudgement runs the same
// window checks Acquire does — MaxPerWindow and MaxTokensPerWindow among
// them — because the window bounds the account, not the seat.
func TestAcquireJudgementRefusesOverBudget(t *testing.T) {
	ctx := context.Background()

	t.Run("max per window", func(t *testing.T) {
		m, _ := meter(t, Budget{MaxJudgements: 5, MaxPerWindow: 1, Window: time.Hour})
		release, err := m.AcquireJudgement(ctx, "origination-1")
		if err != nil {
			t.Fatalf("first judgement acquire: %v", err)
		}
		m.Start(ctx, "origination-1")
		release()

		if _, err := m.AcquireJudgement(ctx, "origination-2"); !errors.Is(err, ErrBudget) {
			t.Fatalf("a started run against MaxPerWindow=1 should refuse the next, got %v", err)
		}
	})

	t.Run("max tokens per window", func(t *testing.T) {
		m, _ := meter(t, Budget{MaxJudgements: 5, MaxTokensPerWindow: 10_000, Window: time.Hour})
		release, err := m.AcquireJudgement(ctx, "origination-1")
		if err != nil {
			t.Fatalf("first judgement acquire: %v", err)
		}
		m.Charge(ctx, "origination-1", Spend{Total: 11_000, Reported: true})
		release()

		if _, err := m.AcquireJudgement(ctx, "origination-2"); !errors.Is(err, ErrBudget) {
			t.Fatalf("spend over MaxTokensPerWindow should refuse the next judgement, got %v", err)
		}
	})
}

// TestReportSplitsRunningBySeatPool: RunningBuilds and RunningJudgements
// move independently, and MaxJudgements defaults to one the same way
// MaxConcurrent defaults to one.
func TestReportSplitsRunningBySeatPool(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 3})

	buildRelease, err := m.Acquire(ctx, "wi-build")
	if err != nil {
		t.Fatalf("build acquire: %v", err)
	}
	if r := m.Report(); r.RunningBuilds != 1 || r.RunningJudgements != 0 || r.MaxJudgements != 1 {
		t.Fatalf("build alone should move RunningBuilds only: %+v", r)
	}

	judgementRelease, err := m.AcquireJudgement(ctx, "origination-1")
	if err != nil {
		t.Fatalf("judgement acquire: %v", err)
	}
	if r := m.Report(); r.RunningBuilds != 1 || r.RunningJudgements != 1 {
		t.Fatalf("build and judgement should both be visible and independent: %+v", r)
	}

	buildRelease()
	if r := m.Report(); r.RunningBuilds != 0 || r.RunningJudgements != 1 {
		t.Fatalf("releasing the build should not touch the judgement seat: %+v", r)
	}
	judgementRelease()
	if r := m.Report(); r.RunningJudgements != 0 {
		t.Fatalf("releasing the judgement should clear it: %+v", r)
	}
}
