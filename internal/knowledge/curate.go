package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// Draft is one claim a run surfaced, before curation binds and attributes it.
type Draft struct {
	Claim string
	// Path narrows the binding within the item's repo; empty binds to the
	// whole tree at the item's head.
	Path string
}

// Curate promotes a settled run's drafts into knowledge: each claim bound to
// the commit the run landed, scoped to the item's box, attributed to the
// curator and the run. This is the curation job's mechanical half — deciding
// WHICH drafts deserve promotion is judgment, which is what the curator role
// exists for; this function makes the promoted result well-formed, bound,
// and attributed no matter who or what exercised the judgment.
//
// Only a DONE item curates: knowledge from a change that never landed is a
// claim about code that does not exist.
func Curate(ctx context.Context, b *Base, item queue.WorkItem, repoRemote, producer string, drafts []Draft) ([]Record, error) {
	if item.State != queue.StateDone || item.Head == "" {
		return nil, fmt.Errorf("knowledge: item %s is %s — only a landed run curates", item.ID, item.State)
	}
	var out []Record
	for _, d := range drafts {
		if strings.TrimSpace(d.Claim) == "" {
			continue
		}
		r, err := b.Put(ctx, Record{
			Scope:    "box/" + item.Box,
			Claim:    d.Claim,
			Repo:     repoRemote,
			Path:     d.Path,
			Commit:   item.Head,
			Producer: producer,
			Run:      item.ID,
		})
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}
