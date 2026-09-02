package acp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestOneRunCannotSpendPastItsCeiling.
//
// The window budget gates whether a run may START and then has no further
// say, so the ceiling it enforces is really "the limit, plus whatever one run
// spends before stopping on its own". Measured live, that was not a rounding
// error: a 6,000,000-token window was carried to 25,419,130 by a single
// session that ran 110 tool calls for 21 minutes and cost $13.60. It broke no
// rule, because there was no rule to break.
//
// The `spendy` agent never settles and never stops reporting cost. Nothing in
// this test can end it except the ceiling — if the bound does not bind, the
// context deadline does, and the test says which happened.
func TestOneRunCannotSpendPastItsCeiling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt := runtimeFor(t, "spendy")
	rt.MaxTokens = 5000

	start := time.Now()
	res, err := rt.Run(ctx, bundle(), "spend everything")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTokenCeiling) {
		t.Fatalf("err = %v, want ErrTokenCeiling (elapsed %s)", err, elapsed)
	}
	if ctx.Err() != nil {
		t.Fatal("the deadline stopped the run, not the ceiling")
	}
	// The kill must not lose the bill. A run stopped for spending too much is
	// the last one whose cost may go unrecorded, and a meter that charged
	// nothing for it would be exactly wrong.
	if !res.Usage.Reported || res.Usage.Total() < 5000 {
		t.Fatalf("the killed run reported no cost: %+v", res.Usage)
	}
}

// TestTheCeilingIsOffByDefault. Zero means unbounded, so an operator who has
// not set one gets today's behaviour rather than a surprise kill.
func TestTheCeilingIsOffByDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt := runtimeFor(t, "streamusage") // spends 300, settles normally
	if rt.MaxTokens != 0 {
		t.Fatal("MaxTokens should default to unbounded")
	}
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Usage.Total() != 300 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

// TestTheCeilingDoesNotFireBelowItself guards the other direction: a run that
// stays under its bound settles normally and is not killed for approaching
// one.
func TestTheCeilingDoesNotFireBelowItself(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt := runtimeFor(t, "streamusage") // spends 300 in two updates of 150
	rt.MaxTokens = 10_000
	res, err := rt.Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("a run well under its ceiling was killed: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want a normal settle", res.StopReason)
	}
}
