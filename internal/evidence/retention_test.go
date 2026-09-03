package evidence_test

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

// scopeOf writes n records into one scope, stamped at one-minute intervals
// ending at `end`, and returns the store and the sink over it.
func scopeOf(t *testing.T, n int, end time.Time) (*sqlite.Store, *evidence.DirSink, string) {
	t.Helper()
	dir := t.TempDir()
	// The store stamps At, so the clock has to be the store's for an
	// age-based policy to mean anything in a test.
	at := end.Add(-time.Duration(n) * time.Minute)
	clock := func() time.Time { return at }
	s, err := sqlite.Open(filepath.Join(dir, "box.db"), sqlite.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for i := range n {
		at = end.Add(-time.Duration(n-i) * time.Minute)
		if _, err := s.Log().Append(context.Background(), evidence.Record{
			Tenant: tn, Scope: "control/capacity", Kind: "charge",
			Subject: fmt.Sprintf("run-%d", i), Actor: "boxd",
			Body: []byte(fmt.Sprintf(`{"tokens":%d,"metered":true}`, 1000+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s, &evidence.DirSink{Dir: filepath.Join(dir, "worm"), Log: s.Log()}, filepath.Join(dir, "worm")
}

// TestCompactionLeavesAChainThatStillVerifies is the property the issue is
// about. Gas Town could simply flatten its chain because it was honest by
// norm; ours is tamper-evident by construction, so a prune is
// indistinguishable from the attack Verify exists to catch unless the schema
// says otherwise. Both sides have to prove out: the hot chain across the
// seam, and the pruned run against the object it was exported to.
func TestCompactionLeavesAChainThatStillVerifies(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s, sink, _ := scopeOf(t, 500, now)

	c := &evidence.Compactor{Log: s.Log(), Sink: sink, Actor: "boxd",
		Retention: evidence.Retention{Keep: 100, KeepFor: time.Hour, MinPrune: 10},
		Now:       func() time.Time { return now }}

	sum, ok, err := c.CompactScope(ctx, tn, "control/capacity")
	if err != nil || !ok {
		t.Fatalf("compaction did not run: ok=%v err=%v", ok, err)
	}

	// Keep=100 leaves the newest hundred; KeepFor=1h would leave sixty. The
	// stricter bound wins, so the seam sits at 400 and not at 440.
	cmp, err := evidence.ParseCompaction(sum)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.From != 1 || cmp.Through != 400 || cmp.Records != 400 {
		t.Fatalf("seam covers [%d..%d] over %d records, want [1..400] over 400", cmp.From, cmp.Through, cmp.Records)
	}
	if cmp.Kinds["charge"] != 400 {
		t.Fatalf("the semantic residue is missing: %v", cmp.Kinds)
	}

	// The hot side: a scope that begins at a summary still verifies.
	if err := s.Log().Verify(ctx, tn, "control/capacity"); err != nil {
		t.Fatalf("the compacted scope does not verify: %v", err)
	}
	var hot int
	for _, err := range s.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		hot++
	}
	if hot != 101 { // the summary at 400, plus 401..500
		t.Fatalf("hot store holds %d records, want 101", hot)
	}

	// The pruned side, offline, from the bytes the sink wrote.
	exported, err := sink.Load(cmp.Export)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.VerifySeam(sum, exported); err != nil {
		t.Fatalf("the seam does not prove against its export: %v", err)
	}
	if len(exported) != 400 {
		t.Fatalf("export holds %d records, want 400", len(exported))
	}
}

// TestASecondCompactionSwallowsTheFirst. A scope holds at most one summary,
// at its start, so successive exports chain: each covers back to the previous
// summary, whose body names the export before it, all the way to genesis.
func TestASecondCompactionSwallowsTheFirst(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s, sink, _ := scopeOf(t, 500, now)

	c := &evidence.Compactor{Log: s.Log(), Sink: sink, Actor: "boxd",
		Retention: evidence.Retention{Keep: 100, KeepFor: time.Hour, MinPrune: 10},
		Now:       func() time.Time { return now }}
	first, ok, err := c.CompactScope(ctx, tn, "control/capacity")
	if err != nil || !ok {
		t.Fatalf("first compaction: ok=%v err=%v", ok, err)
	}

	// Time passes; the hundred that were too young are now too old.
	later := now.Add(4 * time.Hour)
	c.Now = func() time.Time { return later }
	c.Retention = evidence.Retention{Keep: 20, KeepFor: time.Hour, MinPrune: 10}
	second, ok, err := c.CompactScope(ctx, tn, "control/capacity")
	if err != nil || !ok {
		t.Fatalf("second compaction: ok=%v err=%v", ok, err)
	}

	cmp, err := evidence.ParseCompaction(second)
	if err != nil {
		t.Fatal(err)
	}
	// It covers back to the first summary's seat, and swallowed it: 400
	// (the summary) through 480, which is 81 records.
	if cmp.From != first.Seq || cmp.Through != 480 {
		t.Fatalf("second seam covers [%d..%d], want [%d..480]", cmp.From, cmp.Through, first.Seq)
	}
	if cmp.Kinds[evidence.KindCompaction] != 1 {
		t.Fatalf("the second summary did not swallow the first: %v", cmp.Kinds)
	}
	if err := s.Log().Verify(ctx, tn, "control/capacity"); err != nil {
		t.Fatalf("twice-compacted scope does not verify: %v", err)
	}
	// And the chain back to genesis is walkable: the second summary names the
	// first's export, which holds the first summary, which names its own.
	exported, err := sink.Load(cmp.Export)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.VerifySeam(second, exported); err != nil {
		t.Fatal(err)
	}
	inner, err := evidence.ParseCompaction(exported[0])
	if err != nil {
		t.Fatalf("the swallowed summary is not intact inside the export: %v", err)
	}
	genesis, err := sink.Load(inner.Export)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.VerifySeam(exported[0], genesis); err != nil {
		t.Fatalf("the chain does not reach genesis: %v", err)
	}
}

// TestAZeroRetentionCompactsNothing. Everywhere else here an unset policy
// resolves to the strict end because the permissive end is dangerous. Pruning
// inverts that — the destructive act is the one a silent default would take —
// so a compactor nobody configured must do nothing at all.
func TestAZeroRetentionCompactsNothing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s, sink, dir := scopeOf(t, 500, now)

	for _, r := range []evidence.Retention{
		{},                   // nothing said at all
		{Keep: 100},          // a count and no age
		{KeepFor: time.Hour}, // an age and no count
		{Keep: 100, KeepFor: 30 * 24 * time.Hour}, // configured, nothing old enough
	} {
		c := &evidence.Compactor{Log: s.Log(), Sink: sink, Actor: "boxd",
			Retention: r, Now: func() time.Time { return now }}
		if _, ok, err := c.CompactScope(ctx, tn, "control/capacity"); ok || err != nil {
			t.Fatalf("%+v compacted something: ok=%v err=%v", r, ok, err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a compactor that pruned nothing still wrote to the sink")
	}
}

// TestMinPruneKeepsTheChainFromBecomingBoundaries. A seam is not free: every
// summary is a record a verifier parses and an auditor reasons across, so a
// policy that compacted three rows at a time would buy a slightly smaller
// store with a chain that is mostly boundaries.
func TestMinPruneKeepsTheChainFromBecomingBoundaries(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s, sink, _ := scopeOf(t, 120, now)

	c := &evidence.Compactor{Log: s.Log(), Sink: sink, Actor: "boxd",
		// 100 hot of 120 leaves 20 prunable, under a min of 50.
		Retention: evidence.Retention{Keep: 100, KeepFor: time.Minute, MinPrune: 50},
		Now:       func() time.Time { return now }}
	if _, ok, err := c.CompactScope(ctx, tn, "control/capacity"); ok || err != nil {
		t.Fatalf("a 20-record run should be left alone: ok=%v err=%v", ok, err)
	}

	c.Retention.MinPrune = 10
	if _, ok, err := c.CompactScope(ctx, tn, "control/capacity"); !ok || err != nil {
		t.Fatalf("the same run above the minimum should compact: ok=%v err=%v", ok, err)
	}
}

// TestTheSinkRefusesToExportARunThatDoesNotVerify. An export is the durable
// half of a compaction, so exporting a broken prefix would put the tampering
// somewhere Verify can no longer reach — the laundering that Summarize
// already refuses on the hot side, refused here first because this side runs
// first.
func TestTheSinkRefusesToExportARunThatDoesNotVerify(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sink := &evidence.DirSink{Dir: filepath.Join(dir, "worm"), Log: brokenLog{}}
	if _, err := sink.Export(ctx, tn, "control/capacity", 1, 2); err == nil ||
		!strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("a broken run was exported: %v", err)
	}
}

// TestAnExportedObjectIsNotRewritten. A directory is not a retention lock and
// this type says so, but the one thing it can hold is that an object already
// written is never changed under an auditor.
func TestAnExportedObjectIsNotRewritten(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s, sink, dir := scopeOf(t, 20, now)

	exp, err := sink.Export(ctx, tn, "control/capacity", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if exp.RetainUntil != (time.Time{}) {
		t.Fatal("a local directory must not claim a retention lock it does not have")
	}
	path := filepath.Join(dir, strings.TrimPrefix(exp.Digest, "sha256:")+".ndjson")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("the exported object is writable: %v", info.Mode())
	}
	// The same range again is the same bytes, and idempotent rather than an
	// error — a retried sweep must not fail on its own previous success.
	again, err := sink.Export(ctx, tn, "control/capacity", 1, 10)
	if err != nil {
		t.Fatalf("re-exporting the same run failed: %v", err)
	}
	if again.Digest != exp.Digest {
		t.Fatalf("the same run exported to two digests: %s and %s", exp.Digest, again.Digest)
	}
	_ = s
}

// brokenLog yields two records that do not chain.
type brokenLog struct{ evidence.Log }

func (brokenLog) Since(ctx context.Context, t tenant.ID, scope string, seq uint64) iter.Seq2[evidence.Record, error] {
	return func(yield func(evidence.Record, error) bool) {
		for _, r := range []evidence.Record{
			{Tenant: t, Scope: scope, Seq: 1, ID: "aaaa", Kind: "x"},
			{Tenant: t, Scope: scope, Seq: 2, ID: "bbbb", Prev: "not-aaaa", Kind: "x"},
		} {
			if !yield(r, nil) {
				return
			}
		}
	}
}
