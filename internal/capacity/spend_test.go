package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
)

// charges reads back every charge record the meter wrote, in order.
func charges(t *testing.T, m *Meter) []Spend {
	t.Helper()
	var out []Spend
	for rec, err := range m.Store.Log().Since(context.Background(), m.Tenant, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind != "charge" {
			continue
		}
		var s Spend
		if err := json.Unmarshal(rec.Body, &s); err != nil {
			t.Fatalf("a charge record does not parse as a Spend: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// TestTheSplitReachesTheMeterAndTheRecord. Four counters arrived from the
// stream and one integer left for the meter, so the window could say how many
// tokens went through and never what they were worth — and the charge record
// it wrote could never be re-priced by anyone later, which is the half that
// cannot be repaired after the fact.
func TestTheSplitReachesTheMeterAndTheRecord(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 2, Window: time.Hour})

	m.Charge(ctx, "wi-1", Spend{
		Input: 12_000, Output: 3_000, CacheRead: 900_000, CacheWrite: 40_000,
		CostMicros: 1_612_000, Reported: true})
	m.Charge(ctx, "wi-2", Spend{
		Input: 8_000, Output: 1_000, CacheRead: 100_000, CacheWrite: 0,
		CostMicros: 240_500, Reported: true})

	r := m.Report()
	if r.Input != 20_000 || r.Output != 4_000 || r.CacheRead != 1_000_000 || r.CacheWrite != 40_000 {
		t.Fatalf("the window's split: in=%d out=%d cache_read=%d cache_write=%d", r.Input, r.Output, r.CacheRead, r.CacheWrite)
	}
	if want := int64(20_000 + 4_000 + 1_000_000 + 40_000); r.Tokens != want {
		t.Fatalf("tokens = %d, want %d", r.Tokens, want)
	}
	// The money the agent itself reported, accumulated as an integer. The
	// two runs cost $1.612 and $0.2405; a float total would already be
	// approximate here and an operator adding up the charges would not
	// reproduce it.
	if r.CostMicros != 1_852_500 {
		t.Fatalf("cost = %d micro-dollars, want 1852500 ($1.8525)", r.CostMicros)
	}
	if got := USD(r.CostMicros); got != 1.8525 {
		t.Fatalf("USD(%d) = %v", r.CostMicros, got)
	}

	// And the same facts are on the chain, because a total is all a later
	// reader gets if the record kept only a total.
	recs := charges(t, m)
	if len(recs) != 2 {
		t.Fatalf("charge records: %d", len(recs))
	}
	if recs[0].CacheRead != 900_000 || recs[0].CostMicros != 1_612_000 || !recs[0].Reported {
		t.Fatalf("first charge record lost the split: %+v", recs[0])
	}
}

// TestTheRecordSaysWhatWasCharged. In memory a tally the agent stands behind
// beats a sum of the parts this runtime happens to recognise. On the record
// that distinction is a trap: `tokens` already means the charged total on
// every row written before the split existed, so a run reporting parts and no
// tally must not write it absent — absent reads as zero to everything
// downstream, which is the failure the whole package refuses elsewhere.
func TestTheRecordSaysWhatWasCharged(t *testing.T) {
	ctx := context.Background()
	m, _ := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour})

	m.Charge(ctx, "wi-1", Spend{Input: 1_000, Output: 500, CacheRead: 20_000, Reported: true})

	recs := charges(t, m)
	if len(recs) != 1 {
		t.Fatalf("charge records: %d", len(recs))
	}
	if recs[0].Total != 21_500 {
		t.Fatalf("the record charged %d, and the meter charged %d", recs[0].Total, m.Report().Tokens)
	}
	if recs[0].Total != m.Report().Tokens {
		t.Fatal("the record and the window disagree about what this run cost")
	}
}

// TestACostCeilingBrakesWhatATokenCeilingCannot is the issue in one test. Two
// windows of identical token volume, one of them fresh input and output and
// one of them almost all cached reads, cost about fifty times apart. A brake
// denominated in tokens stops both or neither; a brake denominated in money
// stops the expensive one.
func TestACostCeilingBrakesWhatATokenCeilingCannot(t *testing.T) {
	ctx := context.Background()
	// $10 an hour, and a token ceiling far too loose to be what is braking.
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 100_000_000,
		MaxCostPerWindow: Micros(10.00), Window: time.Hour}

	cheap, _ := meter(t, budget)
	// A million tokens that are almost all cache reads: about $1.65.
	cheap.Charge(ctx, "wi-1", Spend{Input: 20_000, Output: 5_000, CacheRead: 975_000,
		CostMicros: Micros(1.65), Reported: true})
	if err := take(ctx, cheap, "wi-2"); err != nil {
		t.Fatalf("a cheap million tokens should not brake a $10 window: %v", err)
	}

	dear, _ := meter(t, budget)
	// The same million tokens as fresh input and output: about $52.
	dear.Charge(ctx, "wi-1", Spend{Input: 700_000, Output: 300_000,
		CostMicros: Micros(52.50), Reported: true})
	if err := take(ctx, dear, "wi-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("an expensive million tokens must brake a $10 window, got %v", err)
	}

	// The token ceiling is not what stopped it, and the two windows prove it:
	// identical volume, opposite verdicts.
	if cheap.Report().Tokens != dear.Report().Tokens {
		t.Fatalf("the two windows are not the same volume: %d vs %d",
			cheap.Report().Tokens, dear.Report().Tokens)
	}
	if !dear.Report().Refusing || cheap.Report().Refusing {
		t.Fatalf("refusing: cheap=%v dear=%v", cheap.Report().Refusing, dear.Report().Refusing)
	}
}

// TestAnUnpricedRunClosesACostWindow is the fail-closed rule one currency
// along. A run whose tokens parsed and whose cost did not looks metered from
// every angle except the one that bills, so a cost ceiling that counted it as
// $0.00 would be the brake reading unknown as zero.
//
// And the other half, which is what keeps it honest: with no cost ceiling set
// there is no account for the silence to be a hole in, so it must not refuse.
// A rule that fires where nobody asked for it is how a fail-closed default
// gets switched off wholesale.
func TestAnUnpricedRunClosesACostWindow(t *testing.T) {
	ctx := context.Background()

	priced, _ := meter(t, Budget{MaxConcurrent: 1, MaxCostPerWindow: Micros(10.00), Window: time.Hour})
	priced.Charge(ctx, "wi-1", Spend{Input: 1_000, Output: 500, Reported: true})
	if err := take(ctx, priced, "wi-2"); !errors.Is(err, ErrUnpriced) {
		t.Fatalf("want ErrUnpriced under a cost budget, got %v", err)
	}
	if r := priced.Report(); r.Unpriced != 1 || r.Unmetered != 0 {
		t.Fatalf("an unpriced run is not an unmetered one: unpriced=%d unmetered=%d", r.Unpriced, r.Unmetered)
	}

	tokensOnly, _ := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 1_000_000, Window: time.Hour})
	tokensOnly.Charge(ctx, "wi-1", Spend{Input: 1_000, Output: 500, Reported: true})
	if err := take(ctx, tokensOnly, "wi-2"); err != nil {
		t.Fatalf("no cost ceiling, so an unpriced run is not a gap: %v", err)
	}
	if tokensOnly.Report().Unpriced != 0 {
		t.Fatal("unpriced counted against a window that is not budgeting in money")
	}
}

// TestALongRunIsChargedToTheWindowItReportedIn. A run that started at 11:59
// and reported at 12:01 had its whole cost discarded: Charge added it to the
// window the meter last touched, and the next reader rolled the clock forward
// and zeroed the account. The chain recorded it correctly the whole time, so
// Report and Restore disagreed permanently — and the runs this hits are the
// long ones, which are the expensive ones.
//
// Acquire and Report both roll the window and Charge did not, which is why
// the ordinary case hid it: a claim rolls the clock a moment before every
// charge except the one that straddles a boundary.
func TestALongRunIsChargedToTheWindowItReportedIn(t *testing.T) {
	ctx := context.Background()
	m, now := meter(t, Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour})

	// A claim at 11:59, in the previous window.
	*now = time.Date(2026, 9, 1, 11, 59, 0, 0, time.UTC)
	release, err := m.Acquire(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	// It reports at 12:01, in the next one.
	*now = time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC)
	m.Charge(ctx, "run-1", Spend{Input: 400_000, Output: 100_000,
		CacheRead: 6_000_000, Reported: true})
	release()

	if got := m.Report().Tokens; got != 6_500_000 {
		t.Fatalf("the window that was billed reports %d tokens, want 6,500,000", got)
	}
	if _, err := m.Acquire(ctx, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("6.5M against a 6M ceiling and the brake did not bite: %v", err)
	}
	// And the memory agrees with the chain, which is the actual property —
	// a Restore into this same window must produce the same account.
	if got := m.Report().Input; got != 400_000 {
		t.Fatalf("the split went with it: input=%d", got)
	}
}

// TestTheSplitSurvivesARestart. The window's account lives on the chain, so
// everything the record gained has to come back out of it — otherwise a
// restart silently narrows the meter back to a total.
func TestTheSplitSurvivesARestart(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxCostPerWindow: Micros(10.00), Window: time.Hour}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	m.Charge(ctx, "run-1", Spend{Input: 12_000, Output: 3_000, CacheRead: 900_000,
		CacheWrite: 40_000, CostMicros: Micros(10.50), Reported: true})

	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	before, after := m.Report(), m2.Report()
	if before.Input != after.Input || before.Output != after.Output ||
		before.CacheRead != after.CacheRead || before.CacheWrite != after.CacheWrite {
		t.Fatalf("the restart lost the split:\nbefore %+v\nafter  %+v", before, after)
	}
	if after.CostMicros != Micros(10.50) {
		t.Fatalf("restored cost = %d, want %d", after.CostMicros, Micros(10.50))
	}
	if _, err := m2.Acquire(ctx, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("a restart cleared the cost brake: %v", err)
	}
}

// TestACyclePreSplitChargeStillRestores. The chain is append-only and the
// window Restore reads is the live one, so for as long as one window straddles
// this change the meter will read rows written in the old shape: a bare
// `tokens` and `metered`, with no components and no money. Those must restore
// their total rather than being discarded as unparseable — which would count
// them as unmetered and stop the factory on a deploy.
func TestAPreSplitChargeStillRestores(t *testing.T) {
	now := time.Date(2026, 9, 1, 14, 20, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 1, MaxTokensPerWindow: 6_000_000, Window: time.Hour}
	m, reopen := meters(t, budget, clock)
	ctx := context.Background()

	// Exactly the body the meter wrote before the split existed.
	if _, err := m.Store.Log().Append(ctx, evidence.Record{
		Tenant: tnr, Scope: "control/capacity", Kind: "charge", Subject: "run-1", Actor: "boxd",
		Body: []byte(`{"tokens":20076747,"metered":true,"spent_this_window":20076747,"unmetered_this_window":0}`),
	}); err != nil {
		t.Fatal(err)
	}

	m2 := reopen()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	r := m2.Report()
	if r.Tokens != 20_076_747 {
		t.Fatalf("an old charge restored as %d tokens", r.Tokens)
	}
	if r.Unmetered != 0 {
		t.Fatal("an old charge restored as unmetered — a deploy would stop the factory")
	}
	// Its components are genuinely unknown, and unknown is what it reads as.
	// Inventing a split for a row that never carried one would be the worse
	// answer: a plausible number nobody measured.
	if r.Input != 0 || r.Output != 0 || r.CacheRead != 0 || r.CacheWrite != 0 {
		t.Fatalf("an old charge invented a split: %+v", r)
	}
}
