// Package capacity is the budget the factory spends itself against. Work
// waits rather than overdrawing, and both the spend and the refusals are
// recorded — an autonomous system whose cost is invisible until the invoice
// is an incident with a delay on it.
package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// ErrBudget reports an exhausted window: not a failure, the factory pacing
// itself. Callers leave the work pending and try next window.
var ErrBudget = errors.New("capacity: window budget exhausted — work waits")

// ErrBusy reports full concurrency; try again when something finishes.
var ErrBusy = errors.New("capacity: concurrency limit reached")

// ErrUnmetered reports that a run in this window finished without saying what
// it cost, so the factory has stopped claiming.
//
// Fail closed. The one thing a meter must never do is read unknown as zero —
// that is how a budget stays green while the money leaves — and an autonomous
// agent whose spend is invisible is not a thing to keep running because
// nothing has visibly broken. Claiming stops until the window turns over,
// which is loud, cheap and recoverable; the alternative is a brake that
// silently disengaged. Set [Budget.AllowUnmetered] to proceed anyway.
var ErrUnmetered = errors.New("capacity: a run reported no cost — claims stopped until the window turns (set allow_unmetered to proceed blind)")

// Budget bounds a tenant's execution.
type Budget struct {
	// MaxConcurrent bounds simultaneous runs. Zero means one — an unset
	// budget must never mean an unbounded one, same rule as rollouts.
	MaxConcurrent int
	// MaxPerWindow bounds run STARTS per window; zero means unlimited
	// starts (concurrency still binds).
	MaxPerWindow int
	// MaxTokensPerWindow bounds what the window may SPEND, in tokens; zero
	// means unbounded.
	//
	// Counting starts is not counting cost. A start is one number whether the
	// run behind it reads three files or the whole repository, so a budget
	// denominated in starts cannot tell a cheap factory from an expensive
	// one and cannot stop the expensive one. This is the number an invoice
	// is denominated in, so it is the number the brake is denominated in.
	MaxTokensPerWindow int64
	// AllowUnmetered lets the factory keep working after a run that would not
	// say what it cost. It defaults to false, and the default is the point:
	// an autonomous agent whose spend is invisible is a position nobody
	// chooses deliberately and anybody can end up in. An unset policy must
	// never resolve to the permissive reading, here as everywhere else.
	//
	// This binds whether or not a token budget is set. Without measurement
	// there is no budget to enforce, so measurement is the precondition
	// rather than a feature of the budget. An operator who genuinely wants to
	// fly blind can say so, and the saying is on the record.
	AllowUnmetered bool
	Window         time.Duration
}

// Meter enforces a budget and records what it does.
type Meter struct {
	Store  store.Store
	Tenant tenant.ID
	Actor  string
	Budget Budget
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time

	mu          sync.Mutex
	running     int
	windowStart time.Time
	started     int
	refused     bool  // one refusal record per window, not one per poll
	spent       int64 // tokens charged this window
	unmetered   int   // runs this window that finished without saying what they cost
}

// SetBudget swaps the budget (config reload). The window's spend is NOT
// cleared: windows are aligned to the clock rather than to anything a process
// or a config file did, so there is nothing to restart — and a reload that
// zeroed the account would make the brake resettable by editing a file and
// sending a signal, which is not a brake.
func (m *Meter) SetBudget(b Budget) {
	m.mu.Lock()
	m.Budget = b
	m.mu.Unlock()
}

// Restore folds the current window's account back out of the evidence chain.
//
// The meter wrote every charge and every start to control/capacity and then
// read none of them back: the window lived in memory alone. So a restart
// cleared the brake. Measured live — the meter refused at 25,419,130 tokens
// against a 6,000,000 ceiling, boxd was restarted, and the very next status
// read tokens=0, refusing=false. A crash loop is an unbounded budget, and the
// one thing that makes a token brake worth having is that it survives the
// process it is braking.
//
// Call it once, at construction, before the first Acquire.
func (m *Meter) Restore(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	start := m.windowAt(m.now())
	m.resetWindow(start)
	if m.Budget.Window <= 0 {
		return nil // no window, nothing to bound, nothing to restore
	}
	for rec, err := range m.Store.Log().Since(ctx, m.Tenant, "control/capacity", 1) {
		if err != nil {
			return err
		}
		if rec.At.Before(start) {
			continue
		}
		switch rec.Kind {
		case "spend":
			m.started++
		case "charge":
			var c struct {
				Tokens  int64 `json:"tokens"`
				Metered bool  `json:"metered"`
			}
			if json.Unmarshal(rec.Body, &c) != nil {
				// A charge that will not parse is spend we cannot account
				// for, which is exactly what unmetered means.
				m.unmetered++
				continue
			}
			if c.Metered {
				m.spent += c.Tokens
			} else {
				m.unmetered++
			}
		}
	}
	return nil
}

// windowAt is the accounting window a moment falls in, aligned to the clock.
//
// Aligned rather than sliding from first use, because a persistent budget
// needs "the current window" to be answerable by a process that has just
// started and has no memory of when the last one began. A clock-derived
// bucket is the same answer for every reader, needs no extra record to
// anchor it, and gives an operator a reset time they can predict.
func (m *Meter) windowAt(now time.Time) time.Time {
	if m.Budget.Window <= 0 {
		return time.Time{}
	}
	return now.Truncate(m.Budget.Window)
}

func (m *Meter) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Acquire takes one execution slot, returning its release. The spend is
// evidence; so is the first refusal of each window.
func (m *Meter) Acquire(ctx context.Context, what string) (func(), error) {
	m.mu.Lock()
	maxC := m.Budget.MaxConcurrent
	if maxC <= 0 {
		maxC = 1
	}
	if m.running >= maxC {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	if w := m.windowAt(m.now()); !w.Equal(m.windowStart) {
		m.resetWindow(w)
	}
	if m.Budget.MaxPerWindow > 0 && m.started >= m.Budget.MaxPerWindow {
		first := !m.refused
		m.refused = true
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what,
				fmt.Sprintf(`{"max_per_window":%d,"window":%q}`, m.Budget.MaxPerWindow, m.Budget.Window))
		}
		return nil, ErrBudget
	}
	// Unknown before over-budget: a window holding an unmetered run has no
	// trustworthy total, so refusing for the total it happens to show would
	// be reporting the wrong reason for the right answer.
	if m.unmetered > 0 && !m.Budget.AllowUnmetered {
		first := !m.refused
		m.refused = true
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what,
				fmt.Sprintf(`{"reason":"unmetered","unmetered_runs":%d,"window":%q}`, m.unmetered, m.Budget.Window))
		}
		return nil, ErrUnmetered
	}
	if m.Budget.MaxTokensPerWindow > 0 {
		if m.spent >= m.Budget.MaxTokensPerWindow {
			first := !m.refused
			m.refused = true
			spent := m.spent
			m.mu.Unlock()
			if first {
				m.record(ctx, "budget-refused", what,
					fmt.Sprintf(`{"reason":"tokens","spent":%d,"max_tokens_per_window":%d,"window":%q}`,
						spent, m.Budget.MaxTokensPerWindow, m.Budget.Window))
			}
			return nil, ErrBudget
		}
	}
	m.running++
	m.started++
	m.mu.Unlock()
	m.record(ctx, "spend", what, fmt.Sprintf(`{"running":%d,"started_this_window":%d}`, m.running, m.started))
	return func() {
		m.mu.Lock()
		m.running--
		m.mu.Unlock()
	}, nil
}

// Charge records what a finished run actually cost. Only the agent stream
// knows: the loop sees a commit and the pump sees a slot, so the number has
// to travel back from the adapter rather than being inferred here.
//
// metered false means the run said nothing about its cost. That is recorded
// as its own fact — never as a zero — and under a token budget it closes the
// window to new claims; see [ErrUnmetered].
func (m *Meter) Charge(ctx context.Context, what string, tokens int64, metered bool) {
	m.mu.Lock()
	if metered {
		m.spent += tokens
	} else {
		m.unmetered++
	}
	spent, unmetered := m.spent, m.unmetered
	m.mu.Unlock()
	m.record(ctx, "charge", what,
		fmt.Sprintf(`{"tokens":%d,"metered":%v,"spent_this_window":%d,"unmetered_this_window":%d}`,
			tokens, metered, spent, unmetered))
}

// Report is the window's account as an operator reads it.
//
// Unmetered travels beside the total on purpose: the total alone is a lie
// whenever that number is not zero, and a spend figure shown without it would
// be the most reassuring thing on the screen and the least true.
type Report struct {
	Tokens        int64         `json:"tokens"`
	Unmetered     int           `json:"unmetered"`
	MaxTokens     int64         `json:"max_tokens"`
	Running       int           `json:"running"`
	Started       int           `json:"started"`
	MaxConcurrent int           `json:"max_concurrent"`
	MaxPerWindow  int           `json:"max_per_window"`
	Window        time.Duration `json:"-"`
	WindowSec     int64         `json:"window_sec"`
	// Refusing is true while the meter is turning work away, so a room can
	// say the factory is paced rather than idle. An operator watching nothing
	// happen needs those told apart.
	Refusing bool `json:"refusing"`
}

// Report snapshots the window.
func (m *Meter) Report() Report {
	m.mu.Lock()
	defer m.mu.Unlock()
	maxC := m.Budget.MaxConcurrent
	if maxC <= 0 {
		maxC = 1
	}
	return Report{
		Tokens: m.spent, Unmetered: m.unmetered, MaxTokens: m.Budget.MaxTokensPerWindow,
		Running: m.running, Started: m.started,
		MaxConcurrent: maxC, MaxPerWindow: m.Budget.MaxPerWindow,
		Window: m.Budget.Window, WindowSec: int64(m.Budget.Window.Seconds()),
		Refusing: m.refused,
	}
}

// resetWindow starts a fresh accounting window at its aligned boundary.
// Callers hold mu.
func (m *Meter) resetWindow(start time.Time) {
	m.windowStart, m.started, m.refused = start, 0, false
	m.spent, m.unmetered = 0, 0
}

func (m *Meter) record(ctx context.Context, kind, subject, body string) {
	m.Store.Log().Append(ctx, evidence.Record{
		Tenant: m.Tenant, Scope: "control/capacity", Kind: kind, Subject: subject,
		Actor: m.Actor, Body: []byte(body),
	})
}
