package capacity

import (
	"context"
	"testing"
)

// TestWindowIsNilBeforeAnyRunReports: the meter's first exported read of the
// account window must not invent one — a box nobody has heard from yet has
// no window to hand back, not a zero-valued one that would read as an
// account never at risk (issue #112).
func TestWindowIsNilBeforeAnyRunReports(t *testing.T) {
	m, _ := meter(t, Budget{})
	if w := m.Window(); w != nil {
		t.Fatalf("Window() = %+v, want nil before any run ever reported one", w)
	}
}

// TestWindowReturnsACopyOfTheHeldLimit: the last account window a run
// reported, handed back as a copy so a caller mutating it cannot reach back
// into the meter's own state.
func TestWindowReturnsACopyOfTheHeldLimit(t *testing.T) {
	m, _ := meter(t, Budget{})
	ctx := context.Background()
	m.Charge(ctx, "run-1", Spend{
		Reported: true, Total: 10,
		Limit: &Limit{Status: "allowed", Window: "five_hour", Utilization: 0.4, ResetsAt: 1234},
	})
	w := m.Window()
	if w == nil {
		t.Fatal("Window() = nil after a run reported one")
	}
	if w.Window != "five_hour" || w.ResetsAt != 1234 || w.Status != "allowed" {
		t.Fatalf("Window() = %+v, want the last reported limit", w)
	}
	w.Status = "rejected"
	if held := m.Window(); held.Status != "allowed" {
		t.Fatalf("mutating the returned copy reached the meter's own state: %+v", held)
	}
}
