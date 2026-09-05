package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// This file assembles the auditor's answer. BySubject returns the records
// that name a sampled commit; what an auditor works with is the chain those
// records are links of — each scope walked end to end so the verdict covers
// the whole chain, each link's absence named rather than elided. A chain
// with a missing link must read as a missing link, never as a shorter chain.
//
// The scope prefixes below are the published provenance chain
// (ingress → work item → changeset → checks → promotion), spelled here once
// as the order the audit query presents. The writers own the names; the
// loop-level tests are what hold the two to each other.

// Chain is everything recorded about one subject, assembled for an auditor:
// the spans that name it, in the published chain's order, and every gap the
// records show, named.
type Chain struct {
	Subject string `json:"subject"`
	Spans   []Span `json:"spans"`
	Gaps    []Gap  `json:"gaps,omitempty"`
}

// Span is one scope's contribution to a subject's provenance.
type Span struct {
	Scope string `json:"scope"`
	// Records is the scope's whole chain when Whole says so, else only the
	// records naming the subject. A per-entity chain — workitem, changeset,
	// ingress — is bounded by the thing it is about and embeds whole, because
	// a hash chain proves nothing from the middle: an export re-verifies
	// offline only from its start. A standing scope (checks/<repo>, the
	// ladder) grows without bound and would make one commit's artifact carry
	// a repository's entire history, so it embeds the links alone.
	Records []Record `json:"records"`
	Whole   bool     `json:"whole"`
	// Total counts the scope's records, embedded or not.
	Total int `json:"total"`
	// Names are the sequence numbers of the records that name the subject —
	// the links themselves, distinguished from the chain around them.
	Names []uint64 `json:"names"`
	// Verify is empty when the scope's chain holds end to end, else the first
	// break, named. The whole scope is walked and proven either way — what
	// Whole narrows is only what the span carries, never what it checked.
	Verify string `json:"verify,omitempty"`
	// Compacted is set when the span starts at a summary record: the chain
	// crossed into WORM there, and the receipt — range, object, digest, and
	// retention lock — is the summary's body.
	Compacted *Compaction `json:"compacted,omitempty"`
}

// Gap is a link the published chain expects and the records do not show.
// Named rather than elided: silence here would render a broken chain as a
// shorter one, which is worse than no view at all.
type Gap struct {
	// Link is the chain position that is missing: ingress, run, gate, landing.
	Link string `json:"link"`
	Why  string `json:"why"`
}

// chainRank orders spans the way the published chain reads. Unknown scopes
// sort after the known links, by first appearance.
func chainRank(scope string) int {
	switch {
	case strings.HasPrefix(scope, "ingress/"):
		return 0
	case strings.HasPrefix(scope, "workitem/"):
		return 1
	case strings.HasPrefix(scope, "changeset/"):
		return 2
	case strings.HasPrefix(scope, "checks/"):
		return 3
	case strings.HasPrefix(scope, "ladder/"):
		return 4
	}
	return 5
}

// Provenance assembles the chain for one subject: every scope that names it,
// walked and verified end to end, the origination followed from the work
// item, and the gaps named. A subject nothing names returns an empty chain —
// "nothing is recorded" is an answer, and the renderer's job is to say it
// plainly.
func Provenance(ctx context.Context, l Log, t tenant.ID, subject string) (Chain, error) {
	c := Chain{Subject: subject}
	// Which scopes name the subject, in order of first appearance.
	var scopes []string
	seen := map[string]bool{}
	for r, err := range l.BySubject(ctx, t, subject) {
		if err != nil {
			return Chain{}, err
		}
		if !seen[r.Scope] {
			seen[r.Scope] = true
			scopes = append(scopes, r.Scope)
		}
	}
	if len(scopes) == 0 {
		return c, nil
	}
	spans := make([]Span, 0, len(scopes))
	for _, scope := range scopes {
		s, err := span(ctx, l, t, scope, subject)
		if err != nil {
			return Chain{}, err
		}
		spans = append(spans, s)
	}
	// The work item names its origination; follow it, so the chain starts at
	// the ingress the way the invariant says all work does. Followed rather
	// than queried, because the ingress records name the delivery, not the
	// commit — the link lives in the work item's body.
	for _, s := range spans {
		if !strings.HasPrefix(s.Scope, "workitem/") {
			continue
		}
		ing := ingressOf(s)
		if ing == "" {
			c.Gaps = append(c.Gaps, Gap{Link: "ingress",
				Why: fmt.Sprintf("work item %s records no origination — orphan work, which the ingress invariant says cannot exist", strings.TrimPrefix(s.Scope, "workitem/"))})
			continue
		}
		if seen["ingress/"+ing] {
			continue
		}
		seen["ingress/"+ing] = true
		is, err := span(ctx, l, t, "ingress/"+ing, subject)
		if err != nil {
			return Chain{}, err
		}
		if is.Total == 0 {
			c.Gaps = append(c.Gaps, Gap{Link: "ingress",
				Why: fmt.Sprintf("work item %s names ingress %s and no such chain exists — the origination is missing", strings.TrimPrefix(s.Scope, "workitem/"), ing)})
			continue
		}
		spans = append(spans, is)
	}
	sort.SliceStable(spans, func(i, j int) bool {
		return chainRank(spans[i].Scope) < chainRank(spans[j].Scope)
	})
	c.Spans = spans
	c.Gaps = append(c.Gaps, gaps(spans, subject)...)
	return c, nil
}

// boundedScope reports a per-entity chain, bounded by the thing it is about
// — the scopes the retention sweep deliberately never compacts, and the ones
// an export can afford to carry whole.
func boundedScope(scope string) bool {
	return strings.HasPrefix(scope, "workitem/") ||
		strings.HasPrefix(scope, "changeset/") ||
		strings.HasPrefix(scope, "ingress/")
}

// span walks one scope end to end in a single pass: every link checked with
// the same judgement Verify applies, the records naming the subject marked,
// the WORM boundary read off a leading summary — and the records kept whole
// or narrowed to the links, as the scope's shape affords.
func span(ctx context.Context, l Log, t tenant.ID, scope, subject string) (Span, error) {
	s := Span{Scope: scope, Whole: boundedScope(scope)}
	var prev Record
	for r, err := range l.Since(ctx, t, scope, 1) {
		if err != nil {
			return Span{}, err
		}
		if s.Verify == "" {
			if err := checkStep(prev, r); err != nil {
				s.Verify = err.Error()
			}
		}
		prev = r
		if s.Total == 0 && r.Kind == KindCompaction {
			if comp, err := ParseCompaction(r); err == nil {
				s.Compacted = &comp
			}
		}
		s.Total++
		if s.Whole || r.Subject == subject {
			s.Records = append(s.Records, r)
		}
		if r.Subject == subject {
			s.Names = append(s.Names, r.Seq)
		}
	}
	return s, nil
}

// ingressOf reads the origination a work item's records name. The state
// records carry the whole item as their body; the shape is read structurally
// rather than by kind, so a narrative record with no item in it simply says
// nothing.
func ingressOf(s Span) string {
	ing := ""
	for _, r := range s.Records {
		var body struct {
			Ingress string `json:"ingress"`
		}
		if json.Unmarshal(r.Body, &body) == nil && body.Ingress != "" {
			ing = body.Ingress
		}
	}
	return ing
}

// gaps names the links the records claim and do not show. Each rule is
// conditional on what IS recorded: a commit that was built and parked owes
// nobody a landing, but a landing owes the chain a verdict, and a settled
// item owes it the landing it settled at.
func gaps(spans []Span, subject string) []Gap {
	var haveItem, haveBuilt, haveGate, haveLand, haveDone bool
	for _, s := range spans {
		if strings.HasPrefix(s.Scope, "workitem/") {
			haveItem = true
		}
		for _, r := range s.Records {
			if r.Subject != subject {
				continue
			}
			switch r.Kind {
			case "built", "build-resumed", "build-rebased":
				haveBuilt = true
			case "gate":
				haveGate = true
			case "land":
				haveLand = true
			case "done":
				haveDone = true
			}
		}
	}
	var out []Gap
	if !haveItem {
		out = append(out, Gap{Link: "work item",
			Why: "no work item's chain names this commit — the run that produced it is not on the record"})
	} else if !haveBuilt {
		out = append(out, Gap{Link: "run",
			Why: "no build record names this commit — the work item's chain never shows a run producing it"})
	}
	if (haveLand || haveDone) && !haveGate {
		out = append(out, Gap{Link: "gate",
			Why: "this commit landed with no gate verdict on the record — nothing shows it was judged"})
	}
	if haveDone && !haveLand {
		out = append(out, Gap{Link: "landing",
			Why: "the work item settled done at this commit but no landing names it — the changeset → commit link is missing"})
	}
	return out
}
