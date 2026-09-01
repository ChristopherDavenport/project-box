package evidence

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// These tests cover the offline judgements the conformance suite cannot
// reach: a store never yields a broken prefix, so Summarize's refusals — and
// the distinction between a seam and a chopped chain — are asserted here on
// hand-sealed records.

func chain(t *testing.T, n int) []Record {
	t.Helper()
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var rs []Record
	prev := ""
	for i := range n {
		r, err := Seal(Record{
			Tenant: tenant.ID("t1"), Scope: "scope",
			Seq: uint64(i + 1), Prev: prev,
			Kind: "test", Subject: fmt.Sprintf("sha-%d", i), Actor: "a",
			Body: []byte(`{"probe":true}`),
			At:   at.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
		rs = append(rs, r)
		prev = r.ID
	}
	return rs
}

func testReceipt(rs []Record) Export {
	last := rs[len(rs)-1]
	return Export{
		Tenant: last.Tenant, Scope: last.Scope,
		From: rs[0].Seq, To: last.Seq, Head: last.ID,
		URI: "mem://test", Digest: "d", At: last.At,
	}
}

// summarize seals a summary over rs the way a backend would.
func summarize(t *testing.T, rs []Record) Record {
	t.Helper()
	sum, err := Summarize(rs, testReceipt(rs), "compactor")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	sum.At = rs[len(rs)-1].At.Add(time.Hour)
	sum, err = Seal(sum)
	if err != nil {
		t.Fatalf("seal summary: %v", err)
	}
	return sum
}

func TestSummarizeRefusesWhatMustNotFlatten(t *testing.T) {
	rs := chain(t, 4)

	if _, err := Summarize(nil, Export{}, "compactor"); err == nil {
		t.Fatal("empty prefix must be refused")
	}

	// A prefix that does not verify is refused, never flattened — compaction
	// must not launder tampering.
	tampered := append([]Record(nil), rs...)
	tampered[1].Body = []byte("altered")
	if _, err := Summarize(tampered, testReceipt(tampered), "compactor"); err == nil {
		t.Fatal("tampered prefix must be refused")
	}
	removed := []Record{rs[0], rs[2], rs[3]}
	if _, err := Summarize(removed, testReceipt(removed), "compactor"); err == nil {
		t.Fatal("gapped prefix must be refused")
	}

	// A receipt must pin exactly the pruned run.
	for name, mangle := range map[string]func(*Export){
		"wrong-head":   func(e *Export) { e.Head = rs[0].ID },
		"wrong-range":  func(e *Export) { e.To = 9 },
		"wrong-tenant": func(e *Export) { e.Tenant = "t2" },
		"wrong-scope":  func(e *Export) { e.Scope = "elsewhere" },
	} {
		exp := testReceipt(rs)
		mangle(&exp)
		if _, err := Summarize(rs, exp, "compactor"); err == nil {
			t.Fatalf("%s receipt must be refused", name)
		}
	}

	// A bare summary has nothing beyond it to summarize.
	sum := summarize(t, rs)
	if _, err := Summarize([]Record{sum}, testReceipt([]Record{sum}), "compactor"); err == nil {
		t.Fatal("a prefix ending at a summary must be refused")
	}
}

func TestSeamRoundTripOffline(t *testing.T) {
	rs := chain(t, 5)
	pruned, suffix := rs[:3], rs[3:]
	sum := summarize(t, pruned)

	c, err := ParseCompaction(sum)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.From != 1 || c.Through != 3 || c.Records != 3 || c.Kinds["test"] != 3 {
		t.Fatalf("summary content: %+v", c)
	}
	if !c.FirstAt.Equal(pruned[0].At) || !c.LastAt.Equal(pruned[2].At) {
		t.Fatalf("summary time bounds: %+v", c)
	}

	hot := append([]Record{sum}, suffix...)
	if err := VerifyRecords(hot); err != nil {
		t.Fatalf("hot side must verify across the seam: %v", err)
	}
	if err := VerifySeam(sum, pruned); err != nil {
		t.Fatalf("worm side must verify: %v", err)
	}
}

func TestChoppedChainWithoutSummaryStillFails(t *testing.T) {
	// The property the whole design exists for: pruning without a seam is
	// still the attack. A chain missing its prefix, with no summary standing
	// for it, must not verify.
	rs := chain(t, 5)
	err := VerifyRecords(rs[3:])
	if err == nil {
		t.Fatal("a chopped chain with no summary must fail verification")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("failure should name the gap: %v", err)
	}
}

func TestSummaryMidChainFails(t *testing.T) {
	// A summary is only admissible at a walk's start. One that appears after
	// other records is a seam where no seam can be.
	rs := chain(t, 4)
	sum := summarize(t, rs[:3])
	if err := VerifyRecords([]Record{rs[0], rs[1], rs[2], sum}); err == nil {
		t.Fatal("summary mid-chain must fail verification")
	}
}

func TestSummaryBodyConsistencyIsChecked(t *testing.T) {
	rs := chain(t, 4)
	sum := summarize(t, rs[:3])

	// Re-sealed with an inconsistent body: the record hashes, but the seam it
	// describes does not exist. ParseCompaction — and therefore any verified
	// walk — must catch each inconsistency.
	for name, body := range map[string]string{
		"not-json":        `not json`,
		"wrong-seat":      `{"from":1,"through":2,"records":2,"export":{"tenant":"t1","scope":"scope","from":1,"to":2,"head":"` + sum.Prev + `"}}`,
		"impossible-from": `{"from":0,"through":3,"records":3,"export":{"tenant":"t1","scope":"scope","from":0,"to":3,"head":"` + sum.Prev + `"}}`,
		"unanchored-head": `{"from":1,"through":3,"records":3,"export":{"tenant":"t1","scope":"scope","from":1,"to":3,"head":"beef"}}`,
		"export-range":    `{"from":1,"through":3,"records":3,"export":{"tenant":"t1","scope":"scope","from":2,"to":3,"head":"` + sum.Prev + `"}}`,
		"export-tenant":   `{"from":1,"through":3,"records":3,"export":{"tenant":"t2","scope":"scope","from":1,"to":3,"head":"` + sum.Prev + `"}}`,
	} {
		forged := sum
		forged.Body = []byte(body)
		forged, err := Seal(forged)
		if err != nil {
			t.Fatalf("%s: seal: %v", name, err)
		}
		if _, err := ParseCompaction(forged); err == nil {
			t.Fatalf("%s: inconsistent summary body must be refused", name)
		}
		if err := VerifyRecords([]Record{forged, rs[3]}); err == nil {
			t.Fatalf("%s: a walk over an inconsistent summary must fail", name)
		}
	}
}
