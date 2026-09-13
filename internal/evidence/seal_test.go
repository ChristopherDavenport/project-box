package evidence

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// v1Record builds a record and seals it by hand under sealDomainV1 — the
// field set before Pid existed — the way every record in a chain written
// before this change is actually sealed on disk, dogfood's live database
// included.
func v1Record(t *testing.T) Record {
	t.Helper()
	r := Record{
		Tenant: tenant.ID("t1"), Scope: "scope",
		Seq: 1, Prev: "",
		Kind: "test", Subject: "sha-1", Actor: "a",
		OnBehalfOf: "person@example.com", Grant: "g1",
		Body: []byte(`{"probe":true}`),
		At:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Pid:  0,
	}
	r.ID = hex.EncodeToString(digestV1(r))
	return r
}

func TestCheckRecordAcceptsAV1SealedRecordWithNoPid(t *testing.T) {
	r := v1Record(t)
	if err := CheckRecord(r); err != nil {
		t.Fatalf("a record sealed under v1 with pid 0 must still verify: %v", err)
	}
}

func TestCheckRecordRejectsAV1SealedRecordTamperedEitherWay(t *testing.T) {
	r := v1Record(t)
	r.Body = []byte(`{"probe":false}`)
	if err := CheckRecord(r); err == nil {
		t.Fatal("a tampered field must fail the v2 digest")
	}
	// Setting Pid on a v1-sealed record must not let it slide back to the v1
	// fallback either: the digest carries the tamper, and Pid != 0 forecloses
	// the fallback in the first place.
	r2 := v1Record(t)
	r2.Pid = 4242
	if err := CheckRecord(r2); err == nil {
		t.Fatal("a v1-sealed record whose Pid was tampered to nonzero must fail both digests")
	}
}

func TestSealAndCheckRecordRoundTripUnderV2WithPidSet(t *testing.T) {
	r, err := Seal(Record{
		Tenant: tenant.ID("t1"), Scope: "scope",
		Seq: 1, Prev: "",
		Kind: "test", Subject: "sha-1", Actor: "a",
		OnBehalfOf: "person@example.com", Grant: "g1",
		Body: []byte(`{"probe":true}`),
		At:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Pid:  4242,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := CheckRecord(r); err != nil {
		t.Fatalf("a freshly sealed record with a nonzero pid must verify: %v", err)
	}
	tampered := r
	tampered.Pid = 1
	if err := CheckRecord(tampered); err == nil {
		t.Fatal("a tampered pid must be caught — pid is part of the v2 digest")
	}
}
