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
	"math"
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

// ErrUnpriced reports a run that said how many tokens it spent and not what
// they cost, under a budget denominated in money.
//
// The same rule as [ErrUnmetered], one currency along. A cost ceiling that
// silently counted an unpriced run as $0.00 would be the brake reading
// unknown as zero, which is the failure the token side already refuses — and
// it is likelier here, because a run whose tokens parse and whose cost does
// not looks metered from every angle except the one that bills.
var ErrUnpriced = errors.New("capacity: a run reported tokens but no cost, under a cost budget — claims stopped until the window turns (set allow_unmetered to proceed blind)")

// ErrCredential reports one of two things, both about which account a box
// runs on rather than how much it spent: a run whose reported credential
// does not match the one [Budget.Credential] names, or a budget with a brake
// set ([Budget.WindowReserve] or [Budget.MaxCostPerWindow]) that never named
// one at all.
//
// The same rule as [ErrUnmetered] and [ErrUnpriced] wearing a different
// coat: which account a run spent against is not allowed to read as
// whichever one is convenient. A key left in the daemon's environment while
// the box believes it is on a subscription would silently move every run
// onto the invoice a plan brake never watches; a subscription run under a
// cost ceiling that thinks it is metering a key prices nothing it should be
// pricing. An unnamed credential with a brake set is the same failure before
// the first mismatched run: a brake that does not say which account it
// binds does not bind anything. Claims stop until an operator names the
// credential, or — for a mismatch — until the window turns and the
// offending run has rolled out of it.
var ErrCredential = errors.New("capacity: a run's credential does not match the one this box named, or a brake is set with none named — claims stopped")

// Spend is what one run cost, in both currencies it is billed in.
//
// Tokens are not fungible. A cached read is roughly a tenth the price of a
// fresh input token and an output token several times either, so two runs
// with identical totals can differ on the invoice by more than an order of
// magnitude — which makes a single total a number that cannot be priced, and
// a brake denominated in it a brake that does not know what it is stopping.
// The split is carried rather than summed for that reason, and it is carried
// onto the record because a charge written as one integer can never be
// re-priced afterwards.
type Spend struct {
	Input      int64 `json:"input,omitempty"`
	Output     int64 `json:"output,omitempty"`
	CacheRead  int64 `json:"cache_read,omitempty"`
	CacheWrite int64 `json:"cache_write,omitempty"`
	// Total is the run's own tally over the parts when it gave one, and wins
	// over their sum: a total the agent stands behind is a stronger claim
	// than a sum of the components this factory happens to recognise.
	Total int64 `json:"tokens,omitempty"`
	// CostMicros is what the run said it cost, in millionths of a dollar.
	//
	// Integer, and converted once at the seam where the agent's float
	// arrives. Money that accumulates over a window must not drift, and a
	// budget an operator cannot reproduce by adding up the charges is not one
	// they can audit.
	CostMicros int64 `json:"cost_micros,omitempty"`
	// Reported is false when nothing said what the run cost at all. Absence
	// is carried rather than collapsing into a zero total; see [ErrUnmetered].
	Reported bool `json:"metered"`
	// Limit is the account's own limit as the run reported it, when it did.
	// It rides the charge record so a restart restores it with the rest.
	Limit *Limit `json:"limit,omitempty"`
	// Aborted is true when the run's own transport dropped the call after it
	// had sent its prompt — see acp.ErrAborted. It must live here rather than
	// on the Charge call, because the charge record's body IS this Spend
	// verbatim and [Meter.Restore] refolds the window from those records: a
	// flag anywhere else would silently read back as an ordinary unmetered
	// run on the next boxd restart, which is the exact failure this issue
	// (#69) exists to close. An aborted run that never reported (Reported
	// false) is a KNOWN zero — nothing was answered, so nothing was spent
	// that could be priced — and does not raise [Meter.unmetered]; an aborted
	// run that DID report (Reported true) accrues exactly like any other
	// spend, because the box knows what that attempt cost even though it did
	// not finish.
	Aborted bool `json:"aborted,omitempty"`
}

// Limit is the provider account's rate-limit state as an agent reported it:
// not this meter's window but the account's — a subscription's five-hour or
// seven-day allowance, shared with whoever else is using the account and
// invisible to the meter until a run says. The one number the factory's own
// accounting cannot produce, and the one that actually bound in round 4.
type Limit struct {
	// Status is allowed, allowed_warning, or rejected.
	Status string `json:"status"`
	// Window names the limit: five_hour, seven_day, and the model-specific
	// weekly ones.
	Window string `json:"window"`
	// Utilization is how much of the window is used, 0 to 1.
	Utilization float64 `json:"utilization"`
	// ResetsAt is when the window turns, in unix seconds.
	ResetsAt int64 `json:"resets_at"`
}

// Spent reports whether the account's window should refuse new starts at
// this moment under a reserve: rejected outright, or at or above the reserve,
// and not yet reset. A zero reserve refuses only a rejection.
func (l *Limit) Spent(now time.Time, reserve float64) bool {
	if l == nil || !now.Before(time.Unix(l.ResetsAt, 0)) {
		return false
	}
	return l.Status == "rejected" || (reserve > 0 && l.Utilization >= reserve)
}

// Tokens is every token the run is billed for, cache reads included —
// cheaper, never free.
func (s Spend) Tokens() int64 {
	if s.Total > 0 {
		return s.Total
	}
	return s.Input + s.Output + s.CacheRead + s.CacheWrite
}

// Priced reports the run having said what it cost in money. A metered run
// that is not priced is the [ErrUnpriced] case: known in tokens, unknown in
// the currency a cost ceiling is written in.
func (s Spend) Priced() bool { return s.CostMicros > 0 }

// CredentialKind names which account a run spent against: a subscription
// whose window this meter cannot produce on its own, or a metered API key
// billed by the token. The empty value means neither was said, which is a
// third state and not a synonym for either — a box that has not yet heard
// from a run, and a box a mismatch has already closed, both read as this
// same absence rather than defaulting to whichever kind happens to be
// convenient to assume.
type CredentialKind string

const (
	// CredentialPlan is a subscription: the run reported the account's own
	// rate-limit window.
	CredentialPlan CredentialKind = "plan"
	// CredentialKey is a metered API key: the run reported cost with no
	// account window behind it.
	CredentialKey CredentialKind = "key"
	// CredentialUnknown is neither — nothing on the run said. The zero
	// value, so an absent field decodes to this rather than to a kind.
	CredentialUnknown CredentialKind = ""
)

// Credential derives which account this spend was made against.
//
// A reported account window wins over a reported price: the issue that
// motivated this method measured a subscription run that also carried a
// nonzero cost estimate, and the window is the fact a key can never produce,
// so it is the stronger claim. Neither present is [CredentialUnknown] — not
// guessed as whichever kind the budget happens to name.
func (s Spend) Credential() CredentialKind {
	switch {
	case s.Limit != nil:
		return CredentialPlan
	case s.Priced():
		return CredentialKey
	default:
		return CredentialUnknown
	}
}

// USD renders micro-dollars as dollars, for display only — never for
// accumulation.
func USD(micros int64) float64 { return float64(micros) / 1e6 }

// Micros converts a dollar figure to micro-dollars, rounding to the nearest.
// The one place a float becomes money, so it is the one place the rounding
// rule is written.
func Micros(usd float64) int64 { return int64(math.Round(usd * 1e6)) }

// Budget bounds a tenant's execution.
type Budget struct {
	// MaxConcurrent bounds simultaneous runs. Zero means one — an unset
	// budget must never mean an unbounded one, same rule as rollouts.
	MaxConcurrent int
	// MaxPerWindow bounds run STARTS per window; zero means unlimited
	// starts (concurrency still binds).
	MaxPerWindow int
	// MaxTokensPerWindow bounds what the window may spend in TOKENS; zero
	// means unbounded.
	//
	// Counting starts is not counting cost. A start is one number whether the
	// run behind it reads three files or the whole repository, so a budget
	// denominated in starts cannot tell a cheap factory from an expensive one
	// and cannot stop the expensive one.
	//
	// This is a volume ceiling and not the invoice, which it was once
	// described as and never was: a token is not a unit of money, and a
	// window of cached reads and a window of output tokens priced tens of
	// times apart both count the same here. It still bounds the thing volume
	// is the right unit for — how much material the factory pushes through a
	// provider in an hour. For the invoice, see MaxCostPerWindow.
	MaxTokensPerWindow int64
	// MaxCostPerWindow bounds what the window may spend in MONEY, in
	// millionths of a dollar; zero means unbounded.
	//
	// This is the brake denominated the way the bill is. It is fed by what
	// each run says it cost rather than by a price table here, because the
	// rate card is the provider's and a copy of it in this repository would
	// be wrong on the morning it changed with nothing to say so.
	//
	// The consequence is stated rather than implied: under a cost ceiling, a
	// run that reports tokens and no cost closes the window exactly as an
	// unmetered one does. See [ErrUnpriced].
	MaxCostPerWindow int64
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
	// WindowReserve refuses new starts while the ACCOUNT's own window — the
	// subscription's, as the last run reported it — is at or above this
	// utilization, until it resets; zero means only a rejected status
	// refuses. This is the brake denominated in the currency that bound in
	// round 4: one run consumed the account's five-hour window and the
	// factory's meter, counting its own tokens, refused nothing. Set it to
	// the share of the window the human on the same account keeps for
	// themselves.
	//
	// It reads the last report, so it binds only as tightly as runs report:
	// the human's own use between runs is invisible until the next run says.
	WindowReserve float64
	// Credential names which account this box runs on: [CredentialPlan] for
	// a subscription, [CredentialKey] for a metered API key.
	// [CredentialUnknown] (the zero value) means the box has not said, which
	// is refused rather than assumed the moment WindowReserve or
	// MaxCostPerWindow asks a brake to bind — an unnamed credential is a
	// brake that does not know what it is protecting, the same rule
	// AllowUnmetered's default states for spend it cannot see at all. A run
	// that reports the OTHER kind than the one named here also refuses,
	// once its own spend is charged: see [ErrCredential].
	Credential CredentialKind
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
	cost        int64 // micro-dollars charged this window
	split       Spend // the same tokens, by kind — what makes the total priceable
	unmetered   int   // runs this window that finished without saying what they cost
	unpriced    int   // runs this window that reported tokens and no money
	aborted     int   // runs this window the transport dropped after the prompt was sent
	// mismatch is the kind a charge reported THIS window when it was a real
	// kind and not the one Budget.Credential names; CredentialUnknown means
	// no mismatch yet. Window-scoped like unmetered and unpriced, so a
	// mismatching run closes only the window it happened in — the next
	// window is a clean read on whether the box is still misconfigured.
	mismatch CredentialKind
	// limit is the account's window as the last run reported it. Not reset
	// with the meter's window: the account's clock is its own, and the report
	// says when it turns.
	limit *Limit
	// lastCredential is the kind the last charge reported, kept across
	// window resets the same way limit is: it is the account's own identity,
	// not a per-window tally.
	lastCredential CredentialKind
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
			// Outside the meter's window, but the account's window is its
			// own clock: the last report still says whether it has reset —
			// and so does the credential it spent against, which is why
			// lastCredential is read here too rather than only from charges
			// inside the window.
			if rec.Kind == "charge" {
				var s Spend
				if json.Unmarshal(rec.Body, &s) == nil {
					if s.Limit != nil {
						m.limit = s.Limit
					}
					if k := s.Credential(); k != CredentialUnknown {
						m.lastCredential = k
					}
				}
			}
			continue
		}
		switch rec.Kind {
		case "spend":
			m.started++
		case "charge":
			// The charge body IS a Spend, so the record and the in-memory
			// account cannot drift into two shapes. A record written before
			// the split existed carries `tokens` and `metered` and nothing
			// else, and still restores its total correctly — the components
			// are simply absent, which is the honest reading of a charge that
			// never recorded them.
			var s Spend
			if json.Unmarshal(rec.Body, &s) != nil {
				// A charge that will not parse is spend we cannot account
				// for, which is exactly what unmetered means.
				m.unmetered++
				continue
			}
			m.accrue(s)
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

// Acquire takes one execution slot, returning its release. The first refusal
// of each window is evidence; a start is not, here — a claim can still be
// sent back to Wait before it ever builds, so Acquire checks MaxPerWindow
// against starts already on the books but leaves recording a new one, and its
// evidence record, to [Meter.Start] once the run actually begins.
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
	// Unpriced before over-cost, for the reason unmetered comes before
	// over-tokens: a window holding a run whose money nobody reported has no
	// trustworthy total in dollars either.
	if m.unpriced > 0 && !m.Budget.AllowUnmetered {
		first := !m.refused
		m.refused = true
		unpriced := m.unpriced
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what,
				fmt.Sprintf(`{"reason":"unpriced","unpriced_runs":%d,"window":%q}`, unpriced, m.Budget.Window))
		}
		return nil, ErrUnpriced
	}
	// Credential mismatch: a charge this window reported the OTHER real kind
	// than the one this box named. Spend was already accrued in accrue —
	// spend is spend — so only new starts refuse here.
	if m.mismatch != CredentialUnknown {
		first := !m.refused
		m.refused = true
		named, reported := m.Budget.Credential, m.mismatch
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what,
				fmt.Sprintf(`{"reason":"credential","named":%q,"reported":%q}`, named, reported))
		}
		return nil, ErrCredential
	}
	// A brake denominated in an account only means something if the box says
	// which account: WindowReserve is the subscription's window and
	// MaxCostPerWindow is the key's invoice, and neither is meaningful
	// pointed at nothing.
	if m.Budget.Credential == CredentialUnknown && (m.Budget.WindowReserve > 0 || m.Budget.MaxCostPerWindow > 0) {
		first := !m.refused
		m.refused = true
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what, `{"reason":"credential-unsaid"}`)
		}
		return nil, ErrCredential
	}
	if m.limit.Spent(m.now(), m.Budget.WindowReserve) {
		first := !m.refused
		m.refused = true
		l := *m.limit
		m.mu.Unlock()
		if first {
			m.record(ctx, "budget-refused", what,
				fmt.Sprintf(`{"reason":"account-window","status":%q,"account_window":%q,"utilization":%.2f,"resets_at":%d,"reserve":%.2f}`,
					l.Status, l.Window, l.Utilization, l.ResetsAt, m.Budget.WindowReserve))
		}
		return nil, fmt.Errorf("%w: the account's %s window is %s at %.0f%% (reserve %.0f%%), resets %s",
			ErrBudget, l.Window, l.Status, l.Utilization*100, m.Budget.WindowReserve*100, time.Unix(l.ResetsAt, 0).Format(time.RFC3339))
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
	if m.Budget.MaxCostPerWindow > 0 {
		if m.cost >= m.Budget.MaxCostPerWindow {
			first := !m.refused
			m.refused = true
			cost := m.cost
			m.mu.Unlock()
			if first {
				m.record(ctx, "budget-refused", what,
					fmt.Sprintf(`{"reason":"cost","cost_micros":%d,"max_cost_per_window":%d,"window":%q}`,
						cost, m.Budget.MaxCostPerWindow, m.Budget.Window))
			}
			return nil, ErrBudget
		}
	}
	m.running++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.running--
		m.mu.Unlock()
	}, nil
}

// Start records that a run Acquire already admitted has actually begun
// building, as distinct from having merely claimed a concurrency slot.
//
// A claim can still lose the section lease and go back to Wait before the
// builder ever runs; charging the start at Acquire counted that claim twice —
// once for the wait that went nowhere and again for the run that later
// carried it to completion — while a run that stayed in Wait forever spent a
// start it never used. Start is the one point that has both facts: the slot
// was granted, and the agent is now building. It never refuses and never
// checks a budget; MaxPerWindow already checked what it needed to at Acquire,
// against the started count as of that moment.
func (m *Meter) Start(ctx context.Context, what string) {
	m.mu.Lock()
	if w := m.windowAt(m.now()); !w.Equal(m.windowStart) {
		m.resetWindow(w)
	}
	m.started++
	running, started := m.running, m.started
	m.mu.Unlock()
	m.record(ctx, "spend", what, fmt.Sprintf(`{"running":%d,"started_this_window":%d}`, running, started))
}

// Charge records what a finished run actually cost. Only the agent stream
// knows: the loop sees a commit and the pump sees a slot, so the number has
// to travel back from the adapter rather than being inferred here.
//
// An unreported spend is recorded as its own fact — never as a zero — and
// under a token budget it closes the window to new claims; see
// [ErrUnmetered]. A spend reported in tokens but not in money does the same
// under a cost budget; see [ErrUnpriced].
func (m *Meter) Charge(ctx context.Context, what string, s Spend) {
	m.mu.Lock()
	// Roll the window before accruing, the way Acquire and Report already do.
	// Without this a charge lands in whatever window the meter last touched,
	// and the next reader rolls it forward and zeroes the account — so a run
	// that STARTED at 11:59 and reported at 12:01 had its whole cost silently
	// discarded from memory while the chain recorded it correctly, leaving
	// Restore and Report permanently disagreeing. It went unseen because
	// Acquire happens to roll the window a moment before every charge in the
	// ordinary case, and the case it does not cover is the long run: the
	// expensive one.
	if w := m.windowAt(m.now()); !w.Equal(m.windowStart) {
		m.resetWindow(w)
	}
	m.accrue(s)
	// `tokens` on the record is what this charge was ACCOUNTED at, never the
	// agent's tally as distinct from the sum of its parts. The distinction
	// matters in memory, where a tally the agent stands behind beats a sum of
	// the components this runtime recognises; it must not reach the record,
	// where the same key already means the charged total on every row written
	// before the split existed. A run reporting parts and no tally would
	// otherwise record `tokens` absent — zero to every later reader.
	charged := s
	charged.Total = s.Tokens()
	window := struct {
		Spend
		SpentThisWindow     int64 `json:"spent_this_window"`
		CostThisWindow      int64 `json:"cost_micros_this_window"`
		UnmeteredThisWindow int   `json:"unmetered_this_window"`
		UnpricedThisWindow  int   `json:"unpriced_this_window"`
	}{charged, m.spent, m.cost, m.unmetered, m.unpriced}
	m.mu.Unlock()
	body, err := json.Marshal(window)
	if err != nil {
		// A charge that will not serialize is a charge the window cannot be
		// rebuilt from, which is what unmetered means on the way back in.
		body = []byte(fmt.Sprintf(`{"metered":false,"error":%q}`, err.Error()))
	}
	m.record(ctx, "charge", what, string(body))
}

// accrue folds one spend into the window's account. Callers hold mu.
//
// Unpriced is only counted when a cost ceiling is set. Without one, money the
// agent never mentioned is not a gap in an account nobody is keeping — and
// counting it anyway would turn on a refusal for operators who never asked to
// budget in dollars.
func (m *Meter) accrue(s Spend) {
	if s.Limit != nil {
		// Kept whether or not the run was metered: a session that died at
		// the account's limit is exactly the run whose report matters most.
		m.limit = s.Limit
	}
	if k := s.Credential(); k != CredentialUnknown {
		m.lastCredential = k
		if m.Budget.Credential != CredentialUnknown && k != m.Budget.Credential {
			m.mismatch = k
		}
	}
	if s.Aborted {
		m.aborted++
	}
	if !s.Reported {
		if !s.Aborted {
			m.unmetered++
		}
		// An aborted run that never reported spent a known nothing — the
		// transport dropped the call before anything came back to price —
		// and stops here without touching spent, cost, or the split: there
		// is nothing to accrue, and it must not raise unmetered either.
		return
	}
	m.spent += s.Tokens()
	m.cost += s.CostMicros
	m.split.Input += s.Input
	m.split.Output += s.Output
	m.split.CacheRead += s.CacheRead
	m.split.CacheWrite += s.CacheWrite
	if m.Budget.MaxCostPerWindow > 0 && !s.Priced() {
		m.unpriced++
	}
}

// Report is the window's account as an operator reads it.
//
// Unmetered travels beside the total on purpose: the total alone is a lie
// whenever that number is not zero, and a spend figure shown without it would
// be the most reassuring thing on the screen and the least true.
type Report struct {
	Tokens int64 `json:"tokens"`
	// Input, Output, CacheRead and CacheWrite are the same tokens by kind:
	// the total says how much went through, and only the split says what it
	// was worth. A window that is 90% cache reads and one that is 90% output
	// read alike on Tokens alone and differ on the bill by an order of
	// magnitude.
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	// CostMicros is the window's spend in millionths of a dollar, as the runs
	// themselves reported it. Unpriced travels beside it for the reason
	// Unmetered travels beside Tokens.
	CostMicros    int64 `json:"cost_micros"`
	MaxCostMicros int64 `json:"max_cost_micros"`
	Unpriced      int   `json:"unpriced"`
	Unmetered     int   `json:"unmetered"`
	// Aborted is how many runs this window the transport dropped after the
	// prompt was sent, reported or not — an operator's count of a distinct
	// symptom (a flaky call, a dying child) from Unmetered's (nobody said
	// what a finished run cost).
	Aborted       int           `json:"aborted"`
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
	// AccountWindow is the account's own window as the last run reported it,
	// and AccountReserve the utilization above which the meter refuses.
	AccountWindow  *Limit  `json:"account_window,omitempty"`
	AccountReserve float64 `json:"account_reserve,omitempty"`
	// Credential is the account this box NAMED, and LastCredential the kind
	// the last charged run actually REPORTED. Kept apart on purpose: a box
	// that names plan and receives a key run is the exact mismatch
	// [ErrCredential] exists to catch, and folding the two into one field
	// would erase the evidence that a mismatch happened at all. Either is
	// omitted, never rendered as a zero-ish value, when nothing has said —
	// an absent credential must not read as "key" or "plan" to a later
	// reader any more than an absent Spend reads as free.
	Credential     CredentialKind `json:"credential,omitempty"`
	LastCredential CredentialKind `json:"last_credential,omitempty"`
}

// Window returns a copy of the account window this meter last heard about
// from a run's report, nil when no run ever has. The meter's first exported
// READ of the account window (issue #112): a mid-run refusal that names no
// reset of its own (a run refused at session or prompt start, before any
// usage update) resolves one from here instead, because the meter is the one
// place the account's own window survives across runs and restarts (see
// [Meter.Restore]).
//
// Unlike [Meter.Report], this does not roll the window forward first: the
// account's clock is its own, kept apart from the meter's window exactly as
// [Meter.limit] is, and a stale copy of what the account last reported is
// still the best guess there is for it — it is never invented.
func (m *Meter) Window() *Limit {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limit == nil {
		return nil
	}
	cp := *m.limit
	return &cp
}

// Report snapshots the window.
//
// It rolls the window forward first. Reading is not a side-effect-free act
// here: the counters belong to whichever window the clock is in, and a report
// that answered from a window the clock has left would show yesterday's spend
// until something happened to claim.
func (m *Meter) Report() Report {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w := m.windowAt(m.now()); !w.Equal(m.windowStart) {
		m.resetWindow(w)
	}
	maxC := m.Budget.MaxConcurrent
	if maxC <= 0 {
		maxC = 1
	}
	return Report{
		Tokens: m.spent, Unmetered: m.unmetered, Aborted: m.aborted, MaxTokens: m.Budget.MaxTokensPerWindow,
		Input: m.split.Input, Output: m.split.Output,
		CacheRead: m.split.CacheRead, CacheWrite: m.split.CacheWrite,
		CostMicros: m.cost, MaxCostMicros: m.Budget.MaxCostPerWindow, Unpriced: m.unpriced,
		Running: m.running, Started: m.started,
		MaxConcurrent: maxC, MaxPerWindow: m.Budget.MaxPerWindow,
		Window: m.Budget.Window, WindowSec: int64(m.Budget.Window.Seconds()),
		Refusing:      m.wouldRefuse(),
		AccountWindow: m.limit, AccountReserve: m.Budget.WindowReserve,
		Credential: m.Budget.Credential, LastCredential: m.lastCredential,
	}
}

// wouldRefuse reports whether a claim asked for right now would be turned
// away on budget grounds. Callers hold mu.
//
// Distinct from m.refused, which answers "have I already written the refusal
// record for this window" and exists so one refusal is evidence and the next
// thousand polls are not. Reporting that field as the room's state conflated
// the two, and the difference showed the moment the ceiling was raised: the
// budget was lifted, a run started, and the room went on saying "refusing"
// with work visibly in flight, because something had refused an hour earlier.
func (m *Meter) wouldRefuse() bool {
	switch {
	case m.Budget.MaxPerWindow > 0 && m.started >= m.Budget.MaxPerWindow:
		return true
	case m.unmetered > 0 && !m.Budget.AllowUnmetered:
		return true
	case m.unpriced > 0 && !m.Budget.AllowUnmetered:
		return true
	case m.mismatch != CredentialUnknown:
		return true
	case m.Budget.Credential == CredentialUnknown && (m.Budget.WindowReserve > 0 || m.Budget.MaxCostPerWindow > 0):
		return true
	case m.limit.Spent(m.now(), m.Budget.WindowReserve):
		return true
	case m.Budget.MaxTokensPerWindow > 0 && m.spent >= m.Budget.MaxTokensPerWindow:
		return true
	case m.Budget.MaxCostPerWindow > 0 && m.cost >= m.Budget.MaxCostPerWindow:
		return true
	}
	return false
}

// resetWindow starts a fresh accounting window at its aligned boundary.
// Callers hold mu.
func (m *Meter) resetWindow(start time.Time) {
	m.windowStart, m.started, m.refused = start, 0, false
	m.spent, m.cost, m.split = 0, 0, Spend{}
	m.unmetered, m.unpriced, m.aborted = 0, 0, 0
	m.mismatch = CredentialUnknown
}

func (m *Meter) record(ctx context.Context, kind, subject, body string) {
	m.Store.Log().Append(ctx, evidence.Record{
		Tenant: m.Tenant, Scope: "control/capacity", Kind: kind, Subject: subject,
		Actor: m.Actor, Body: []byte(body),
	})
}
