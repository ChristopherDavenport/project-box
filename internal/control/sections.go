package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// WholeRepository names the wait bucket for a unit whose Sections was empty
// at the moment it waited — it leases every declared section rather than
// one (see [queue.WorkItem.Sections]), so a wait against it is a wait
// against the whole repository, not against any name a repo declared.
// Never a landed bucket: [queue.WorkItem.LandedSections] is computed from
// paths actually changed and never contains this value.
const WholeRepository = "(whole repository)"

// SectionCensus is one repository's per-section fold of the landed chain —
// see [Room.Sections].
type SectionCensus struct {
	Sections []SectionStat `json:"sections"`
	// CoChanges counts how often two sections landed together in one
	// change, drawn from every landing whose LandedSections named two or
	// more.
	CoChanges []SectionCoChange `json:"co_changes,omitempty"`
}

// SectionStat is one section's tally, or a bucket's: "unsectioned" for a
// landed path no declared section covers, or [WholeRepository] for a unit
// that waited holding none.
type SectionStat struct {
	Name string `json:"name"`
	// Landed is how many landings' LandedSections named this section.
	Landed int `json:"landed"`
	// Waits is how many section-wait transitions were attributed to this
	// section — see [Room.Sections].
	Waits int `json:"waits"`
	// WaitSeconds sums the duration of every wait above whose end could be
	// attributed to a following transition record. Nil when none of them
	// could be — never a bare zero, which would read as "waited no time"
	// rather than "unknown" (CLAUDE.md's unknown-never-reads-as-zero rule).
	WaitSeconds *int64 `json:"wait_seconds,omitempty"`
}

// SectionCoChange counts how often a pair of sections landed together.
// Sections is the pair in sorted order, so the same pair always keys the
// same entry regardless of which section a landing named first.
type SectionCoChange struct {
	Sections [2]string `json:"sections"`
	Count    int       `json:"count"`
}

// Sections folds the landed chain into a per-repository census: how many
// landings touched each declared section (plus "unsectioned" and
// [WholeRepository] when the chain actually produced them — never invented),
// how many section-waits were attributed to each and for how long when that
// could be pinned to a following transition, and which sections co-changed
// in one landing.
//
// sections names repo's declared sections. Every one of them appears in the
// result even at zero landings and zero waits: a declared section nothing
// has touched yet is known-zero, not unknown, and only a name the fold
// never declared is missing from the map entirely.
//
// Enumeration walks index/landings exactly as [Room.Sample] does — dedupe by
// Subject — then [queue.Queue.Load] loads each landed item for its folded
// LandedSections and Sections, filtered to repo. Every count here is a
// tally of that one fold: reading the listing a second time some other way
// is what the count-is-a-projection rule forbids.
//
// SectionWaits cannot be read from the settled item — it is zeroed the
// moment a section is granted (see loop.go's RunHolding) — so wait history
// is folded separately, per landed item, from its own workitem/<id> chain:
// see [Room.sectionWaits].
func (r *Room) Sections(ctx context.Context, repo string, sections []string) (SectionCensus, error) {
	stats := map[string]*SectionStat{}
	var order []string
	stat := func(name string) *SectionStat {
		s, ok := stats[name]
		if !ok {
			s = &SectionStat{Name: name}
			stats[name] = s
			order = append(order, name)
		}
		return s
	}
	for _, n := range sections {
		stat(n)
	}

	q := &queue.Queue{Store: r.Store, Runner: r.Actor}
	var coChanges []SectionCoChange
	coIndex := map[[2]string]int{}

	seen := map[string]bool{}
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/landings", 1) {
		if err != nil {
			return SectionCensus{}, err
		}
		if seen[rec.Subject] {
			continue
		}
		seen[rec.Subject] = true
		item, err := q.Load(ctx, r.Tenant, rec.Subject)
		if err != nil {
			return SectionCensus{}, err
		}
		if item.Repo != repo {
			continue
		}
		if len(item.Sections) == 0 {
			// This landing held every declared section to build, denying
			// every section-scoped unit for the length of its run — a cost
			// invisible in LandedSections alone, since what it held and what
			// it touched are different questions.
			stat(WholeRepository).Landed++
		}
		if len(item.LandedSections) > 0 {
			for _, n := range item.LandedSections {
				stat(n).Landed++
			}
			if len(item.LandedSections) >= 2 {
				names := append([]string(nil), item.LandedSections...)
				sort.Strings(names)
				for i := 0; i < len(names); i++ {
					for j := i + 1; j < len(names); j++ {
						pair := [2]string{names[i], names[j]}
						if idx, ok := coIndex[pair]; ok {
							coChanges[idx].Count++
						} else {
							coIndex[pair] = len(coChanges)
							coChanges = append(coChanges, SectionCoChange{Sections: pair, Count: 1})
						}
					}
				}
			}
		}
		if err := r.sectionWaits(ctx, item.ID, stat); err != nil {
			return SectionCensus{}, err
		}
	}

	out := SectionCensus{CoChanges: coChanges}
	for _, n := range order {
		out.Sections = append(out.Sections, *stats[n])
	}
	return out, nil
}

// sectionWaits folds one landed item's own chain for section-wait
// transitions, attributing each to stat. It walks workitem/<id> rather than
// the settled item because SectionWaits is zeroed the instant a section is
// granted (loop.go:602/612) — the settled body says nothing about how long
// the item spent waiting to get there, only the transition records do.
//
// Only state records carry the whole item (see [queue.StateRecord]);
// everything else in this scope is narrative — an authz decision, an
// escalation, a gate verdict — and is skipped rather than unmarshalled as
// one, exactly as [queue.Queue.Load] itself does.
//
// A wait is a state record whose SectionWaits rose over the immediately
// preceding state record's. It is attributed to that record's own Sections
// (empty means [WholeRepository]) and its duration is the gap between that
// record's Record.At and the next state record's — the one sanctioned
// cross-record clock. A wait with no following state record (the walk ends
// on it) is counted without a duration: unknown never reads as zero.
func (r *Room) sectionWaits(ctx context.Context, id string, stat func(string) *SectionStat) error {
	type pendingWait struct {
		names []string
		start time.Time
	}
	var prev *queue.WorkItem
	var pending *pendingWait
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+id, 1) {
		if err != nil {
			return err
		}
		if !queue.StateRecord(rec.Kind) {
			continue
		}
		var w queue.WorkItem
		if err := json.Unmarshal(rec.Body, &w); err != nil {
			return fmt.Errorf("control: item %s record %d (%s): %w", id, rec.Seq, rec.Kind, err)
		}
		if pending != nil {
			d := int64(rec.At.Sub(pending.start).Seconds())
			for _, n := range pending.names {
				s := stat(n)
				s.Waits++
				if s.WaitSeconds == nil {
					s.WaitSeconds = new(int64)
				}
				*s.WaitSeconds += d
			}
			pending = nil
		}
		if prev != nil && w.SectionWaits > prev.SectionWaits {
			names := w.Sections
			if len(names) == 0 {
				names = []string{WholeRepository}
			}
			pending = &pendingWait{names: names, start: rec.At}
		}
		next := w
		prev = &next
	}
	if pending != nil {
		for _, n := range pending.names {
			stat(n).Waits++
		}
	}
	return nil
}
