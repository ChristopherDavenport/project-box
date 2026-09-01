// Package evidence records what the factory did, in a form an external auditor
// can test.
//
// Three properties matter, and they come from different places. The hash chain
// carries ordering and no-omission: each record names the previous record in
// its scope, so removing or reordering anything breaks the chain. Attribution
// comes from authentication — the control plane records the subject it
// authenticated, in the same way CloudTrail-style logging does. Retention
// comes from exporting to storage that is immutable by the people being
// audited — a retention-locked bucket — because a database the operator can
// write is not evidence of anything to someone auditing that operator.
//
// Git already provides ordering for content changes, since every commit names
// its parent. This package covers what git does not record:
// gate verdicts, lease grants, deployments, knowledge assertions, break-glass
// invocations, and reviewer sign-off.
//
// Volume is handled without surrendering any of that. Compact replaces a
// scope's oldest records with one summary record that keeps the chain
// continuous — it takes the pruned head's seat and names its hash — while the
// full run stays in WORM export. Pruning a tamper-evident chain is otherwise
// indistinguishable from the attack Verify exists to catch, so the seam is
// part of the schema: Verify proves the hot side across it, and VerifySeam
// proves the exported side, both from bytes alone.
package evidence

import (
	"context"
	"iter"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Record is one authenticated, chained fact.
type Record struct {
	// ID is the deterministic hash over the record's canonical serialization,
	// and the value the next record in the scope names as its Prev.
	ID string
	// Tenant owns this record. Chains never span tenants, so one tenant's
	// auditor can be handed a complete, verifiable chain that contains nothing
	// of anyone else's — and retention, export, and deletion are per-tenant
	// operations rather than filtered ones.
	Tenant tenant.ID
	// Scope names the chain this record belongs to, within the tenant. Chains are per-scope so
	// that a busy Box does not serialize behind an idle one; the cost is that
	// ordering is only proven within a scope.
	Scope string
	// Seq is this record's position in its scope, starting at 1.
	Seq uint64
	// Prev is the ID of the preceding record in the scope, empty only for the
	// first. This field is what makes the log tamper-evident rather than
	// merely tamper-logged.
	Prev string
	// Kind is what happened: a gate verdict, a lease grant, a landing, a
	// deployment, a break-glass invocation.
	Kind string
	// Subject is the thing the record is about — usually a commit sha, else a
	// run or a lease ref. Auditors sample changes and demand complete evidence
	// for each, so this is the field the audit query runs on.
	Subject string
	// Actor is the authenticated subject that took the action.
	Actor string
	// OnBehalfOf is the subject whose authority was exercised, empty when the
	// actor acted on its own. Both are recorded because an audit needs to
	// separate a change a person made from one an automation made for them,
	// and a log that never drew that line cannot have it recovered later.
	OnBehalfOf string
	// Grant identifies the authorization that permitted the action, so the
	// record shows why it was allowed rather than only that it happened.
	Grant string
	Body  []byte
	At    time.Time
	// Trace is the W3C trace id this record was produced under, joining the
	// evidence chain to the telemetry that explains it. The two are separate
	// systems on purpose — telemetry is sampled and expires, evidence is
	// complete and retained — so this is a correlation id, never a dependency.
	// An empty value is normal: telemetry may be disabled, and evidence must
	// not be.
	Trace string
}

// Log is an append-only chained record store.
//
// Implementations must reject any append that would break the chain, and must
// never expose an update or a free delete: a correction is a new record that
// supersedes an earlier one, never an edit to it, and the only prune is
// Compact, which leaves a verifiable seam behind.
type Log interface {
	// Append seals a record into its scope, assigning Seq, Prev and ID. It
	// returns the sealed record. Concurrent appends to one scope serialize;
	// appends to different scopes do not contend.
	Append(ctx context.Context, r Record) (Record, error)
	// Head returns the newest record in a scope, or a zero Record if the scope
	// is empty.
	Head(ctx context.Context, t tenant.ID, scope string) (Record, error)
	// Since walks a scope forward from a sequence number. The error half of
	// each pair reports a failure mid-walk, which a bare sequence could only
	// swallow.
	Since(ctx context.Context, t tenant.ID, scope string, seq uint64) iter.Seq2[Record, error]
	// BySubject returns every record about one subject, across scopes. This is
	// the auditor's query: given a sampled commit, produce everything that
	// happened to it. The query spans scopes but never tenants: chains are
	// per-tenant, and so is an audit.
	BySubject(ctx context.Context, t tenant.ID, subject string) iter.Seq2[Record, error]
	// Verify walks a scope and reports the first record whose chain link does
	// not hold. A compacted scope verifies across its seam: the walk may begin
	// at a summary record, whose internal consistency is checked along with
	// every link. A nil error means the scope is intact as
	// stored — it says nothing about records never written, which is what
	// export and anchoring are for, and nothing about the pruned run, which is
	// what VerifySeam proves against the WORM export.
	Verify(ctx context.Context, t tenant.ID, scope string) error
	// Compact replaces every record of the scope with sequence at most
	// through by one summary record seated at the pruned head: same Seq, and
	// Prev naming the head's ID — the hash the surviving successor already
	// names, so the chain stays continuous across the seam. exp is the WORM
	// receipt for exactly the pruned run and is validated against the store's
	// own rows before anything is deleted: no checkpoint, no prune. A prefix
	// that does not verify is refused rather than flattened, actor is
	// recorded because a prune is an action like any other, and a prior
	// summary is swallowed into the new one — a scope holds at most one, at
	// its start.
	Compact(ctx context.Context, t tenant.ID, scope string, through uint64, exp Export, actor string) (Record, error)
}

// Sink writes a sealed range of a chain somewhere it cannot later be altered.
//
// Locally that is a directory, which proves the shape but not the property.
// Deployed it is a retention-locked bucket, and the lock is the control an
// auditor actually tests.
type Sink interface {
	Export(ctx context.Context, t tenant.ID, scope string, from, to uint64) (Export, error)
}

// Export records one write to a Sink, so the export itself is auditable. An
// export is verifiable offline: each record's ID is recomputable from its
// canonical serialization and every Prev link is checkable from the bytes
// alone, with no service running — Head ties the object to the chain, and
// Digest ties it to itself. The JSON form is part of the summary record's
// body, and therefore of what Seal hashes.
type Export struct {
	Tenant tenant.ID `json:"tenant"`
	Scope  string    `json:"scope"`
	From   uint64    `json:"from"`
	To     uint64    `json:"to"`
	// Head is the ID of the last record covered, tying the export to the chain
	// it came from.
	Head string `json:"head"`
	// URI locates the exported object.
	URI string `json:"uri"`
	// Digest is the hash of the exported bytes.
	Digest string `json:"digest"`
	// RetainUntil is when the object's retention lock expires. A zero value
	// means the sink offers no retention guarantee, which is the local case.
	RetainUntil time.Time `json:"retain_until"`
	At          time.Time `json:"at"`
}
