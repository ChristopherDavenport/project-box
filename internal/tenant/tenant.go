// Package tenant carries the outermost scope in the system.
//
// Multi-tenancy is structural here rather than a column added to tables. The
// familiar failure is a `tenant_id` retrofitted onto an existing schema, where
// isolation then depends on every query remembering to filter and one that
// forgets is a cross-tenant leak that no type or test catches. Nothing in this
// design is harder to add later, so it is present from the first commit.
//
// # The doctrine
//
// Three rules, and the first two are what make the third testable.
//
//  1. **Tenant leads every primary key.** In Spanner this is isolation and
//     locality at once: a tenant's rows occupy a contiguous key range, so one
//     busy tenant cannot hotspot another, and a query that crosses tenants is
//     obviously wrong at the schema rather than subtly wrong in a predicate.
//
//  2. **An unscoped handle does not exist.** Storage is reached only through a
//     handle obtained by naming a tenant, so a caller cannot forget the scope —
//     there is no API through which to forget it. Never derive the tenant by
//     looking up a Box or a repository: an identifier that has to be resolved
//     before it can be trusted is an identifier an attacker can supply.
//
//  3. **Non-interference is asserted, not assumed.** The conformance suite
//     writes as one tenant and reads as another for every operation, and
//     expects nothing back. Buzz specifies the same property formally as
//     `Inv_NonInterference` in `MultiTenantRelay.tla`; this is the mechanized
//     version of the same claim.
//
// # What is deferred
//
// Single-tenant operation is multi-tenant with one tenant, so the rules above
// cost a constant field and nothing else. What genuinely adds complexity —
// tenant lifecycle, per-tenant quotas, isolation stronger than logical — is
// deferred, and [Router] is the seam that keeps it deferrable.
package tenant

import (
	"context"
	"errors"
	"regexp"
)

// ID identifies a tenant. It is the outermost scope of every key, every
// evidence chain, and every authority grant.
type ID string

// ErrInvalid reports an ID that cannot be used as a key prefix.
var ErrInvalid = errors.New("tenant: invalid id")

var idPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Valid reports whether the ID is well-formed: 1–63 characters of lowercase
// letters, digits, and hyphens, starting and ending alphanumeric — the
// DNS-label shape, chosen because IDs are key prefixes and path components,
// not display names. A tenant's human name lives elsewhere and may change
// without moving a single row.
func (t ID) Valid() bool { return idPattern.MatchString(string(t)) }

// Validate returns [ErrInvalid] if the ID is not well-formed.
func (t ID) Validate() error {
	if !t.Valid() {
		return ErrInvalid
	}
	return nil
}

// Isolation is how strongly a tenant is separated from its neighbours.
//
// The levels are a migration path, not a menu to agonize over up front. Start
// every tenant Logical; move the ones that need more, when they need it.
type Isolation string

const (
	// IsolationLogical shares a database, separated by key prefix. Isolation
	// rests on the doctrine above holding, which is why it is asserted by the
	// conformance suite rather than trusted.
	IsolationLogical Isolation = "logical"
	// IsolationDatabase gives a tenant its own database in a shared instance.
	// Stronger separation, and the level at which a customer-managed
	// encryption key becomes possible.
	IsolationDatabase Isolation = "database"
	// IsolationInstance gives a tenant its own instance. Full separation, full
	// cost.
	IsolationInstance Isolation = "instance"
)

// Tenant is the owner of Boxes, agents, and evidence.
type Tenant struct {
	ID        ID
	Name      string
	Isolation Isolation
	// Owner is the subject accountable for this tenant. Grants within the
	// tenant root here, and no principal of another tenant can hold one.
	Owner string
}

// Router resolves a tenant to the storage backing it.
//
// This exists so that raising a tenant's [Isolation] is a routing change rather
// than a rewrite. Where a tenant's handle points — a shared database, its own,
// its own instance — is decided here and nowhere else, so callers never learn
// which and never depend on it.
type Router interface {
	// Route returns the storage scope for a tenant. It is the only way to
	// obtain one: rule 2 of the doctrine is enforced by there being no
	// alternative constructor.
	Route(ctx context.Context, id ID) (Scope, error)
}

// Scope is tenant-bound access to storage.
//
// Implementations bind the tenant at construction and apply it to every
// operation. A Scope for one tenant must be incapable of naming another's data
// — not by checking, but by having nowhere to put the other tenant's id.
type Scope interface {
	// Tenant reports which tenant this scope is bound to.
	Tenant() ID
}
