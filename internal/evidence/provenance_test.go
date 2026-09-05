package evidence

import (
	"context"
	"errors"
	"iter"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// memLog is the smallest Log that can hold a chain: enough to assemble
// provenance from hand-built records without a store. Append seals like a
// backend would; Compact is deliberately absent — compacted spans are seeded
// directly, the way compact_test builds its seams.
type memLog struct {
	scopes map[string][]Record
	order  []string
	at     time.Time
}

func newMemLog() *memLog {
	return &memLog{scopes: map[string][]Record{}, at: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
}

func (m *memLog) Append(ctx context.Context, r Record) (Record, error) {
	rs := m.scopes[r.Scope]
	r.Seq = uint64(len(rs)) + 1
	if len(rs) > 0 {
		r.Prev = rs[len(rs)-1].ID
	}
	m.at = m.at.Add(time.Second)
	r.At = m.at
	sealed, err := Seal(r)
	if err != nil {
		return Record{}, err
	}
	if len(rs) == 0 {
		m.order = append(m.order, r.Scope)
	}
	m.scopes[r.Scope] = append(rs, sealed)
	return sealed, nil
}

// seed places already-sealed records — a compacted span, a tampered one —
// exactly as given.
func (m *memLog) seed(scope string, rs []Record) {
	if _, ok := m.scopes[scope]; !ok {
		m.order = append(m.order, scope)
	}
	m.scopes[scope] = rs
}

func (m *memLog) Head(ctx context.Context, t tenant.ID, scope string) (Record, error) {
	rs := m.scopes[scope]
	if len(rs) == 0 {
		return Record{}, nil
	}
	return rs[len(rs)-1], nil
}

func (m *memLog) Since(ctx context.Context, t tenant.ID, scope string, seq uint64) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		for _, r := range m.scopes[scope] {
			if r.Seq >= seq && !yield(r, nil) {
				return
			}
		}
	}
}

func (m *memLog) BySubject(ctx context.Context, t tenant.ID, subject string) iter.Seq2[Record, error] {
	var out []Record
	for _, scope := range m.order {
		for _, r := range m.scopes[scope] {
			if r.Subject == subject {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return func(yield func(Record, error) bool) {
		for _, r := range out {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func (m *memLog) Verify(ctx context.Context, t tenant.ID, scope string) error {
	return VerifyScope(ctx, m, t, scope)
}

func (m *memLog) Compact(ctx context.Context, t tenant.ID, scope string, through uint64, exp Export, actor string) (Record, error) {
	return Record{}, errors.New("memLog does not compact")
}

const pt = tenant.ID("t1")

func put(t *testing.T, l *memLog, scope, kind, subject, body string) {
	t.Helper()
	if _, err := l.Append(context.Background(), Record{
		Tenant: pt, Scope: scope, Kind: kind, Subject: subject,
		Actor: "runner-1", OnBehalfOf: "chris", Body: []byte(body),
	}); err != nil {
		t.Fatal(err)
	}
}

// landed writes the records the real writers produce for one landed change:
// the origination, the work item's run, the gate, the landing, the settle.
func landed(t *testing.T, l *memLog, sha string) {
	t.Helper()
	put(t, l, "ingress/ing-1", "ingress-open", "delivery/issue-9", `{"route":"issues","key":"issue-9"}`)
	put(t, l, "workitem/wi-1", "queued", "wi-1", `{"id":"wi-1","ingress":"ing-1","state":"pending"}`)
	put(t, l, "workitem/wi-1", "claimed", "wi-1", `{"id":"wi-1","ingress":"ing-1","state":"claimed"}`)
	put(t, l, "workitem/wi-1", "built", sha, "")
	put(t, l, "workitem/wi-1", "gate", sha, `{"Gate":"build-and-test","Decision":"pass"}`)
	put(t, l, "changeset/cs-wi-1", "land", sha, `{"repo":"shop","branch":"main"}`)
	put(t, l, "workitem/wi-1", "done", sha, `{"id":"wi-1","ingress":"ing-1","state":"done","head":"`+sha+`"}`)
}

func TestProvenanceAssemblesTheChainInOrder(t *testing.T) {
	l := newMemLog()
	sha := "aaaa000000000000000000000000000000000000"
	landed(t, l, sha)

	c, err := Provenance(context.Background(), l, pt, sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Gaps) != 0 {
		t.Fatalf("a complete chain reported gaps: %+v", c.Gaps)
	}
	// The published chain's order: the origination first, followed even
	// though no ingress record names the sha.
	var order []string
	for _, s := range c.Spans {
		order = append(order, s.Scope)
		if s.Verify != "" {
			t.Errorf("span %s does not verify: %s", s.Scope, s.Verify)
		}
	}
	want := []string{"ingress/ing-1", "workitem/wi-1", "changeset/cs-wi-1"}
	if len(order) != len(want) {
		t.Fatalf("spans %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("spans %v, want %v", order, want)
		}
	}
	// The work item span carries its whole chain, with the subject's links
	// marked: the audit reads the change, the export verifies the scope.
	wi := c.Spans[1]
	if len(wi.Records) != 5 {
		t.Fatalf("work item span holds %d records, want the whole chain (5)", len(wi.Records))
	}
	if len(wi.Names) != 3 { // built, gate, done
		t.Fatalf("work item span names %v, want 3 links", wi.Names)
	}
}

func TestProvenanceNamesTheMissingLink(t *testing.T) {
	ctx := context.Background()
	sha := "bbbb000000000000000000000000000000000000"

	find := func(c Chain, link string) bool {
		for _, g := range c.Gaps {
			if g.Link == link {
				return true
			}
		}
		return false
	}

	t.Run("done without a landing", func(t *testing.T) {
		l := newMemLog()
		put(t, l, "ingress/ing-1", "ingress-open", "delivery/k", `{}`)
		put(t, l, "workitem/wi-1", "queued", "wi-1", `{"ingress":"ing-1"}`)
		put(t, l, "workitem/wi-1", "built", sha, "")
		put(t, l, "workitem/wi-1", "gate", sha, `{"Decision":"pass"}`)
		put(t, l, "workitem/wi-1", "done", sha, `{"ingress":"ing-1","state":"done"}`)
		c, err := Provenance(ctx, l, pt, sha)
		if err != nil {
			t.Fatal(err)
		}
		if !find(c, "landing") {
			t.Fatalf("a done with no land must name the landing gap, got %+v", c.Gaps)
		}
	})

	t.Run("landing without a verdict", func(t *testing.T) {
		l := newMemLog()
		put(t, l, "ingress/ing-1", "ingress-open", "delivery/k", `{}`)
		put(t, l, "workitem/wi-1", "queued", "wi-1", `{"ingress":"ing-1"}`)
		put(t, l, "workitem/wi-1", "built", sha, "")
		put(t, l, "changeset/cs-wi-1", "land", sha, `{}`)
		c, err := Provenance(ctx, l, pt, sha)
		if err != nil {
			t.Fatal(err)
		}
		if !find(c, "gate") {
			t.Fatalf("a landing with no verdict must name the gate gap, got %+v", c.Gaps)
		}
	})

	t.Run("no work item at all", func(t *testing.T) {
		l := newMemLog()
		put(t, l, "changeset/cs-x", "land", sha, `{}`)
		c, err := Provenance(ctx, l, pt, sha)
		if err != nil {
			t.Fatal(err)
		}
		if !find(c, "work item") {
			t.Fatalf("a chain with no work item must say so, got %+v", c.Gaps)
		}
	})

	t.Run("origination that does not exist", func(t *testing.T) {
		l := newMemLog()
		put(t, l, "workitem/wi-1", "queued", "wi-1", `{"ingress":"ing-gone"}`)
		put(t, l, "workitem/wi-1", "built", sha, "")
		c, err := Provenance(ctx, l, pt, sha)
		if err != nil {
			t.Fatal(err)
		}
		if !find(c, "ingress") {
			t.Fatalf("a named ingress with no chain must be a gap, got %+v", c.Gaps)
		}
	})

	t.Run("orphan work", func(t *testing.T) {
		l := newMemLog()
		put(t, l, "workitem/wi-1", "built", sha, "")
		c, err := Provenance(ctx, l, pt, sha)
		if err != nil {
			t.Fatal(err)
		}
		if !find(c, "ingress") {
			t.Fatalf("an item with no origination must be a gap, got %+v", c.Gaps)
		}
	})
}

func TestProvenanceVerifiesEachSpan(t *testing.T) {
	l := newMemLog()
	sha := "cccc000000000000000000000000000000000000"
	landed(t, l, sha)
	// Tamper with a stored record: the span must say so, not render anyway.
	l.scopes["workitem/wi-1"][2].Body = []byte("altered after the fact")

	c, err := Provenance(context.Background(), l, pt, sha)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range c.Spans {
		if s.Scope == "workitem/wi-1" {
			if s.Verify == "" {
				t.Fatal("a tampered span verified clean")
			}
			return
		}
	}
	t.Fatal("tampered span missing from the chain")
}

// TestProvenanceElidesAStandingScopeButStillProvesIt: checks/<repo> grows
// without bound, so its span carries only the links — and the whole scope is
// still walked, so tampering with a record the span does not embed is caught
// anyway. What Whole narrows is what the span carries, never what it checked.
func TestProvenanceElidesAStandingScopeButStillProvesIt(t *testing.T) {
	sha := "eeee000000000000000000000000000000000000"
	build := func() *memLog {
		l := newMemLog()
		landed(t, l, sha)
		put(t, l, "checks/app", "check", "othersha-1", `{"green":true}`)
		put(t, l, "checks/app", "check", sha, `{"green":true}`)
		put(t, l, "checks/app", "check", "othersha-2", `{"green":false}`)
		return l
	}

	c, err := Provenance(context.Background(), build(), pt, sha)
	if err != nil {
		t.Fatal(err)
	}
	var ck *Span
	for i := range c.Spans {
		if c.Spans[i].Scope == "checks/app" {
			ck = &c.Spans[i]
		}
	}
	if ck == nil {
		t.Fatalf("checks span missing: %+v", c.Spans)
	}
	if ck.Whole || ck.Total != 3 || len(ck.Records) != 1 || ck.Verify != "" {
		t.Fatalf("a standing scope must carry only the links and still verify: %+v", ck)
	}

	// Tamper with a record the span will not embed: the verdict must break.
	l := build()
	l.scopes["checks/app"][0].Body = []byte("altered")
	c, err = Provenance(context.Background(), l, pt, sha)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range c.Spans {
		if s.Scope == "checks/app" && s.Verify == "" {
			t.Fatal("a tampered standing scope verified clean")
		}
	}
}

func TestProvenanceReportsTheWORMCrossing(t *testing.T) {
	// A compacted scope: summary standing for the pruned prefix, live records
	// after it — built exactly the way compact_test builds its seams.
	rs := chain(t, 6)
	sum := summarize(t, rs[:4])
	l := newMemLog()
	l.seed("scope", append([]Record{sum}, rs[4:]...))

	c, err := Provenance(context.Background(), l, pt, "sha-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spans) != 1 {
		t.Fatalf("spans: %+v", c.Spans)
	}
	s := c.Spans[0]
	if s.Verify != "" {
		t.Fatalf("a well-formed seam must verify: %s", s.Verify)
	}
	if s.Compacted == nil {
		t.Fatal("the WORM crossing is not reported")
	}
	if s.Compacted.Export.URI != "mem://test" || s.Compacted.Through != 4 {
		t.Fatalf("the crossing must carry the receipt: %+v", s.Compacted)
	}
}

func TestProvenanceOfNothingIsAnEmptyChain(t *testing.T) {
	c, err := Provenance(context.Background(), newMemLog(), pt, "dddd00")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spans) != 0 || len(c.Gaps) != 0 {
		t.Fatalf("an unknown subject must answer empty, got %+v", c)
	}
	if !strings.Contains(c.Subject, "dddd00") {
		t.Fatalf("the answer must still name what was asked: %+v", c)
	}
}
