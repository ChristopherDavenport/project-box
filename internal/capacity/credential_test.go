package capacity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestAPlanBoxRefusesAKeyRun: a box that named plan, whose run reports cost
// and no account window, refuses the next start and writes the refusal
// record — the mismatch the issue names first: a key left in the daemon's
// environment must stop the box rather than quietly move the run onto the
// invoice.
func TestAPlanBoxRefusesAKeyRun(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, Credential: CredentialPlan})
	m.Charge(ctx, "run-1", Spend{Input: 1_000, Output: 500, CostMicros: Micros(1.50), Reported: true})

	if err := take(ctx, m, "run-2"); !errors.Is(err, ErrCredential) {
		t.Fatalf("a plan box given a key run was not refused: %v", err)
	}
	found := false
	for rec, err := range m.Store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "budget-refused" && strings.Contains(string(rec.Body), `"reason":"credential"`) {
			found = true
			if !strings.Contains(string(rec.Body), `"named":"plan"`) || !strings.Contains(string(rec.Body), `"reported":"key"`) {
				t.Errorf("the record does not name both kinds: %s", rec.Body)
			}
		}
	}
	if !found {
		t.Fatal("no credential refusal on the record")
	}
}

// TestAKeyBoxRefusesAPlanRun: the same finding, the other direction — a box
// that named key, whose run reports an account window, refuses. A
// subscription run under a cost ceiling prices nothing for the ceiling to
// enforce.
func TestAKeyBoxRefusesAPlanRun(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, Credential: CredentialKey})
	m.Charge(ctx, "run-1", Spend{Total: 1_000, Reported: true,
		Limit: &Limit{Status: "allowed", Window: "five_hour", Utilization: 0.1, ResetsAt: now.Add(time.Hour).Unix()}})

	if err := take(ctx, m, "run-2"); !errors.Is(err, ErrCredential) {
		t.Fatalf("a key box given a plan run was not refused: %v", err)
	}
}

// TestTheMismatchingRunsSpendIsStillCharged: spend is spend. The run that
// tripped the mismatch is charged into the window's account before the
// meter closes to new starts, exactly as an unmetered or unpriced run's
// charge is not discarded either.
func TestTheMismatchingRunsSpendIsStillCharged(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, Credential: CredentialPlan})
	m.Charge(ctx, "run-1", Spend{Input: 1_000, Output: 500, CostMicros: Micros(1.50), Reported: true})

	rep := m.Report()
	if rep.Tokens != 1_500 || rep.CostMicros != Micros(1.50) {
		t.Fatalf("the mismatching run's spend was not accrued: tokens=%d cost=%d", rep.Tokens, rep.CostMicros)
	}
	if !rep.Refusing {
		t.Fatal("charged spend but the window does not say it is refusing")
	}
}

// TestAMatchingCredentialIsUnaffected: a box naming a credential whose runs
// report that same kind runs exactly as it did before this brake existed.
func TestAMatchingCredentialIsUnaffected(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, Credential: CredentialPlan})
	m.Charge(ctx, "run-1", Spend{Total: 1_000, Reported: true,
		Limit: &Limit{Status: "allowed", Window: "five_hour", Utilization: 0.1, ResetsAt: now.Add(time.Hour).Unix()}})

	if err := take(ctx, m, "run-2"); err != nil {
		t.Fatalf("a plan box given a plan run was refused: %v", err)
	}
}

// TestAnUnnamedCredentialRefusesUnderABrake is the meter-side half of the
// UNSAID case; boxcfg refuses this at load, but a Meter built directly in Go
// (not from a config file) needs the same belt: a brake set with no
// credential named does not know what it is protecting.
func TestAnUnnamedCredentialRefusesUnderABrake(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour, WindowReserve: 0.5})

	if err := take(ctx, m, "run-1"); !errors.Is(err, ErrCredential) {
		t.Fatalf("window_reserve set with no credential named was not refused: %v", err)
	}
	found := false
	for rec, err := range m.Store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "budget-refused" && strings.Contains(string(rec.Body), `"reason":"credential-unsaid"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no credential-unsaid refusal on the record")
	}
}

// TestAnUnbrakedBudgetNeedsNoCredential: refusing every empty credential
// would stop every box that never asked to budget at all — only a brake
// makes the question load-bearing.
func TestAnUnbrakedBudgetNeedsNoCredential(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour})
	if err := take(ctx, m, "run-1"); err != nil {
		t.Fatalf("an unbraked budget with no credential named was refused: %v", err)
	}
}

// TestTheCredentialMismatchSurvivesARestart: the mismatch counter is set
// inside accrue from the charge body, which already carries Limit and
// CostMicros, so Restore folds it back out of the chain for free — a brake
// a restart clears is the finding capacity.go's Restore doc exists about.
func TestTheCredentialMismatchSurvivesARestart(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 2, Window: time.Hour, Credential: CredentialPlan}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	m.Charge(ctx, "run-1", Spend{Input: 1_000, Output: 500, CostMicros: Micros(1.50), Reported: true})
	if err := take(ctx, m, "run-2"); !errors.Is(err, ErrCredential) {
		t.Fatalf("mismatch did not refuse before restart: %v", err)
	}

	m2 := reopen()
	m2.SetBudget(budget)
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if err := take(ctx, m2, "run-3"); !errors.Is(err, ErrCredential) {
		t.Fatalf("the restored meter forgot the credential mismatch: %v", err)
	}
}

// TestReportCarriesBothCredentials: the box's own NAMED credential and the
// LAST run's REPORTED one are separate facts on the report, and an absent
// one must not render as a zero-ish value that reads like a kind.
func TestReportCarriesBothCredentials(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour})
	if rep := m.Report(); rep.Credential != "" || rep.LastCredential != "" {
		t.Fatalf("a fresh meter with no credential named and no charge should read absent: %+v", rep)
	}

	m.Charge(ctx, "run-1", Spend{Input: 1_000, Output: 500, CostMicros: Micros(1.50), Reported: true})
	rep := m.Report()
	if rep.Credential != "" {
		t.Fatalf("credential = %q, want absent — this budget never named one", rep.Credential)
	}
	if rep.LastCredential != CredentialKey {
		t.Fatalf("last_credential = %q, want key", rep.LastCredential)
	}
}
