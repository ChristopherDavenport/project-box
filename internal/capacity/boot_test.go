package capacity

import (
	"context"
	"testing"
	"time"
)

// TestBootTotalsSurviveWindowRolloverWhileWindowFiguresDoNot: issue #186.
// /v1/status wants a since-boot spend beside the window's, and the two must
// disagree the moment a window turns — otherwise the boot figure is just the
// window figure with a different name.
func TestBootTotalsSurviveWindowRolloverWhileWindowFiguresDoNot(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour})

	take(ctx, m, "wi-1")
	m.Charge(ctx, "wi-1", Spend{Total: 100, Reported: true})

	*now = now.Add(time.Hour + time.Minute) // into the next window

	take(ctx, m, "wi-2")
	m.Charge(ctx, "wi-2", Spend{Total: 50, Reported: true})

	r := m.Report()
	if r.Tokens != 50 {
		t.Fatalf("window tokens = %d, want only the new window's 50", r.Tokens)
	}
	if r.BootTokens != 150 {
		t.Fatalf("boot tokens = %d, want both charges' 150", r.BootTokens)
	}
}

// TestBootUnpricedSurvivesRolloverWhileWindowUnpricedResets: an unpriced
// charge before the roll is a gap in the money total that a window reset
// must not erase — the boot figure is the one an operator reads to notice a
// pricing gap that has been open since before the last window turned.
func TestBootUnpricedSurvivesRolloverWhileWindowUnpricedResets(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour, MaxCostPerWindow: 1_000_000})

	take(ctx, m, "wi-1")
	// Tokens reported, cost not: unpriced under a cost ceiling.
	m.Charge(ctx, "wi-1", Spend{Total: 10, Reported: true})

	if r := m.Report(); r.Unpriced != 1 || r.BootUnpriced != 1 {
		t.Fatalf("before the roll: window unpriced = %d, boot unpriced = %d, want 1 and 1", r.Unpriced, r.BootUnpriced)
	}

	*now = now.Add(time.Hour + time.Minute) // into the next window

	r := m.Report()
	if r.Unpriced != 0 {
		t.Fatalf("window unpriced did not reset across the roll: %d", r.Unpriced)
	}
	if r.BootUnpriced != 1 {
		t.Fatalf("boot unpriced was cleared by a window rollover: %d", r.BootUnpriced)
	}
}

// TestRestoreLeavesBootTotalsAtZero: Restore refolds the CURRENT window from
// records that were all written before this boot came up. None of that spend
// happened during this process's lifetime, so none of it may land in the
// boot totals — only the live Charge path may do that. Without the explicit
// split between accrue and accrueBoot, a restart would make "since boot"
// read as "since whenever the evidence chain last had a charge," which is
// not the figure /v1/status wants beside uptime_sec.
func TestRestoreLeavesBootTotalsAtZero(t *testing.T) {
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

	if got := m.Report().BootTokens; got != 20_076_747 {
		t.Fatalf("the original process's own boot total = %d, want its live charge counted", got)
	}

	// The restart.
	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	r := m2.Report()
	if r.Tokens != 20_076_747 {
		t.Fatalf("restored window tokens = %d, want the pre-restart charge refolded", r.Tokens)
	}
	if r.BootTokens != 0 {
		t.Fatalf("restore accrued a pre-boot charge into the boot total: %d, want 0", r.BootTokens)
	}
}
