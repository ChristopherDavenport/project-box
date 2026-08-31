// Package store is the backend seam: the one interface a storage backend
// implements, and nothing else the rest of the system may depend on.
//
// The contract is the property, never the mechanism. A backend promises
// linearizable exclusive claims, strictly-increasing never-reused fencing
// tokens, append-only chaining, and tenant non-interference — how it delivers
// them is its own business, and callers must not be able to tell SQLite from
// Spanner through this interface. The conformance suite in
// internal/store/conformance is the enforcement: one suite, every backend,
// asserting the properties rather than the implementations.
//
// Two backends exist from the first commit, deliberately. An interface with
// one implementation is a wrapper, not an abstraction — with only SQLite its
// semantics would silently become the definition, and the mismatch would
// surface after everything had been built on them.
package store

import (
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// Store is tenant-scoped access to the control plane's state.
//
// Lease and evidence state live in one store on purpose: a lease grant and the
// record of it commit together, rather than the second being a publish that can
// fail and leave the audit trail lying.
type Store interface {
	// Leaser returns the lease allocator as seen by one runner. The runner
	// name is bound at construction rather than passed per call because a
	// process is one runner: letting each call claim a different identity
	// would make the observability fields on a lease unreliable exactly when
	// they matter, during an incident.
	Leaser(runner string) forge.Leaser
	// Log returns the evidence log.
	//
	// Append stamps [evidence.Record.At] from the store's clock when the
	// caller leaves it zero, and preserves it otherwise; either way the stored
	// value round-trips exactly, because the timestamp participates in the
	// record's ID.
	Log() evidence.Log
	// Close releases the backend's resources.
	Close() error
}
