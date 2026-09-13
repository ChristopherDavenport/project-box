// Package conformance is the one test suite every storage backend must pass.
//
// It asserts the properties the architecture depends on — exclusive claims,
// strictly-increasing never-reused fencing tokens, append-only chaining, and
// tenant non-interference — against the [store.Store] interface alone, so a
// backend cannot pass by being the implementation the tests were shaped
// around. Writing this suite before any backend existed is what keeps
// SQLite's semantics from quietly becoming the definition.
//
// Each subtest works in its own tenant, which both isolates subtests sharing
// one database (the Spanner harness reuses its emulator database) and
// exercises tenant keying continuously rather than in one dedicated test.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Harness is what a backend hands the suite.
type Harness struct {
	Store store.Store
	// Advance moves the store's clock forward, for expiry tests without real
	// waiting. A backend whose clock cannot be faked (Spanner's server-side
	// time) leaves it nil and the suite uses short real TTLs and sleeps —
	// slower, but the property tested is identical.
	Advance func(d time.Duration)
}

// Run executes the suite against one backend.
func Run(t *testing.T, open func(t *testing.T) Harness) {
	h := open(t)
	s := h.Store
	ctx := context.Background()

	// ttl/wait pick the fake-clock path when the harness offers one.
	ttl := 90 * time.Minute
	wait := func() { h.Advance(2 * time.Hour) }
	if h.Advance == nil {
		ttl = 1200 * time.Millisecond
		wait = func() { time.Sleep(1600 * time.Millisecond) }
	}

	t.Run("lease/exclusive", func(t *testing.T) {
		tn := tid(t)
		a, b := s.Leaser("runner-a"), s.Leaser("runner-b")
		la, err := a.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("first acquire: %v", err)
		}
		if la.Token == 0 {
			t.Fatal("token must be non-zero")
		}
		if la.Runner != "runner-a" || la.Tenant != tn {
			t.Fatalf("lease identity wrong: %+v", la)
		}
		if _, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-2", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("second acquire: want ErrHeld, got %v", err)
		}
		// A different ref in the same repo is free.
		if _, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/other", "run-2", ttl)); err != nil {
			t.Fatalf("different ref: %v", err)
		}
	})

	t.Run("lease/race-single-winner", func(t *testing.T) {
		tn := tid(t)
		const n = 16
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				l := s.Leaser(fmt.Sprintf("runner-%d", i))
				_, errs[i] = l.Acquire(ctx, req(tn, "repo", "refs/heads/main", fmt.Sprintf("run-%d", i), ttl))
			}()
		}
		wg.Wait()
		won := 0
		for i, err := range errs {
			switch {
			case err == nil:
				won++
			case errors.Is(err, forge.ErrHeld):
			default:
				t.Fatalf("racer %d: unexpected error %v", i, err)
			}
		}
		if won != 1 {
			t.Fatalf("want exactly 1 winner, got %d", won)
		}
	})

	t.Run("lease/token-monotonic-across-release", func(t *testing.T) {
		tn := tid(t)
		l := s.Leaser("runner-a")
		var last forge.Token
		for i := range 5 {
			got, err := l.Acquire(ctx, req(tn, "repo", "refs/heads/main", fmt.Sprintf("run-%d", i), ttl))
			if err != nil {
				t.Fatalf("acquire %d: %v", i, err)
			}
			if got.Token <= last {
				t.Fatalf("token %d not greater than predecessor %d — tokens may never be reused", got.Token, last)
			}
			last = got.Token
			if err := l.Release(ctx, got); err != nil {
				t.Fatalf("release %d: %v", i, err)
			}
		}
	})

	t.Run("lease/expiry-supersede-fences-stalled-holder", func(t *testing.T) {
		tn := tid(t)
		a, b := s.Leaser("runner-a"), s.Leaser("runner-b")
		stale, err := a.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		wait() // runner-a stalls past its deadline
		fresh, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-2", ttl))
		if err != nil {
			t.Fatalf("supersede after expiry: %v", err)
		}
		if fresh.Token <= stale.Token {
			t.Fatalf("superseding token %d not greater than stale %d", fresh.Token, stale.Token)
		}
		// The stalled holder wakes up believing it holds the lease. Its token
		// has been superseded, and every path must refuse it.
		if _, err := a.Renew(ctx, stale, ttl); !errors.Is(err, forge.ErrStaleToken) {
			t.Fatalf("stalled renew: want ErrStaleToken, got %v", err)
		}
		// Releasing a superseded lease is not an error — and must not free the
		// successor's claim.
		if err := a.Release(ctx, stale); err != nil {
			t.Fatalf("stalled release: %v", err)
		}
		if _, err := a.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-3", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("successor must still hold after stale release: got %v", err)
		}
		_ = fresh
	})

	t.Run("lease/expired-unrenewed-is-not-resurrectable", func(t *testing.T) {
		tn := tid(t)
		l := s.Leaser("runner-a")
		got, err := l.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		wait()
		// Expired with no successor yet: the claim is gone, not dormant.
		if _, err := l.Renew(ctx, got, ttl); !errors.Is(err, forge.ErrExpired) {
			t.Fatalf("renew after expiry: want ErrExpired, got %v", err)
		}
	})

	t.Run("lease/renew-extends-and-release-frees", func(t *testing.T) {
		tn := tid(t)
		a, b := s.Leaser("runner-a"), s.Leaser("runner-b")
		got, err := a.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		renewed, err := a.Renew(ctx, got, ttl)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		if renewed.Token != got.Token {
			t.Fatalf("renew must keep the token: had %d, got %d", got.Token, renewed.Token)
		}
		if !renewed.Expires.After(got.Expires) {
			t.Fatalf("renew must extend expiry: %v -> %v", got.Expires, renewed.Expires)
		}
		// A released ref is immediately acquirable, at a greater token.
		if err := a.Release(ctx, renewed); err != nil {
			t.Fatalf("release: %v", err)
		}
		next, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-2", ttl))
		if err != nil {
			t.Fatalf("acquire after release: %v", err)
		}
		if next.Token <= renewed.Token {
			t.Fatalf("post-release token %d not greater than %d", next.Token, renewed.Token)
		}
		// The old holder's renew now fails on its superseded token — a release
		// does not leave a resurrection path.
		if _, err := a.Renew(ctx, renewed, ttl); !errors.Is(err, forge.ErrStaleToken) && !errors.Is(err, forge.ErrExpired) {
			t.Fatalf("renew of released lease: want stale/expired, got %v", err)
		}
	})

	t.Run("lease/inspect-reports-the-holder-without-taking-it", func(t *testing.T) {
		tn := tid(t)
		a, b := s.Leaser("runner-a"), s.Leaser("runner-b")
		// A ref nobody has ever leased has no holder — and asking must not
		// create one, which is the whole point of a read on this interface.
		if _, ok, err := b.Inspect(ctx, tn, "repo", "refs/heads/main"); err != nil || ok {
			t.Fatalf("unleased ref: ok=%v err=%v", ok, err)
		}
		if _, err := a.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl)); err != nil {
			t.Fatalf("acquire: %v", err)
		}
		got, ok, err := b.Inspect(ctx, tn, "repo", "refs/heads/main")
		if err != nil || !ok {
			t.Fatalf("held ref: ok=%v err=%v", ok, err)
		}
		// The report is the grant, not the asker: b inspected, a holds.
		if got.Runner != "runner-a" || got.Run != "run-1" || got.Token == 0 {
			t.Fatalf("inspect must report the HOLDER's identity: %+v", got)
		}
		if got.Tenant != tn || got.Repo != "repo" || got.Ref != "refs/heads/main" {
			t.Fatalf("inspect must report the ref it was asked about: %+v", got)
		}
		if got.Expired(got.Expires.Add(-time.Nanosecond)) {
			t.Fatalf("a live grant must not read as expired: %+v", got)
		}
		// It did not take the lease: the holder is still the holder.
		if _, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-2", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("inspect must not disturb the claim: got %v", err)
		}
		// Past the deadline with nobody superseding, the grant is still
		// REPORTED — "runner-a has held this well past its lease" is the fact
		// an operator needs — while Acquire correctly treats the row as free.
		wait()
		stalled, ok, err := b.Inspect(ctx, tn, "repo", "refs/heads/main")
		if err != nil || !ok {
			t.Fatalf("lapsed grant must still name its holder: ok=%v err=%v", ok, err)
		}
		if !stalled.Expired(stalled.Expires.Add(time.Nanosecond)) {
			t.Fatalf("a lapsed grant must read as expired: %+v", stalled)
		}
		// A released ref has no holder at all.
		fresh, err := b.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-3", ttl))
		if err != nil {
			t.Fatalf("acquire after lapse: %v", err)
		}
		if err := b.Release(ctx, fresh); err != nil {
			t.Fatalf("release: %v", err)
		}
		if got, ok, err := a.Inspect(ctx, tn, "repo", "refs/heads/main"); err != nil || ok {
			t.Fatalf("released ref must report no holder: %+v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("lease/tenant-non-interference", func(t *testing.T) {
		tnA, tnB := tid(t, "a"), tid(t, "b")
		a, b := s.Leaser("runner-a"), s.Leaser("runner-b")
		la, err := a.Acquire(ctx, req(tnA, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("tenant A acquire: %v", err)
		}
		// The same repo and ref name in another tenant contends for nothing.
		lb, err := b.Acquire(ctx, req(tnB, "repo", "refs/heads/main", "run-2", ttl))
		if err != nil {
			t.Fatalf("tenant B acquire of same ref name: %v", err)
		}
		// Reading across tenants sees the reader's own lease and never the
		// other's — the read path gets the same treatment as every write one.
		seen, ok, err := b.Inspect(ctx, tnB, "repo", "refs/heads/main")
		if err != nil || !ok || seen.Run != "run-2" {
			t.Fatalf("tenant B must see its OWN lease: %+v ok=%v err=%v", seen, ok, err)
		}
		// A forged cross-tenant release must not free A's claim.
		forged := la
		forged.Tenant = tnB
		_ = b.Release(ctx, forged) // error or no-op both acceptable; the property is below
		if _, err := b.Acquire(ctx, req(tnA, "repo", "refs/heads/main", "run-3", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("tenant A's lease must survive cross-tenant meddling: got %v", err)
		}
		_, _ = la, lb
	})

	t.Run("lease/preempt-fences-live-holder", func(t *testing.T) {
		tn := tid(t)
		worker, incident := s.Leaser("runner-a"), s.Leaser("runner-inc")
		held, err := worker.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-1", ttl))
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		// The holder is LIVE — not stalled, not expired — and is preempted
		// anyway. Same fence, no new mechanism.
		taken, err := incident.Preempt(ctx, req(tn, "repo", "refs/heads/main", "incident-run", ttl))
		if err != nil {
			t.Fatalf("preempt over a live claim: %v", err)
		}
		if taken.Token <= held.Token {
			t.Fatalf("preempting token %d not greater than displaced %d", taken.Token, held.Token)
		}
		if _, err := worker.Renew(ctx, held, ttl); !errors.Is(err, forge.ErrStaleToken) {
			t.Fatalf("displaced holder's renew: want ErrStaleToken, got %v", err)
		}
		// Its release is a harmless no-op that cannot free the incident's claim.
		if err := worker.Release(ctx, held); err != nil {
			t.Fatalf("displaced release: %v", err)
		}
		if _, err := worker.Acquire(ctx, req(tn, "repo", "refs/heads/main", "run-2", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("incident's claim must survive: got %v", err)
		}
		// The incident's own lease behaves normally afterwards.
		if _, err := incident.Renew(ctx, taken, ttl); err != nil {
			t.Fatalf("incident renew: %v", err)
		}
	})

	t.Run("chain/append-assigns-and-links", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var prev evidence.Record
		for i := range 4 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
			if r.Seq != uint64(i+1) {
				t.Fatalf("seq: want %d got %d", i+1, r.Seq)
			}
			if r.ID == "" || r.At.IsZero() {
				t.Fatalf("append must assign ID and At: %+v", r)
			}
			if err := evidence.CheckRecord(r); err != nil {
				t.Fatalf("returned record must verify: %v", err)
			}
			if err := evidence.CheckLink(prev, r); err != nil {
				t.Fatalf("link: %v", err)
			}
			prev = r
		}
	})

	t.Run("chain/round-trip-verifies-offline", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		want, err := log.Append(ctx, rec(tn, "scope", "sha-1"))
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		got, err := log.Head(ctx, tn, "scope")
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		if got.ID != want.ID || !got.At.Equal(want.At) {
			t.Fatalf("round trip changed the record:\n want %+v\n got  %+v", want, got)
		}
		// The stored form re-verifies from its bytes alone — no store, no
		// service. This is the exit criterion, asserted literally.
		if err := evidence.VerifyRecords([]evidence.Record{got}); err != nil {
			t.Fatalf("offline verify: %v", err)
		}
	})

	t.Run("chain/on-behalf-of-and-pid-round-trip", func(t *testing.T) {
		// on_behalf_of and pid exist to separate a change a person made from one
		// an automation made for them, on a specific process — an audit query
		// that can only read one of them back is not carrying the other, whatever
		// Append accepted.
		tn := tid(t)
		log := s.Log()
		r := rec(tn, "scope", "sha-1")
		r.OnBehalfOf = "person@example.com"
		r.Pid = 4242
		want, err := log.Append(ctx, r)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		check := func(name string, got evidence.Record) {
			t.Helper()
			if got.OnBehalfOf != want.OnBehalfOf || got.Pid != want.Pid {
				t.Fatalf("%s: on_behalf_of/pid did not round-trip: want on_behalf_of=%q pid=%d, got on_behalf_of=%q pid=%d",
					name, want.OnBehalfOf, want.Pid, got.OnBehalfOf, got.Pid)
			}
		}
		head, err := log.Head(ctx, tn, "scope")
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		check("head", head)
		for got, err := range log.Since(ctx, tn, "scope", 1) {
			if err != nil {
				t.Fatalf("since: %v", err)
			}
			check("since", got)
		}
		found := false
		for got, err := range log.BySubject(ctx, tn, "sha-1") {
			if err != nil {
				t.Fatalf("by subject: %v", err)
			}
			check("by subject", got)
			found = true
		}
		if !found {
			t.Fatalf("by subject: record not found")
		}
	})

	t.Run("chain/since-and-by-subject", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		for i := range 5 {
			if _, err := log.Append(ctx, rec(tn, "scope-a", fmt.Sprintf("sha-%d", i%2))); err != nil {
				t.Fatalf("append: %v", err)
			}
		}
		if _, err := log.Append(ctx, rec(tn, "scope-b", "sha-0")); err != nil {
			t.Fatalf("append scope-b: %v", err)
		}
		var seqs []uint64
		for r, err := range log.Since(ctx, tn, "scope-a", 3) {
			if err != nil {
				t.Fatalf("since: %v", err)
			}
			seqs = append(seqs, r.Seq)
		}
		if len(seqs) != 3 || seqs[0] != 3 || seqs[2] != 5 {
			t.Fatalf("since(3): want [3 4 5], got %v", seqs)
		}
		// BySubject spans scopes within the tenant: sha-0 appears in both.
		n := 0
		for r, err := range log.BySubject(ctx, tn, "sha-0") {
			if err != nil {
				t.Fatalf("by subject: %v", err)
			}
			if r.Subject != "sha-0" {
				t.Fatalf("wrong subject: %+v", r)
			}
			n++
		}
		if n != 4 { // seqs 1,3,5 in scope-a plus 1 in scope-b
			t.Fatalf("by subject: want 4 records across scopes, got %d", n)
		}
	})

	t.Run("chain/concurrent-appends-serialize", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		const n = 12
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			}()
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("concurrent append %d: %v", i, err)
			}
		}
		// All landed, chain intact, no gaps and no duplicates.
		if err := s.Log().Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("verify after concurrent appends: %v", err)
		}
		head, err := log.Head(ctx, tn, "scope")
		if err != nil || head.Seq != n {
			t.Fatalf("head after %d appends: seq=%d err=%v", n, head.Seq, err)
		}
	})

	t.Run("chain/tenant-non-interference", func(t *testing.T) {
		tnA, tnB := tid(t, "a"), tid(t, "b")
		log := s.Log()
		if _, err := log.Append(ctx, rec(tnA, "scope", "sha-1")); err != nil {
			t.Fatalf("append: %v", err)
		}
		// Every read path as tenant B: nothing.
		if head, err := log.Head(ctx, tnB, "scope"); err != nil || head.Seq != 0 {
			t.Fatalf("cross-tenant head: %+v, %v", head, err)
		}
		for r, err := range log.Since(ctx, tnB, "scope", 1) {
			t.Fatalf("cross-tenant since leaked: %+v, %v", r, err)
		}
		for r, err := range log.BySubject(ctx, tnB, "sha-1") {
			t.Fatalf("cross-tenant by-subject leaked: %+v, %v", r, err)
		}
		if err := log.Verify(ctx, tnB, "scope"); err != nil {
			t.Fatalf("verify of empty cross-tenant scope must pass vacuously: %v", err)
		}
		// And tenant B's own chain starts at 1, oblivious to A's.
		rb, err := log.Append(ctx, rec(tnB, "scope", "sha-1"))
		if err != nil || rb.Seq != 1 {
			t.Fatalf("tenant B chain: seq=%d err=%v", rb.Seq, err)
		}
	})

	t.Run("chain/compact-preserves-verifiable-seam", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var all []evidence.Record
		for i := range 6 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
			all = append(all, r)
		}
		pruned := all[:4]
		sum, err := log.Compact(ctx, tn, "scope", 4, receipt(pruned), "compactor")
		if err != nil {
			t.Fatalf("compact: %v", err)
		}
		// The summary takes the pruned head's seat and names its hash, which
		// is the same hash the surviving successor already names.
		if sum.Seq != 4 || sum.Prev != all[3].ID || sum.Kind != evidence.KindCompaction {
			t.Fatalf("summary must sit at the pruned head: %+v", sum)
		}
		// The store proves the hot side of the seam.
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("compacted scope must verify: %v", err)
		}
		// The suffix is untouched: the head is the record it always was, and
		// the hot store holds exactly summary + survivors.
		head, err := log.Head(ctx, tn, "scope")
		if err != nil || head.ID != all[5].ID {
			t.Fatalf("head after compaction: %+v, %v", head, err)
		}
		var hot []evidence.Record
		for r, err := range log.Since(ctx, tn, "scope", 1) {
			if err != nil {
				t.Fatalf("since: %v", err)
			}
			hot = append(hot, r)
		}
		if len(hot) != 3 || hot[0].ID != sum.ID || hot[1].ID != all[4].ID || hot[2].ID != all[5].ID {
			t.Fatalf("hot store must keep summary + suffix, got %d records", len(hot))
		}
		// Offline, an auditor proves both sides from bytes alone: the hot walk
		// starting at the summary, and the exported run ending at the hash the
		// summary continues from.
		if err := evidence.VerifyRecords(hot); err != nil {
			t.Fatalf("hot side offline: %v", err)
		}
		if err := evidence.VerifySeam(sum, pruned); err != nil {
			t.Fatalf("worm side offline: %v", err)
		}
		// Appends continue as if nothing happened.
		r7, err := log.Append(ctx, rec(tn, "scope", "sha-7"))
		if err != nil || r7.Seq != 7 || r7.Prev != all[5].ID {
			t.Fatalf("append after compaction: %+v, %v", r7, err)
		}
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("verify after post-compaction append: %v", err)
		}
	})

	t.Run("chain/compact-to-head-then-again", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var all []evidence.Record
		for i := range 3 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
			all = append(all, r)
		}
		// Compacting through the head leaves a scope of exactly one summary.
		sum1, err := log.Compact(ctx, tn, "scope", 3, receipt(all), "compactor")
		if err != nil {
			t.Fatalf("compact to head: %v", err)
		}
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("bare summary must verify: %v", err)
		}
		// The next append chains to the summary itself — the seam's other
		// spelling.
		r4, err := log.Append(ctx, rec(tn, "scope", "sha-4"))
		if err != nil || r4.Seq != 4 || r4.Prev != sum1.ID {
			t.Fatalf("append onto summary: %+v, %v", r4, err)
		}
		r5, err := log.Append(ctx, rec(tn, "scope", "sha-5"))
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		// A second compaction swallows the first summary: its export runs
		// from the prior summary's seat, so exports chain back to genesis.
		hot := []evidence.Record{sum1, r4, r5}
		sum2, err := log.Compact(ctx, tn, "scope", 5, receipt(hot), "compactor")
		if err != nil {
			t.Fatalf("second compact: %v", err)
		}
		c, err := evidence.ParseCompaction(sum2)
		if err != nil {
			t.Fatalf("parse second summary: %v", err)
		}
		if c.From != 3 || c.Through != 5 || sum2.Prev != r5.ID {
			t.Fatalf("second summary must cover back to the first: %+v", c)
		}
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("verify after second compaction: %v", err)
		}
		if err := evidence.VerifySeam(sum2, hot); err != nil {
			t.Fatalf("seam over an export that starts at a summary: %v", err)
		}
	})

	t.Run("chain/compact-refusals", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var all []evidence.Record
		for i := range 3 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
			all = append(all, r)
		}
		// The summary kind is reserved for the seam.
		forged := rec(tn, "scope", "sha-x")
		forged.Kind = evidence.KindCompaction
		if _, err := log.Append(ctx, forged); err == nil {
			t.Fatal("Append must refuse the reserved summary kind")
		}
		// A receipt that does not pin the pruned run is refused: no
		// checkpoint, no prune.
		good := receipt(all[:2])
		bad := good
		bad.Head = all[2].ID
		if _, err := log.Compact(ctx, tn, "scope", 2, bad, "compactor"); err == nil {
			t.Fatal("Compact must refuse a receipt whose head is not the pruned head")
		}
		bad = good
		bad.To = 3
		if _, err := log.Compact(ctx, tn, "scope", 2, bad, "compactor"); err == nil {
			t.Fatal("Compact must refuse a receipt covering a different range")
		}
		// Beyond the head, in an empty scope, or in another tenant: nothing
		// to stand for.
		if _, err := log.Compact(ctx, tn, "scope", 9, good, "compactor"); err == nil {
			t.Fatal("Compact past the head must be refused")
		}
		if _, err := log.Compact(ctx, tn, "elsewhere", 2, good, "compactor"); err == nil {
			t.Fatal("Compact of an empty scope must be refused")
		}
		if _, err := log.Compact(ctx, tid(t, "other"), "scope", 2, good, "compactor"); err == nil {
			t.Fatal("cross-tenant Compact must find nothing")
		}
		// Every refusal left the chain exactly as it was.
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("chain after refusals: %v", err)
		}
		if head, err := log.Head(ctx, tn, "scope"); err != nil || head.ID != all[2].ID {
			t.Fatalf("head after refusals: %+v, %v", head, err)
		}
		// A bare summary cannot be compacted again — there is nothing beyond
		// it to summarize.
		if _, err := log.Compact(ctx, tn, "scope", 2, good, "compactor"); err != nil {
			t.Fatalf("legitimate compact: %v", err)
		}
		if _, err := log.Compact(ctx, tn, "scope", 2, good, "compactor"); err == nil {
			t.Fatal("re-compacting a bare summary must be refused")
		}
	})

	t.Run("chain/compacted-tamper-still-fails", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var all []evidence.Record
		for i := range 5 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
			all = append(all, r)
		}
		pruned := all[:3]
		sum, err := log.Compact(ctx, tn, "scope", 3, receipt(pruned), "compactor")
		if err != nil {
			t.Fatalf("compact: %v", err)
		}
		hot := []evidence.Record{sum, all[3], all[4]}
		if err := evidence.VerifyRecords(hot); err != nil {
			t.Fatalf("intact compacted chain must verify: %v", err)
		}
		// As before compaction, the attacks are asserted offline: the store's
		// own API has no mutation to abuse. A tampered survivor fails.
		tampered := append([]evidence.Record(nil), hot...)
		tampered[1].Body = []byte("altered after the fact")
		if err := evidence.VerifyRecords(tampered); err == nil {
			t.Fatal("tampered survivor must fail verification")
		}
		// A removed survivor breaks the chain, seam or no seam.
		if err := evidence.VerifyRecords([]evidence.Record{hot[0], hot[2]}); err == nil {
			t.Fatal("removed survivor must break the chain")
		}
		// A doctored summary no longer matches its own ID.
		doctored := append([]evidence.Record(nil), hot...)
		doctored[0].Body = []byte(`{"from":1,"through":3}`)
		if err := evidence.VerifyRecords(doctored); err == nil {
			t.Fatal("doctored summary must fail verification")
		}
		// Re-sealing a summary aimed at a different history does not help:
		// the survivor names the true pruned head, and the impostor names
		// something else.
		impostor := sum
		impostor.Prev = all[0].ID
		reparsed, err := evidence.ParseCompaction(sum)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		reparsed.Export.Head = all[0].ID
		if impostor.Body, err = json.Marshal(reparsed); err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if impostor, err = evidence.Seal(impostor); err != nil {
			t.Fatalf("seal: %v", err)
		}
		if err := evidence.VerifyRecords([]evidence.Record{impostor, hot[1], hot[2]}); err == nil {
			t.Fatal("a re-aimed summary must fail against the surviving chain")
		}
		// The WORM side fails the same way: a truncated or tampered export
		// does not prove the seam.
		if err := evidence.VerifySeam(sum, pruned[:2]); err == nil {
			t.Fatal("truncated export must fail the seam")
		}
		exported := append([]evidence.Record(nil), pruned...)
		exported[1].Body = []byte("altered after the fact")
		if err := evidence.VerifySeam(sum, exported); err == nil {
			t.Fatal("tampered export must fail the seam")
		}
	})

	t.Run("chain/verify-detects-tamper-and-removal", func(t *testing.T) {
		tn := tid(t)
		log := s.Log()
		var stored []evidence.Record
		for i := range 3 {
			r, err := log.Append(ctx, rec(tn, "scope", fmt.Sprintf("sha-%d", i)))
			if err != nil {
				t.Fatalf("append: %v", err)
			}
			stored = append(stored, r)
		}
		if err := log.Verify(ctx, tn, "scope"); err != nil {
			t.Fatalf("intact chain must verify: %v", err)
		}
		// Tampering and removal are asserted offline: the store's own API has
		// no mutation to abuse — which is itself the property — so the attack
		// is simulated on the exported form, exactly where an auditor checks.
		tampered := append([]evidence.Record(nil), stored...)
		tampered[1].Body = []byte("altered after the fact")
		if err := evidence.VerifyRecords(tampered); err == nil {
			t.Fatal("tampered record must fail verification")
		}
		removed := []evidence.Record{stored[0], stored[2]}
		if err := evidence.VerifyRecords(removed); err == nil {
			t.Fatal("removed record must break the chain")
		}
	})

	// The run log is telemetry beside the chain: ordered within a run, scoped
	// to the tenant on every call, readable after the run, and pruned by age
	// without a ceremony — which is exactly what the chain must never allow.
	t.Run("runlog/ordered-within-a-run", func(t *testing.T) {
		tn := tid(t)
		rl := s.Runs()
		at := time.Date(2026, 9, 5, 4, 26, 14, 985700924, time.UTC)
		first, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-1", At: at, Kind: "started", Text: "agentref"})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if first.N != 1 || !first.At.Equal(at) {
			t.Fatalf("first frame = n %d at %v, want n 1 at the moment given", first.N, first.At)
		}
		if _, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-1", Kind: "tool", Call: "t-1", Tool: "Read"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-1", Kind: "tool", Call: "t-1", Tool: "Read", Text: "Read main.go"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-2", Kind: "started"}); err != nil {
			t.Fatalf("append to another run: %v", err)
		}
		got, err := rl.Frames(ctx, tn, "wi-1")
		if err != nil {
			t.Fatalf("frames: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("frames for wi-1 = %d, want 3 (the other run's stay out)", len(got))
		}
		for i, f := range got {
			if f.N != uint64(i+1) || f.Run != "wi-1" {
				t.Errorf("frame %d = run %q n %d, want wi-1 n %d", i, f.Run, f.N, i+1)
			}
			if f.At.IsZero() {
				t.Errorf("frame %d has no time; an unset At is stamped on append", i)
			}
		}
		if got[2].Call != "t-1" || got[2].Text != "Read main.go" {
			t.Errorf("third frame = %+v, want the titled Read with its call id", got[2])
		}
		if _, err := rl.Append(ctx, tn, runlog.Frame{Kind: "tool"}); err == nil {
			t.Fatal("a frame with no run was accepted; a run log entry that names no run is unreadable")
		}
		if none, err := rl.Frames(ctx, tn, "wi-none"); err != nil || len(none) != 0 {
			t.Fatalf("a run nothing was kept for = %v, %v; want nothing and no error", none, err)
		}
	})

	t.Run("runlog/session-round-trips", func(t *testing.T) {
		tn := tid(t)
		rl := s.Runs()
		if _, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-1", Kind: "started", Session: "agentref-1"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, err := rl.Append(ctx, tn, runlog.Frame{Run: "wi-1", Kind: "tool", Tool: "Read"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		got, err := rl.Frames(ctx, tn, "wi-1")
		if err != nil {
			t.Fatalf("frames: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("frames = %d, want 2", len(got))
		}
		if got[0].Session != "agentref-1" {
			t.Errorf("first frame's session = %q, want the agentref it was appended with", got[0].Session)
		}
		if got[1].Session != "" {
			t.Errorf("second frame's session = %q, want empty — a frame appended without one must not have one invented", got[1].Session)
		}
	})

	t.Run("runlog/tenant-scoped", func(t *testing.T) {
		a, b := tid(t, "a"), tid(t, "b")
		rl := s.Runs()
		if _, err := rl.Append(ctx, a, runlog.Frame{Run: "wi-1", Kind: "started"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if got, err := rl.Frames(ctx, b, "wi-1"); err != nil || len(got) != 0 {
			t.Fatalf("tenant b reads tenant a's run: %v, %v", got, err)
		}
		if _, err := rl.Append(ctx, "", runlog.Frame{Run: "wi-1", Kind: "started"}); err == nil {
			t.Fatal("an empty tenant was accepted")
		}
	})

	t.Run("runlog/pruned-by-age", func(t *testing.T) {
		a, b := tid(t, "a"), tid(t, "b")
		rl := s.Runs()
		old := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		recent := old.Add(72 * time.Hour)
		for _, f := range []runlog.Frame{
			{Run: "wi-old", At: old, Kind: "started"},
			{Run: "wi-old", At: old.Add(time.Second), Kind: "tool", Tool: "Read"},
			{Run: "wi-new", At: recent, Kind: "started"},
		} {
			if _, err := rl.Append(ctx, a, f); err != nil {
				t.Fatalf("append: %v", err)
			}
		}
		if _, err := rl.Append(ctx, b, runlog.Frame{Run: "wi-old", At: old, Kind: "started"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		n, err := rl.Prune(ctx, a, old.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		if n != 2 {
			t.Fatalf("pruned %d frames, want 2 (the old run's; the recent one and the other tenant's stay)", n)
		}
		if got, _ := rl.Frames(ctx, a, "wi-old"); len(got) != 0 {
			t.Errorf("the old run still has %d frames after the prune", len(got))
		}
		if got, _ := rl.Frames(ctx, a, "wi-new"); len(got) != 1 {
			t.Errorf("the recent run lost frames: %d left, want 1", len(got))
		}
		if got, _ := rl.Frames(ctx, b, "wi-old"); len(got) != 1 {
			t.Errorf("tenant b's run was pruned by tenant a's policy: %d left, want 1", len(got))
		}
	})
}

// tid derives a valid, unique tenant id from the subtest name.
func tid(t *testing.T, suffix ...string) tenant.ID {
	s := strings.ToLower(t.Name())
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, s)
	for _, x := range suffix {
		mapped += "-" + x
	}
	if len(mapped) > 63 {
		mapped = mapped[len(mapped)-63:]
	}
	mapped = strings.Trim(mapped, "-")
	id := tenant.ID(mapped)
	if err := id.Validate(); err != nil {
		t.Fatalf("test bug: derived tenant id %q invalid: %v", mapped, err)
	}
	return id
}

func req(tn tenant.ID, repo, ref, run string, ttl time.Duration) forge.LeaseRequest {
	return forge.LeaseRequest{Tenant: tn, Repo: repo, Ref: ref, Run: run, TTL: ttl}
}

// receipt fabricates the WORM half of a compaction over rs, the in-order run
// about to be pruned. In production a Sink exported the run and returned
// this; the suite asserts the store's side of the bargain — that Compact
// validates the receipt against its own rows — not the sink's.
func receipt(rs []evidence.Record) evidence.Export {
	last := rs[len(rs)-1]
	return evidence.Export{
		Tenant: last.Tenant, Scope: last.Scope,
		From: rs[0].Seq, To: last.Seq, Head: last.ID,
		URI:    "mem://conformance/" + string(last.Tenant),
		Digest: "test-digest",
		At:     last.At,
	}
}

func rec(tn tenant.ID, scope, subject string) evidence.Record {
	return evidence.Record{
		Tenant:  tn,
		Scope:   scope,
		Kind:    "test",
		Subject: subject,
		Actor:   "conformance",
		Body:    []byte(`{"probe":true}`),
	}
}
