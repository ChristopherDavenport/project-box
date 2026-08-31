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

// Budget bounds a tenant's execution.
type Budget struct {
	// MaxConcurrent bounds simultaneous runs. Zero means one — an unset
	// budget must never mean an unbounded one, same rule as rollouts.
	MaxConcurrent int
	// MaxPerWindow bounds run STARTS per window; zero means unlimited
	// starts (concurrency still binds).
	MaxPerWindow int
	Window       time.Duration
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
	refused     bool // one refusal record per window, not one per poll
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
		m.windowStart, m.started, m.refused = now, 0, false
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

func (m *Meter) record(ctx context.Context, kind, subject, body string) {
	m.Store.Log().Append(ctx, evidence.Record{
		Tenant: m.Tenant, Scope: "control/capacity", Kind: kind, Subject: subject,
		Actor: m.Actor, Body: []byte(body),
	})
}
