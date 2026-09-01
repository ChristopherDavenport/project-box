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

// TestTokenBudgetStopsTheWindow: the brake is denominated the way the invoice
// is. Counting run STARTS cannot tell a cheap factory from an expensive one —
// a start is one number whether the run behind it reads three files or the
// whole repository — so the run count stays unlimited here and the tokens do
// all the stopping.
func TestTokenBudgetStopsTheWindow(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 10_000, Window: time.Hour})

	if err := take(ctx, m, "wi-1"); err != nil {
		t.Fatalf("first run refused: %v", err)
	}
	m.Charge(ctx, "wi-1", 4_000, true)
	if err := take(ctx, m, "wi-2"); err != nil {
		t.Fatalf("under budget but refused: %v", err)
	}
	m.Charge(ctx, "wi-2", 7_000, true)

	// 11k spent against a 10k window.
	if err := take(ctx, m, "wi-3"); !errors.Is(err, ErrBudget) {
		t.Fatalf("over budget should refuse, got %v", err)
	}
	if spent, unmetered := m.Spent(); spent != 11_000 || unmetered != 0 {
		t.Fatalf("spend accounting: %d tokens, %d unmetered", spent, unmetered)
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
	m.Charge(ctx, "wi-1", 0, false) // the agent said nothing

	err := take(ctx, m, "wi-2")
	if !errors.Is(err, ErrUnmetered) {
		t.Fatalf("want ErrUnmetered, got %v", err)
	}
	// Nowhere near the token ceiling: the refusal is about not knowing, and
	// it must report that rather than a total it cannot trust.
	if errors.Is(err, ErrBudget) {
		t.Fatal("an unmetered window reported itself as over budget")
	}
	if _, unmetered := m.Spent(); unmetered != 1 {
		t.Fatalf("unmetered runs not counted: %d", unmetered)
	}
}

// TestUnmeteredIsFreeWithoutATokenBudget: the fail-closed rule arms with the
// budget and not before. An operator who never asked for a cost brake must not
// have the factory stop because an agent is quiet about tokens.
func TestUnmeteredIsFreeWithoutATokenBudget(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour})

	for i := range 3 {
		if err := take(ctx, m, "wi"); err != nil {
			t.Fatalf("run %d refused with no token budget: %v", i, err)
		}
		m.Charge(ctx, "wi", 0, false)
	}
}

// TestWindowRolloverForgives: budgets are per window, and both the spend and
// the not-knowing expire with it.
func TestWindowRolloverForgives(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 5_000, Window: time.Hour})

	take(ctx, m, "wi-1")
	m.Charge(ctx, "wi-1", 9_000, true)
	m.Charge(ctx, "wi-1", 0, false)
	if err := take(ctx, m, "wi-2"); err == nil {
		t.Fatal("an exhausted, unmetered window admitted a run")
	}

	*now = now.Add(time.Hour + time.Minute)
	if err := take(ctx, m, "wi-3"); err != nil {
		t.Fatalf("the new window should admit work: %v", err)
	}
	if spent, unmetered := m.Spent(); spent != 0 || unmetered != 0 {
		t.Fatalf("rollover kept the old window's account: %d tokens, %d unmetered", spent, unmetered)
	}
}
