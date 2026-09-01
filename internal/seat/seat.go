// Package seat gives agents continuity: a seat is a persistent named
// identity across runs, with its own knowledge scope and a handoff-and-wake
// cycle — the engineering half of model welfare, built to be tested rather
// than assumed.
//
// A session is a workday; a seat is the colleague. The seat's runs write
// handoff notes as they settle, and the next run wakes with its
// predecessor's notes injected as a resolved layer — role, context, task,
// AND continuity, not amnesia. The notes live on the knowledge plane, so
// they inherit its properties: attributed, supersede-not-erase, swept stale
// when the code they bind to moves, and readable only within the granted
// scope — a seat's memory is its own.
package seat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Seat is one persistent agent identity.
type Seat struct {
	Name string
	Base *knowledge.Base
}

// Scope is the seat's private knowledge scope.
func (s *Seat) Scope() string { return "seat/" + s.Name }

// RecordHandoff writes a settled run's notes to the seat's scope. Closure is
// part of finishing, not an afterthought: the loop's settle path calls this,
// so a run that landed always leaves something for its successor. Notes may
// come from any settled state — a parked run's "here is where I got stuck"
// is exactly what the next occupant needs most.
func (s *Seat) RecordHandoff(ctx context.Context, item queue.WorkItem, repoRemote string, notes []string) error {
	for _, n := range notes {
		if strings.TrimSpace(n) == "" {
			continue
		}
		r := knowledge.Record{
			Scope:    s.Scope(),
			Claim:    n,
			Producer: s.Name,
			Run:      item.ID,
		}
		if repoRemote != "" && item.Head != "" {
			r.Repo, r.Commit = repoRemote, item.Head
		}
		if _, err := s.Base.Put(ctx, r); err != nil {
			return fmt.Errorf("seat %s: handoff: %w", s.Name, err)
		}
	}
	return nil
}

// HandoffFrom returns the notes one named run left in this seat's scope, in
// the order they were written.
//
// [Seat.WakeLayer] answers "what has this seat been doing lately", which is
// the right question for the seat's next run and the wrong one for a
// successor that is not it. Repair work is a different item on a different
// line, opened as new work through a route, and what it needs is the attempt
// IT follows rather than the seat's recent history — the same records, keyed
// by run instead of by recency.
//
// The lookup stays inside this seat's scope. A predecessor that sat elsewhere
// carries its facts to the successor and not its notes, because a seat's
// memory is its own: reading another's would be exactly the cross-scope read
// the knowledge plane refuses everywhere else, and it would need a grant to
// be anything but that.
func (s *Seat) HandoffFrom(ctx context.Context, run string) ([]string, error) {
	if run == "" {
		return nil, nil
	}
	records, err := s.Base.Retrieve(ctx, knowledge.Query{Granted: []string{s.Scope()}})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range records {
		if r.Run != run {
			continue
		}
		// Marked, not hidden — same rule as the wake layer. A successor
		// deciding whether its predecessor's note still applies needs to see
		// that the code underneath it has moved.
		if r.Stale {
			out = append(out, r.Claim+" [stale: the code this was about has moved]")
			continue
		}
		out = append(out, r.Claim)
	}
	return out, nil
}

// WakeLayer builds the continuity layer for the seat's next run: its most
// recent handoffs, newest last, as a resolved inline layer. The bool is
// false when the seat has no history yet — a first day, not an error.
//
// The layer is content-addressed like every layer, so continuity is part of
// the agentref: two runs of one seat with different memories are different
// agents, and the evidence says so.
func (s *Seat) WakeLayer(ctx context.Context, n int) (agent.Layer, bool, error) {
	records, err := s.Base.Retrieve(ctx, knowledge.Query{Granted: []string{s.Scope()}})
	if err != nil {
		return agent.Layer{}, false, err
	}
	if len(records) == 0 {
		return agent.Layer{}, false, nil
	}
	if n > 0 && len(records) > n {
		records = records[len(records)-n:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are the seat %q. Your recent handoff notes, oldest first:\n", s.Name)
	for _, r := range records {
		marker := ""
		if r.Stale {
			marker = " [stale: the code this was about has moved]"
		}
		fmt.Fprintf(&b, "- (%s) %s%s\n", r.Run, r.Claim, marker)
	}
	text := b.String()
	sum := sha256.Sum256([]byte(text))
	return agent.Layer{
		Name:         "wake-" + s.Name,
		Scope:        agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: hex.EncodeToString(sum[:])},
		Instructions: text,
	}, true, nil
}
