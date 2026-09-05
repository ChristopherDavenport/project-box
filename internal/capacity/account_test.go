package capacity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTheAccountsWindowBrakes is round 4's finding as a mechanism. The
// factory's meter counted its own tokens and refused nothing while one run
// consumed the account's five-hour window; every run now reports that window,
// and above the reserve the meter refuses new starts until it resets.
func TestTheAccountsWindowBrakes(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, WindowReserve: 0.6})
	resets := now.Add(90 * time.Minute)
	m.Charge(ctx, "run-1", Spend{Total: 1_000_000, Reported: true,
		Limit: &Limit{Status: "allowed_warning", Window: "five_hour", Utilization: 0.82, ResetsAt: resets.Unix()}})

	err := take(ctx, m, "run-2")
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("a window at 82%% under a 60%% reserve was not refused: %v", err)
	}
	for _, want := range []string{"five_hour", "82%", "60%"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if !m.Report().Refusing || m.Report().AccountWindow == nil {
		t.Fatalf("the room would not be told: %+v", m.Report())
	}
	// The record says which window, how full, and when it turns.
	found := false
	for rec, err := range m.Store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "budget-refused" && strings.Contains(string(rec.Body), `"reason":"account-window"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no account-window refusal on the record")
	}
	// Past the reset it is a fresh window, and the meter says so by letting
	// work through — without any run having reported since.
	*now = resets.Add(time.Minute)
	if err := take(ctx, m, "run-3"); err != nil {
		t.Fatalf("a reset window still refused: %v", err)
	}
}

// TestBelowTheReserveIsNotABrake: the reserve is a share, and under it the
// account's window is nobody's concern.
func TestBelowTheReserveIsNotABrake(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, WindowReserve: 0.6})
	m.Charge(ctx, "run-1", Spend{Total: 10, Reported: true,
		Limit: &Limit{Status: "allowed", Window: "five_hour", Utilization: 0.3, ResetsAt: now.Add(time.Hour).Unix()}})
	if err := take(ctx, m, "run-2"); err != nil {
		t.Fatalf("30%% under a 60%% reserve refused: %v", err)
	}
}

// TestARejectedAccountRefusesWithoutAReserve: a run that died at the limit
// is exactly the report that matters most, and it counts whether or not the
// run was metered and whether or not a reserve is set.
func TestARejectedAccountRefusesWithoutAReserve(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, AllowUnmetered: true})
	m.Charge(ctx, "run-1", Spend{Reported: false,
		Limit: &Limit{Status: "rejected", Window: "five_hour", Utilization: 1, ResetsAt: now.Add(2 * time.Hour).Unix()}})
	if err := take(ctx, m, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("a rejected account was not refused: %v", err)
	}
	*now = now.Add(3 * time.Hour)
	if err := take(ctx, m, "run-3"); err != nil {
		t.Fatalf("still refused after the account reset: %v", err)
	}
}

// TestTheAccountsWindowSurvivesARestart: the report rides the charge record,
// so a daemon restarted into a spent account does not start spending again
// because it forgot — even when the meter's own window has since rolled.
func TestTheAccountsWindowSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, WindowReserve: 0.6})
	resets := now.Add(4 * time.Hour)
	m.Charge(ctx, "run-1", Spend{Total: 10, Reported: true,
		Limit: &Limit{Status: "allowed_warning", Window: "five_hour", Utilization: 0.9, ResetsAt: resets.Unix()}})

	later := now.Add(2 * time.Hour) // the meter's hour has rolled; the account's has not
	m2 := &Meter{Store: m.Store, Tenant: tn, Actor: "runner-2", Now: func() time.Time { return later }}
	m2.SetBudget(m.Budget)
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if err := take(ctx, m2, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("the restored meter forgot the account's window: %v", err)
	}
}
