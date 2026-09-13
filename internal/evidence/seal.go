package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// sealDomain versions the canonical serialization. Any change to the field
// set or encoding MUST change it, or old and new records would hash under one
// scheme and Verify could not tell a tampered record from a re-encoded one.
//
// sealDomainV1 is the field set before Pid existed. Every chain written under
// it — the dogfood daemon's live database included — must keep verifying
// byte-for-byte, so it is not retired: CheckRecord still accepts it, but only
// for a record whose Pid is zero. A record asserting a real pid has no v1
// spelling — Pid was never zero for it — so it must match v2.
const (
	sealDomainV1 = "project-box/evidence/v1"
	sealDomain   = "project-box/evidence/v2"
)

// Seal fills in a record's ID from its canonical serialization. Seq and Prev
// must already be assigned; every other field is hashed as given, so a sealed
// record is verifiable offline from its bytes alone — no service, no store.
//
// At is normalized to UTC nanoseconds before hashing, which is exactly the
// precision backends store; a record therefore hashes identically before it is
// written and after it is read back.
func Seal(r Record) (Record, error) {
	if r.Seq == 0 {
		return Record{}, errors.New("evidence: seal before sequence assignment")
	}
	r.At = r.At.UTC()
	r.ID = hex.EncodeToString(digest(r))
	return r, nil
}

// digest hashes the canonical serialization under the current domain: a
// domain tag, then every field except ID, each length-prefixed so no two
// field boundaries can collide. Length-prefixed concatenation is chosen over
// JSON because determinism is load-bearing here and JSON's is easy to lose
// (key order, number forms, escaping) without any test noticing.
func digest(r Record) []byte {
	return digestDomain(r, sealDomain)
}

// digestV1 hashes the field set as it stood before Pid existed — CheckRecord's
// fallback for records sealed under sealDomainV1.
func digestV1(r Record) []byte {
	return digestDomain(r, sealDomainV1)
}

// digestDomain hashes r's canonical serialization under domain. v1 omits Pid
// entirely rather than hashing a zero for it: the field did not exist yet, and
// "absent" and "present but zero" must not collide in the digest.
func digestDomain(r Record, domain string) []byte {
	h := sha256.New()
	put := func(b []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	putU64 := func(v uint64) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], v)
		put(n[:])
	}
	put([]byte(domain))
	put([]byte(r.Tenant))
	put([]byte(r.Scope))
	putU64(r.Seq)
	put([]byte(r.Prev))
	put([]byte(r.Kind))
	put([]byte(r.Subject))
	put([]byte(r.Actor))
	if domain != sealDomainV1 {
		putU64(uint64(r.Pid))
	}
	put([]byte(r.OnBehalfOf))
	put([]byte(r.Grant))
	put(r.Body)
	putU64(uint64(r.At.UTC().UnixNano()))
	put([]byte(r.Trace))
	return h.Sum(nil)
}

// CheckRecord reports whether a record's ID matches its content. This is the
// tamper check: any altered field changes the digest.
//
// A record matches either the current domain, or — only when its Pid is zero
// — sealDomainV1's field set. That second branch is not leniency: a record
// with Pid zero and a v1 digest is exactly what every chain written before
// this change looks like, and it must keep verifying byte-for-byte. A record
// that asserts a nonzero Pid has no v1 spelling to fall back to.
func CheckRecord(r Record) error {
	if r.ID == hex.EncodeToString(digest(r)) {
		return nil
	}
	if r.Pid == 0 && r.ID == hex.EncodeToString(digestV1(r)) {
		return nil
	}
	return fmt.Errorf("evidence: record %s/%s seq %d does not match its id — content altered", r.Tenant, r.Scope, r.Seq)
}

// CheckLink reports whether cur correctly extends prev. A zero prev admits
// the start of a walk: at genesis that is seq 1 with an empty Prev, and after
// a compaction it is the summary itself, which names the pruned head it
// stands for — so a removed head without a summary is caught the same way a
// removed middle is. A record following a summary may name either the summary
// (sealed after the compaction) or the pruned head the summary carries as its
// own Prev (sealed before it) — two spellings of the same seam.
func CheckLink(prev, cur Record) error {
	if prev.Seq == 0 && cur.Kind == KindCompaction {
		return nil
	}
	wantSeq := prev.Seq + 1
	if cur.Seq != wantSeq {
		return fmt.Errorf("evidence: %s/%s seq %d follows seq %d — record(s) missing", cur.Tenant, cur.Scope, cur.Seq, prev.Seq)
	}
	if prev.Kind == KindCompaction && cur.Prev == prev.Prev {
		return nil
	}
	if cur.Prev != prev.ID {
		return fmt.Errorf("evidence: %s/%s seq %d names prev %q, chain head is %q", cur.Tenant, cur.Scope, cur.Seq, cur.Prev, prev.ID)
	}
	return nil
}

// checkStep applies every offline check to one walked record: its ID
// recomputes, a summary stands only at the walk's start and is internally
// consistent, and the link from prev holds.
func checkStep(prev, cur Record) error {
	if err := CheckRecord(cur); err != nil {
		return err
	}
	if cur.Kind == KindCompaction {
		if prev.Seq != 0 {
			return fmt.Errorf("evidence: %s/%s seq %d is a summary mid-chain — a summary stands only at a scope's start", cur.Tenant, cur.Scope, cur.Seq)
		}
		if _, err := ParseCompaction(cur); err != nil {
			return err
		}
	}
	return CheckLink(prev, cur)
}

// VerifyRecords checks an in-order slice of one scope's records: every ID
// recomputes and every link holds, seam included. It needs no store and no
// context — this is the offline half of Verify, and what an auditor runs
// against an export.
func VerifyRecords(rs []Record) error {
	var prev Record
	for _, r := range rs {
		if err := checkStep(prev, r); err != nil {
			return err
		}
		prev = r
	}
	return nil
}

// VerifyScope walks a stored scope through the log and applies the offline
// checks. Backends implement Log.Verify by delegating here so the judgement is
// single-sourced: a backend that stored records should not also be the
// authority on whether they verify.
func VerifyScope(ctx context.Context, l Log, t tenant.ID, scope string) error {
	var prev Record
	for r, err := range l.Since(ctx, t, scope, 1) {
		if err != nil {
			return err
		}
		if err := checkStep(prev, r); err != nil {
			return err
		}
		prev = r
	}
	return nil
}
