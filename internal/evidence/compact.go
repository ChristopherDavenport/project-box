package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// KindCompaction marks a summary record standing for a pruned prefix. The
// kind is reserved: Append refuses it, so a summary can enter a chain only
// through Compact, after the pruned run's export receipt has been checked
// against the store's own rows. Without the reservation, a well-formed
// summary appended mid-stream would be indistinguishable from a seam.
const KindCompaction = "compaction"

// Compaction is the body of a summary record: what was pruned, what it
// amounted to, and where the full run now lives.
type Compaction struct {
	// From and Through bound the pruned prefix, inclusive. From is the first
	// pruned row's sequence — 1 on a scope's first compaction, the prior
	// summary's seat afterwards. Successive exports therefore chain: each
	// covers back to the previous summary, whose body names the export before
	// it, all the way to genesis.
	From    uint64 `json:"from"`
	Through uint64 `json:"through"`
	// Records is how many rows this compaction pruned, a swallowed prior
	// summary included.
	Records uint64 `json:"records"`
	// Kinds counts the pruned records by kind — the semantic residue: enough
	// to know what a busy scope was doing without replaying the WORM export.
	Kinds map[string]uint64 `json:"kinds"`
	// FirstAt and LastAt bound the pruned records in time.
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
	// Export is the WORM receipt for the pruned run. Its Head equals the
	// summary's Prev: the exported object ends at exactly the hash the hot
	// chain continues from, which is what anchors the seam.
	Export Export `json:"export"`
}

// Summarize builds the summary record standing for a pruned prefix. rs is the
// scope's hot prefix in order — genesis or the prior summary first — and exp
// is the WORM receipt for exactly that run. The returned record is unsealed:
// the backend stamps At from its own clock and seals, the same split Append
// uses. Backends implement Compact by delegating here so the judgement is
// single-sourced, for the same reason Verify delegates to VerifyScope.
//
// The summary takes the pruned head's seat: same Seq, and Prev naming the
// head's ID. A successor sealed before the compaction already names that
// hash, so the chain stays continuous without rewriting anything that
// survives. Subject is left empty on purpose — a summary is about the scope,
// not any one commit, and must not answer an auditor's by-subject query.
func Summarize(rs []Record, exp Export, actor string) (Record, error) {
	if len(rs) == 0 {
		return Record{}, errors.New("evidence: nothing to compact")
	}
	last := rs[len(rs)-1]
	if last.Kind == KindCompaction {
		return Record{}, fmt.Errorf("evidence: %s/%s has nothing beyond the previous summary at seq %d", last.Tenant, last.Scope, last.Seq)
	}
	// A prefix that does not verify must never be flattened: compaction would
	// otherwise launder exactly the tampering Verify exists to catch.
	if err := VerifyRecords(rs); err != nil {
		return Record{}, fmt.Errorf("evidence: refusing to compact a prefix that does not verify: %w", err)
	}
	// The receipt must pin exactly the run being pruned — no checkpoint, no
	// prune.
	if exp.Tenant != last.Tenant || exp.Scope != last.Scope {
		return Record{}, fmt.Errorf("evidence: export receipt is for %s/%s, compacting %s/%s", exp.Tenant, exp.Scope, last.Tenant, last.Scope)
	}
	if exp.From != rs[0].Seq || exp.To != last.Seq {
		return Record{}, fmt.Errorf("evidence: export receipt covers [%d..%d], pruning [%d..%d]", exp.From, exp.To, rs[0].Seq, last.Seq)
	}
	if exp.Head != last.ID {
		return Record{}, fmt.Errorf("evidence: export head %q is not the pruned head %q — the WORM object must end at the record the chain continues from", exp.Head, last.ID)
	}
	kinds := make(map[string]uint64)
	for _, r := range rs {
		kinds[r.Kind]++
	}
	body, err := json.Marshal(Compaction{
		From:    rs[0].Seq,
		Through: last.Seq,
		Records: uint64(len(rs)),
		Kinds:   kinds,
		FirstAt: rs[0].At,
		LastAt:  last.At,
		Export:  exp,
	})
	if err != nil {
		return Record{}, fmt.Errorf("evidence: encode summary: %w", err)
	}
	return Record{
		Tenant: last.Tenant,
		Scope:  last.Scope,
		Seq:    last.Seq,
		Prev:   last.ID,
		Kind:   KindCompaction,
		Actor:  actor,
		Body:   body,
	}, nil
}

// ParseCompaction decodes a summary record's body and checks it stands where
// it claims: coverage ends at the summary's own seat, and the embedded export
// receipt pins the seam — same range, and a head equal to the hash the
// summary names as Prev. These checks hold from the record's bytes alone, so
// they run wherever the record does, offline included; Verify applies them to
// every summary it walks.
func ParseCompaction(r Record) (Compaction, error) {
	if r.Kind != KindCompaction {
		return Compaction{}, fmt.Errorf("evidence: record %s/%s seq %d is %q, not a summary", r.Tenant, r.Scope, r.Seq, r.Kind)
	}
	var c Compaction
	if err := json.Unmarshal(r.Body, &c); err != nil {
		return Compaction{}, fmt.Errorf("evidence: summary %s/%s seq %d body: %w", r.Tenant, r.Scope, r.Seq, err)
	}
	switch {
	case c.Through != r.Seq:
		return Compaction{}, fmt.Errorf("evidence: summary at seq %d claims coverage through %d — a summary sits at its own boundary", r.Seq, c.Through)
	case c.From == 0 || c.From > c.Through:
		return Compaction{}, fmt.Errorf("evidence: summary at seq %d covers impossible range [%d..%d]", r.Seq, c.From, c.Through)
	case c.Export.Tenant != r.Tenant || c.Export.Scope != r.Scope:
		return Compaction{}, fmt.Errorf("evidence: summary %s/%s anchors an export of %s/%s", r.Tenant, r.Scope, c.Export.Tenant, c.Export.Scope)
	case c.Export.From != c.From || c.Export.To != c.Through:
		return Compaction{}, fmt.Errorf("evidence: summary covers [%d..%d] but anchors an export of [%d..%d]", c.From, c.Through, c.Export.From, c.Export.To)
	case c.Export.Head != r.Prev:
		return Compaction{}, fmt.Errorf("evidence: summary anchors export head %q but continues from %q", c.Export.Head, r.Prev)
	}
	return c, nil
}

// VerifySeam proves the WORM side of a compaction: the exported run is an
// intact chain covering exactly what the summary claims, ending at the hash
// the summary continues from. Together with a verified hot side — VerifyScope
// against the store, or VerifyRecords over its export — this is the whole
// proof across the seam, from bytes alone. The exported object's digest and
// retention lock are the sink's side of the bargain, checked against the
// bucket rather than here.
func VerifySeam(summary Record, exported []Record) error {
	if err := CheckRecord(summary); err != nil {
		return err
	}
	c, err := ParseCompaction(summary)
	if err != nil {
		return err
	}
	if len(exported) == 0 {
		return fmt.Errorf("evidence: seam at %s/%s seq %d checked against an empty export", summary.Tenant, summary.Scope, summary.Seq)
	}
	if err := VerifyRecords(exported); err != nil {
		return err
	}
	first, last := exported[0], exported[len(exported)-1]
	if first.Tenant != summary.Tenant || first.Scope != summary.Scope {
		return fmt.Errorf("evidence: seam at %s/%s checked against an export of %s/%s", summary.Tenant, summary.Scope, first.Tenant, first.Scope)
	}
	if first.Seq != c.From || last.Seq != c.Through {
		return fmt.Errorf("evidence: export holds [%d..%d], summary covers [%d..%d]", first.Seq, last.Seq, c.From, c.Through)
	}
	if last.ID != summary.Prev {
		return fmt.Errorf("evidence: exported head %q is not the hash the summary continues from (%q)", last.ID, summary.Prev)
	}
	return nil
}
