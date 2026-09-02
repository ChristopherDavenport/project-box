package forge

import (
	"context"
	"errors"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Errors returned by a [Leaser].
var (
	// ErrHeld reports that another run holds the requested ref.
	ErrHeld = errors.New("forge: ref already leased")
	// ErrExpired reports that a lease's deadline passed before the operation.
	ErrExpired = errors.New("forge: lease expired")
	// ErrStaleToken reports that the presented fencing token has been
	// superseded, meaning the caller lost its lease and did not notice.
	ErrStaleToken = errors.New("forge: fencing token superseded")
)

// Token is a fencing token: a value that increases strictly monotonically
// across every lease ever granted over the same ref, whatever the outcome of
// those leases.
//
// A time-to-live alone cannot make a lease safe. A runner can stall past its
// deadline — a garbage-collection pause, a partition, a suspended VM — lose the
// lease to expiry, and resume still believing it holds one. Every mutating
// operation therefore presents its token, and the holder of authority rejects
// any token it has already superseded, so the stalled runner's write is refused
// rather than racing the new holder's.
//
// The contract is the property, never the mechanism, so that every backend can
// satisfy it its own way and none of them leaks into callers. Spanner takes the
// token from the grant's commit timestamp: grants over one ref serialize on that
// row and commit timestamps respect external consistency, so a later grant
// necessarily carries a greater token, with no counter to maintain and no clock
// to trust. SQLite uses a counter column, which is trivially correct there
// because it serializes writes anyway. Both satisfy the contract.
type Token uint64

// Lease is an exclusive claim over one ref of one repository, held by a single
// run for a bounded time.
type Lease struct {
	// Tenant leads the lease key, as it leads every key. A lease names a ref
	// within a tenant; two tenants holding the same repo and ref name contend
	// for nothing.
	Tenant tenant.ID
	Repo   string
	Ref    string
	// Runner identifies the machine holding the lease, for observability and
	// for reaping a runner known to be gone.
	Runner string
	Run    string
	Token  Token
	// Expires is when the claim lapses if not renewed. Compare against the
	// allocation store's clock, never the local one.
	Expires time.Time
}

// Expired reports whether the lease has lapsed as of now. It is advisory: a
// caller must still present its [Token], because only the [Leaser] can settle
// whether the claim survived.
func (l Lease) Expired(now time.Time) bool {
	return !now.Before(l.Expires)
}

// LeaseRequest asks for a lease over a ref.
type LeaseRequest struct {
	Tenant tenant.ID
	Repo   string
	Ref    string
	Run    string
	// TTL is how long the claim should hold before renewal is required. Keep
	// it short enough that a dead runner frees its refs promptly, and long
	// enough that a live one is not fighting the clock.
	TTL time.Duration
}

// Leaser allocates exclusive claims over (repo, ref) pairs across every runner
// in the control plane.
//
// Implementations must be linearizable. An eventually consistent store — one
// serving replica-routed reads, say — can hand two runners a stale "unheld"
// for the same ref and admit both, which is two agents in one working tree or
// two landings racing one branch. Because the evidence log shares the
// allocation store, a grant and the record of it commit together rather than
// the second being a publish that can fail.
type Leaser interface {
	// Acquire claims a ref, returning [ErrHeld] if a live claim already
	// exists. It does not block.
	Acquire(ctx context.Context, req LeaseRequest) (Lease, error)
	// Inspect reports the outstanding grant over a ref WITHOUT taking it:
	// which runner and run hold it, and when the hold lapses. ok is false
	// when the ref has never been leased or the last grant was released.
	//
	// It is the only read on this interface, and it exists for the control
	// room rather than for callers deciding whether they may act — those
	// still Acquire, because between an Inspect and an action the claim can
	// change and only the acquisition settles it. Note the deliberate
	// asymmetry with Acquire: a grant past its deadline that nobody has
	// superseded is reported here as an outstanding lease, because "runner-3
	// has held this for forty minutes on a twenty-minute lease" is precisely
	// the fact an operator needs, while Acquire correctly treats the same row
	// as free. Judge it with [Lease.Expired].
	Inspect(ctx context.Context, t tenant.ID, repo, ref string) (Lease, bool, error)
	// Renew extends a claim, returning a lease bearing the same token.
	// It reports [ErrExpired] or [ErrStaleToken] if the claim did not
	// survive, in which case the caller must abandon its workspace rather
	// than continue.
	Renew(ctx context.Context, l Lease, ttl time.Duration) (Lease, error)
	// Release drops a claim. Releasing an already-superseded lease is not an
	// error: the claim is gone either way, and the successor's token protects
	// it.
	Release(ctx context.Context, l Lease) error
	// Preempt claims a ref EVEN IF a live claim exists, superseding it. The
	// displaced holder's token is dead from this commit on: its renew fails,
	// its land is refused, and fencing makes the takeover safe with no new
	// mechanism — preemption and the stalled-runner case are the same thing.
	//
	// The store does not ask why. Whether a caller MAY preempt is an
	// authorization question — an open incident, per policy — decided in
	// authz and recorded in evidence, not here. A mechanical layer that
	// second-guessed authority would end up trusted with it.
	Preempt(ctx context.Context, req LeaseRequest) (Lease, error)
}
