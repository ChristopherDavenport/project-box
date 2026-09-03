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
