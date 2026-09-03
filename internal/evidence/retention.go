package evidence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Retention says how much of a scope stays hot.
//
// A zero Retention compacts NOTHING, and that is the fail-safe reading rather
// than an oversight. Everywhere else in this system an unset policy resolves
// to the strict end because the permissive end is the dangerous one; here the
// dangerous end is the other way round — pruning is the destructive act, and
// an operator who has said nothing about retention has not asked for their
// evidence moved off the hot store. Unbounded growth is a problem that gets
// noticed; a default that quietly pruned would be a problem discovered by an
// auditor.
//
// Both bounds hold at once, and the stricter wins. Count alone would prune a
// quiet scope's whole history the moment it exceeded the number; age alone
// would leave a scope that took a million records in an hour entirely hot.
type Retention struct {
	// Keep is how many of the newest records stay in the hot store. Zero
	// disables compaction for the scope.
	Keep int
	// KeepFor is the age below which a record is never pruned, whatever Keep
	// says. Zero disables compaction for the scope.
	KeepFor time.Duration
	// MinPrune is the smallest run worth a seam; a compaction below it is
	// skipped. Zero means [DefaultMinPrune].
	//
	// It exists because a seam is not free: every summary is a record a
	// verifier must parse and an auditor must reason across, so compacting
	// three rows at a time would trade a slightly smaller store for a chain
	// that is mostly boundaries.
	MinPrune int
}

// DefaultMinPrune is the smallest run this will bother to compact.
const DefaultMinPrune = 100

func (r Retention) minPrune() int {
	if r.MinPrune <= 0 {
		return DefaultMinPrune
	}
	return r.MinPrune
}

// Enabled reports a policy that would ever prune anything.
func (r Retention) Enabled() bool { return r.Keep > 0 && r.KeepFor > 0 }

// Compactor moves a scope's cold prefix to a [Sink] and replaces it with one
// summary record, leaving a chain that still verifies across the seam.
//
// It is imperative Go on a timer, and it has to be: Gas Town's compactor was
// among the jobs whose failure they classed as unsurvivable, and a compactor
// that quietly stopped is a store that grows until it is an incident. Nothing
// here consults an agent.
type Compactor struct {
	Log  Log
	Sink Sink
	// Actor is recorded on every summary. A prune is an action like any other.
	Actor     string
	Retention Retention
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

func (c *Compactor) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Sweep compacts each named scope that has grown past its retention, and
// returns the summaries it wrote.
//
// One scope's failure does not stop the others, for the reason the pump
// already collects rather than aborts: a single unreadable chain must not be
// a gate across every other scope's housekeeping. The errors come back joined
// so the failure is still loud.
func (c *Compactor) Sweep(ctx context.Context, t tenant.ID, scopes ...string) ([]Record, error) {
	if !c.Retention.Enabled() {
		return nil, nil
	}
	var out []Record
	var errs []error
	for _, scope := range scopes {
		sum, ok, err := c.CompactScope(ctx, t, scope)
		if err != nil {
			errs = append(errs, fmt.Errorf("evidence: compacting %s/%s: %w", t, scope, err))
			continue
		}
		if ok {
			out = append(out, sum)
		}
	}
	return out, errors.Join(errs...)
}

// CompactScope compacts one scope if its retention says to, reporting whether
// it did. It exports first and prunes second: the pruned run must be durable
// before it stops being reachable, and a failure between the two leaves an
// orphan object rather than a hole in the chain — which is the right way for
// this to fail, because an extra copy is recoverable and a gap is not.
func (c *Compactor) CompactScope(ctx context.Context, t tenant.ID, scope string) (Record, bool, error) {
	if !c.Retention.Enabled() {
		return Record{}, false, nil
	}
	if c.Log == nil || c.Sink == nil {
		return Record{}, false, errors.New("evidence: a compactor needs a log and a sink")
	}
	var seqs []uint64
	var ats []time.Time
	var kinds []string
	for rec, err := range c.Log.Since(ctx, t, scope, 1) {
		if err != nil {
			return Record{}, false, err
		}
		seqs = append(seqs, rec.Seq)
		ats = append(ats, rec.At)
		kinds = append(kinds, rec.Kind)
	}
	if len(seqs) == 0 {
		return Record{}, false, nil
	}
	i, ok := c.boundary(seqs, ats, kinds)
	if !ok {
		return Record{}, false, nil
	}
	from, through := seqs[0], seqs[i]

	exp, err := c.Sink.Export(ctx, t, scope, from, through)
	if err != nil {
		return Record{}, false, err
	}
	sum, err := c.Log.Compact(ctx, t, scope, through, exp, c.Actor)
	if err != nil {
		return Record{}, false, err
	}
	return sum, true, nil
}

// boundary picks the index of the last record that may be pruned, or reports
// that none may be.
func (c *Compactor) boundary(seqs []uint64, ats []time.Time, kinds []string) (int, bool) {
	// By count: everything before the newest Keep.
	last := len(seqs) - c.Retention.Keep - 1
	// By age: the newest record already older than KeepFor.
	cutoff := c.now().Add(-c.Retention.KeepFor)
	oldest := -1
	for i, at := range ats {
		if at.Before(cutoff) {
			oldest = i
		} else {
			break // the scope is in sequence order, and so is time within it
		}
	}
	if oldest < last {
		last = oldest
	}
	if last < 0 {
		return 0, false
	}
	// A summary already sits at the scope's start when it has been compacted
	// before. Landing the new boundary exactly on it would be compacting
	// nothing — Summarize refuses it, and refusing here keeps a no-op sweep
	// from reading as an error every tick.
	if kinds[last] == KindCompaction {
		return 0, false
	}
	if last+1 < c.Retention.minPrune() {
		return 0, false
	}
	return last, true
}
