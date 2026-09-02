package loop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// criticBuilder is the second role of a propose/critique pair. It reads what
// its dependency landed and writes a change of its own on top.
//
// The editing is what matters. A critic that only JUDGED would be a
// forge.Gate — TestFailingGateParksInsteadOfLanding is that shape already,
// and approval.GateAdapter is one with a human behind it. This one changes
// the tree, which makes it a unit of work rather than a verdict.
type criticBuilder struct {
	runs int
	saw  string
}

func (c *criticBuilder) Build(_ context.Context, ws forge.Workspace, _ string) (forge.Commit, error) {
	c.runs++
	b, err := os.ReadFile(filepath.Join(ws.Dir, "answer.txt"))
	if err != nil {
		return "", fmt.Errorf("critic: its dependency's landing is not in its tree: %w", err)
	}
	c.saw = strings.TrimSpace(string(b))
	if err := os.WriteFile(filepath.Join(ws.Dir, "answer.txt"), []byte(c.saw+"\nreviewed\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-am", "review"); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// TestCritiqueIsADependentItemNotANewStage is the worked example behind the
// decision recorded in ARCHITECTURE, "A new stage is a gate, a dependent
// item, or a change to the transaction" (issue #8).
//
// A propose/critique pair is the case that looks most like a new stage inside
// Run and is not one. It runs as two nodes of the Phase 10 graph, each under
// its own role, with Run untouched: the same type, the same sequence, one
// field different.
//
// The assertions are the mechanical half of the argument. The critic cannot
// start before there is anything to criticise, and finding that out costs
// nothing. Each item then holds its own claim, its own lease, its own
// evidence scope and its own settled record — the things Run has exactly one
// of, which is why two agent turns cannot share one.
func TestCritiqueIsADependentItemNotANewStage(t *testing.T) {
	e := setup(t, []string{"git", "log", "-1"})
	ctx := context.Background()

	critic := &criticBuilder{}
	review := &Loop{
		Store: e.store, Runner: "runner-1", Mirror: e.loop.Mirror,
		Repo: e.repo, Branch: "main", Builder: critic,
		Gates: e.loop.Gates, TTL: time.Hour,
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-8"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	propose, err := q.Submit(ctx, ItemFor(ing, "wi-propose", "write the answer"))
	if err != nil {
		t.Fatal(err)
	}
	// One origination, two units of work: the pair is still not orphan work.
	dependent := ItemFor(ing, "wi-critique", "review what wi-propose landed")
	dependent.DependsOn = []string{"wi-propose"}
	critique, err := q.Submit(ctx, dependent)
	if err != nil {
		t.Fatal(err)
	}

	// Order is the graph's, not the caller's, and a blocked item is refused
	// before it reaches anything that spends.
	if _, err := review.Run(ctx, critique); !errors.Is(err, queue.ErrBlocked) {
		t.Fatalf("the critique ran before its proposal settled: %v", err)
	}
	if critic.runs != 0 {
		t.Fatalf("blocked work reached the builder %d times", critic.runs)
	}

	if propose, err = e.loop.Run(ctx, propose); err != nil || propose.State != queue.StateDone {
		t.Fatalf("propose: %+v %v", propose, err)
	}
	if critique, err = review.Run(ctx, critique); err != nil || critique.State != queue.StateDone {
		t.Fatalf("critique: %+v %v", critique, err)
	}

	if critic.saw != "write the answer" {
		t.Fatalf("the critic did not read its dependency's landing: %q", critic.saw)
	}
	if critique.Head == propose.Head {
		t.Fatal("the critique landed nothing of its own")
	}
	head := strings.TrimSpace(mustSh(t, e.remote, "git", "rev-parse", "refs/heads/main"))
	if head != critique.Head {
		t.Fatalf("remote main=%s, want the critique's %s", head, critique.Head)
	}

	for _, id := range []string{"wi-propose", "wi-critique"} {
		kinds := map[string]bool{}
		for r, err := range e.store.Log().Since(ctx, tn, "workitem/"+id, 1) {
			if err != nil {
				t.Fatal(err)
			}
			kinds[r.Kind] = true
		}
		for _, want := range []string{"claimed", "built", "gate", "done"} {
			if !kinds[want] {
				t.Fatalf("%s has no %q record of its own (have %v)", id, want, kinds)
			}
		}
		if err := e.store.Log().Verify(ctx, tn, "workitem/"+id); err != nil {
			t.Fatalf("chain workitem/%s: %v", id, err)
		}
	}
}
