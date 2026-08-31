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
const sealDomain = "project-box/evidence/v1"

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

// digest hashes the canonical serialization: a domain tag, then every field
// except ID, each length-prefixed so no two field boundaries can collide.
// Length-prefixed concatenation is chosen over JSON because determinism is
// load-bearing here and JSON's is easy to lose (key order, number forms,
// escaping) without any test noticing.
func digest(r Record) []byte {
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
	put([]byte(sealDomain))
	put([]byte(r.Tenant))
	put([]byte(r.Scope))
	putU64(r.Seq)
	put([]byte(r.Prev))
	put([]byte(r.Kind))
	put([]byte(r.Subject))
	put([]byte(r.Actor))
	put([]byte(r.OnBehalfOf))
	put([]byte(r.Grant))
	put(r.Body)
	putU64(uint64(r.At.UTC().UnixNano()))
	put([]byte(r.Trace))
	return h.Sum(nil)
}

// CheckRecord reports whether a record's ID matches its content. This is the
// tamper check: any altered field changes the digest.
func CheckRecord(r Record) error {
	if r.ID != hex.EncodeToString(digest(r)) {
		return fmt.Errorf("evidence: record %s/%s seq %d does not match its id — content altered", r.Tenant, r.Scope, r.Seq)
	}
	return nil
}

// CheckLink reports whether cur correctly extends prev. A zero prev admits
// only the first record of a scope, so a removed head is caught the same way a
// removed middle is.
func CheckLink(prev, cur Record) error {
	wantSeq := prev.Seq + 1
	if cur.Seq != wantSeq {
		return fmt.Errorf("evidence: %s/%s seq %d follows seq %d — record(s) missing", cur.Tenant, cur.Scope, cur.Seq, prev.Seq)
	}
	if cur.Prev != prev.ID {
		return fmt.Errorf("evidence: %s/%s seq %d names prev %q, chain head is %q", cur.Tenant, cur.Scope, cur.Seq, cur.Prev, prev.ID)
	}
	return nil
}

// VerifyRecords checks an in-order slice of one scope's records: every ID
// recomputes and every link holds. It needs no store and no context — this is
// the offline half of Verify, and what an auditor runs against an export.
func VerifyRecords(rs []Record) error {
	var prev Record
	for _, r := range rs {
		if err := CheckRecord(r); err != nil {
			return err
		}
		if err := CheckLink(prev, r); err != nil {
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
		if err := CheckRecord(r); err != nil {
			return err
		}
		if err := CheckLink(prev, r); err != nil {
			return err
		}
		prev = r
	}
	return nil
}
