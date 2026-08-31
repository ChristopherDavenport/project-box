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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
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
		// A forged cross-tenant release must not free A's claim.
		forged := la
		forged.Tenant = tnB
		_ = b.Release(ctx, forged) // error or no-op both acceptable; the property is below
		if _, err := b.Acquire(ctx, req(tnA, "repo", "refs/heads/main", "run-3", ttl)); !errors.Is(err, forge.ErrHeld) {
			t.Fatalf("tenant A's lease must survive cross-tenant meddling: got %v", err)
		}
		_, _ = la, lb
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
