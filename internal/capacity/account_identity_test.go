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

const tna = tenant.ID("acme")

// namedMeters opens two Meters with distinct Account names over ONE store on
// ONE scope — issue #217's whole premise: runners with a credential each,
// sharing the control/capacity chain a single unnamed meter used to have to
// itself. reopen builds a fresh Meter for either name, standing in for a
// restart.
func namedMeters(t *testing.T, b Budget, now func() time.Time) (a, bee *Meter, reopen func(account string) *Meter) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "box.db")
	open := func(account string) *Meter {
		s, err := sqlite.Open(path, sqlite.WithClock(now))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		m := &Meter{Store: s, Tenant: tna, Actor: "boxd", Account: account, Now: now}
		m.SetBudget(b)
		return m
	}
	return open("alice"), open("bob"), open
}

// TestAccountsDoNotShareAWindowBrake: two Meters over one store, one account
// each. A Charge that spends alice's reported account window must not touch
// bob's — the whole reason a Meter needs an account identity in the first
// place, since before this the record carried no account and the second
// meter over the same store would have folded alice's charge into its own.
func TestAccountsDoNotShareAWindowBrake(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 2, Window: time.Hour, WindowReserve: 0.6, Credential: CredentialPlan}
	alice, bob, _ := namedMeters(t, budget, clock)

	resets := now.Add(90 * time.Minute)
	alice.Charge(ctx, "run-1", Spend{Total: 1_000_000, Reported: true,
		Limit: &Limit{Status: "allowed_warning", Window: "five_hour", Utilization: 0.82, ResetsAt: resets.Unix()}})

	if err := take(ctx, alice, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("alice's own spent account window did not refuse her: %v", err)
	}
	if err := take(ctx, bob, "run-2"); err != nil {
		t.Fatalf("bob's account was refused by alice's charge: %v", err)
	}
}

// TestARejectedAccountDoesNotRejectItsNeighbor is the same finding at the
// harder edge: a rejected status refuses with no reserve set at all, and
// still must bind only the account that reported it.
func TestARejectedAccountDoesNotRejectItsNeighbor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 2, Window: time.Hour, AllowUnmetered: true}
	alice, bob, _ := namedMeters(t, budget, clock)

	alice.Charge(ctx, "run-1", Spend{Reported: false,
		Limit: &Limit{Status: "rejected", Window: "five_hour", Utilization: 1, ResetsAt: now.Add(2 * time.Hour).Unix()}})

	if err := take(ctx, alice, "run-2"); !errors.Is(err, ErrBudget) {
		t.Fatalf("alice's rejected account was not refused: %v", err)
	}
	if err := take(ctx, bob, "run-2"); err != nil {
		t.Fatalf("bob's account was refused by alice's rejection: %v", err)
	}
}

// TestRestoreRefoldsOnlyItsOwnAccount seeds both accounts' charges
// interleaved on the one scope, then restarts each: a fresh Meter must
// refold only the charges its own Account wrote, never its neighbor's — the
// two Meters over one store would otherwise fold each other's facts in
// Meter.Restore, exactly the bug #217 exists to close.
func TestRestoreRefoldsOnlyItsOwnAccount(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 2, Window: time.Hour}
	alice, bob, reopen := namedMeters(t, budget, clock)

	// Interleaved: alice, bob, alice.
	alice.Charge(ctx, "run-1", Spend{Total: 100, Reported: true})
	bob.Charge(ctx, "run-2", Spend{Total: 5_000, Reported: true})
	alice.Charge(ctx, "run-3", Spend{Total: 20, Reported: true})

	// The restart.
	alice2 := reopen("alice")
	if err := alice2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := alice2.Report().Tokens; got != 120 {
		t.Fatalf("alice restored %d tokens, want only her own 120 (not bob's 5000 folded in)", got)
	}

	bob2 := reopen("bob")
	if err := bob2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := bob2.Report().Tokens; got != 5_000 {
		t.Fatalf("bob restored %d tokens, want only his own 5000 (not alice's folded in)", got)
	}
}

// TestUnnamedRecordsRestoreOnlyIntoTheDefaultAccount: the pre-#217 chain —
// every record written before this feature existed carries no account key
// at all. Those records must fold into the unnamed default Meter (Account
// "") and must never leak into a named account that happens to share the
// store; the reverse must hold too, a named account's charges must not fold
// into the default. Unknown never reads as belonging to everyone.
func TestUnnamedRecordsRestoreOnlyIntoTheDefaultAccount(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	budget := Budget{MaxConcurrent: 2, Window: time.Hour}
	path := filepath.Join(t.TempDir(), "box.db")
	open := func(account string) *Meter {
		s, err := sqlite.Open(path, sqlite.WithClock(clock))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		m := &Meter{Store: s, Tenant: tna, Actor: "boxd", Account: account, Now: clock}
		m.SetBudget(budget)
		return m
	}

	def := open("") // the unnamed default account — today's pre-#217 meter
	carol := open("carol")

	def.Charge(ctx, "run-1", Spend{Total: 7, Reported: true})
	carol.Charge(ctx, "run-2", Spend{Total: 900, Reported: true})

	def2 := open("")
	if err := def2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := def2.Report().Tokens; got != 7 {
		t.Fatalf("default account restored %d tokens, want only its own 7 (not carol's 900 folded in)", got)
	}

	carol2 := open("carol")
	if err := carol2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := carol2.Report().Tokens; got != 900 {
		t.Fatalf("carol restored %d tokens, want only her own 900 (not the default's folded in)", got)
	}
}

// TestReportNamesItsAccount: capacity.Report carries the account name so
// /v1/status can show which account each report is.
func TestReportNamesItsAccount(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	alice, bob, _ := namedMeters(t, Budget{MaxConcurrent: 1, Window: time.Hour}, clock)

	if got := alice.Report().Account; got != "alice" {
		t.Fatalf("alice's report names account %q, want \"alice\"", got)
	}
	if got := bob.Report().Account; got != "bob" {
		t.Fatalf("bob's report names account %q, want \"bob\"", got)
	}

	def, _ := meter(t, Budget{MaxConcurrent: 1, Window: time.Hour})
	if got := def.Report().Account; got != "" {
		t.Fatalf("the unnamed default meter's report names account %q, want empty", got)
	}
}
