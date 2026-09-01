// Package capacity is the budget the factory spends itself against. Work
// waits rather than overdrawing, and both the spend and the refusals are
// recorded — an autonomous system whose cost is invisible until the invoice
// is an incident with a delay on it.
package capacity

import (
	"context"
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
// it cost, while a token budget was in force.
//
// Fail closed. An operator who set a token budget asked to be protected from
// spend, and the one thing a meter must never do is read unknown as zero —
// that is how a budget stays green while the money leaves. Claiming stops
// until the window turns over, which is loud, cheap, and recoverable; the
// alternative is a brake that silently disengaged.
var ErrUnmetered = errors.New("capacity: a run reported no cost while a token budget is in force")

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
	Window             time.Duration
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

// SetBudget swaps the budget (config reload). The current window restarts
// under the new numbers — simpler to reason about than prorating, and a
// reload is rare enough that a fresh window is the least surprising rule.
func (m *Meter) SetBudget(b Budget) {
	m.mu.Lock()
	m.Budget = b
	m.resetWindow(m.now())
	m.mu.Unlock()
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
	now := m.now()
	if m.Budget.Window > 0 && now.Sub(m.windowStart) >= m.Budget.Window {
		m.resetWindow(now)
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
	if m.Budget.MaxTokensPerWindow > 0 {
		// Unknown before over-budget: a window holding an unmetered run has
		// no trustworthy total, so refusing for the total it happens to show
		// would be reporting the wrong reason for the right answer.
		if m.unmetered > 0 {
			first := !m.refused
			m.refused = true
			m.mu.Unlock()
			if first {
				m.record(ctx, "budget-refused", what,
					fmt.Sprintf(`{"reason":"unmetered","unmetered_runs":%d,"window":%q}`, m.unmetered, m.Budget.Window))
			}
			return nil, ErrUnmetered
		}
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

// Spent reports the window's charged tokens and how many runs in it refused
// to say what they cost — the pair a status view needs, since the total alone
// is a lie whenever the second number is not zero.
func (m *Meter) Spent() (tokens int64, unmetered int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spent, m.unmetered
}

// resetWindow starts a fresh accounting window. Callers hold mu.
func (m *Meter) resetWindow(now time.Time) {
	m.windowStart, m.started, m.refused = now, 0, false
	m.spent, m.unmetered = 0, 0
}

func (m *Meter) record(ctx context.Context, kind, subject, body string) {
	m.Store.Log().Append(ctx, evidence.Record{
		Tenant: m.Tenant, Scope: "control/capacity", Kind: kind, Subject: subject,
		Actor: m.Actor, Body: []byte(body),
	})
}
