package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// mustNotRunGate fails the test outright if the loop ever asks it to
// evaluate: proof that a placeholder-shaped commit is refused before any
// gate — real or fake — spends anything on it (#130).
type mustNotRunGate struct {
	t    *testing.T
	name string
}

func (g mustNotRunGate) Name() string { return g.name }

func (g mustNotRunGate) Evaluate(context.Context, forge.ChangeSet) (forge.Verdict, error) {
	g.t.Fatal("gate " + g.name + " was asked to evaluate a commit the commit-message check should have refused pre-gate")
	return forge.Verdict{}, nil
}

// placeholderBuilder mints exactly the shape acp.Builder's doneTrailer
// fallback does: a subject that is the task's first line truncated to 72
// characters, and the adapter's trailer as the body.
type placeholderBuilder struct{}

func (placeholderBuilder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if err := os.WriteFile(filepath.Join(ws.Dir, "answer.txt"), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	subject := task
	if i := strings.IndexByte(subject, '\n'); i >= 0 {
		subject = subject[:i]
	}
	if len(subject) > 72 {
		subject = subject[:72]
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", subject+"\n\n"+agent.AdapterTrailer); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// taskEchoingBuilder commits under a subject that is exactly the task's
// first line — the other shape the adapter mints when nobody wrote a
// message, with no trailer at all.
type taskEchoingBuilder struct{}

func (taskEchoingBuilder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if err := os.WriteFile(filepath.Join(ws.Dir, "answer.txt"), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", task); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// emptySubjectBuilder crafts a commit whose first line is blank, via
// commit-tree directly: `git commit -m` applies cleanup=strip and would
// silently drop a leading blank line before it ever reached the loop.
type emptySubjectBuilder struct{}

func (emptySubjectBuilder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	if err := os.WriteFile(filepath.Join(ws.Dir, "answer.txt"), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	tree, err := sh(ws.Dir, "git", "write-tree")
	if err != nil {
		return "", err
	}
	parent, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "commit-tree", strings.TrimSpace(tree),
		"-p", strings.TrimSpace(parent), "-m", "   ", "-m", task)
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(out)
	if _, err := sh(ws.Dir, "git", "reset", "--hard", head); err != nil {
		return "", err
	}
	return forge.Commit(head), nil
}

// gateRecordFor reads the one "gate" evidence record on subject head and
// unmarshals its embedded [forge.Verdict].
func gateRecordFor(t *testing.T, e *env, head string) gateVerdictRecord {
	t.Helper()
	ctx := context.Background()
	for r, err := range e.store.Log().BySubject(ctx, tn, head) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != "gate" {
			continue
		}
		var vr gateVerdictRecord
		if err := json.Unmarshal(r.Body, &vr); err != nil {
			t.Fatalf("unmarshal gate record: %v", err)
		}
		return vr
	}
	t.Fatalf("no gate record found on subject %s", head)
	return gateVerdictRecord{}
}

// TestPlaceholderTrailerCommitIsRefusedPreGate is #130's headline case: a
// commit still carrying the ACP adapter's fallback trailer is refused as a
// zero-cost commit-message verdict, and no real gate — build, test, review —
// is ever asked about it.
func TestPlaceholderTrailerCommitIsRefusedPreGate(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Builder = placeholderBuilder{}
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{mustNotRunGate{t: t, name: "build-and-test"}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-placeholder"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-placeholder", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a placeholder commit should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "commit-message") {
		t.Fatalf("parked reason should name the commit-message gate: %q", item.Note)
	}
	if !strings.Contains(item.Note, agent.AdapterTrailer) {
		t.Fatalf("parked reason should name the placeholder trailer: %q", item.Note)
	}

	vr := gateRecordFor(t, e, item.Built)
	if vr.Gate != "commit-message" || vr.Decision != forge.DecisionFail {
		t.Fatalf("gate record = %+v, want a failing commit-message verdict", vr)
	}
}

// TestEmptySubjectCommitIsRefusedPreGate: a blank subject line is refused
// the same way, whatever produced it.
func TestEmptySubjectCommitIsRefusedPreGate(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Builder = emptySubjectBuilder{}
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{mustNotRunGate{t: t, name: "build-and-test"}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-empty-subject"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-empty-subject", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("an empty-subject commit should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "commit-message") || !strings.Contains(item.Note, "empty") {
		t.Fatalf("parked reason should name the empty subject: %q", item.Note)
	}
}

// TestSubjectEqualToTaskIsRefusedPreGate: a subject that is exactly the
// task's first line is the other shape the adapter mints when nobody wrote
// a message, and is refused the same way even without the trailer.
func TestSubjectEqualToTaskIsRefusedPreGate(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Builder = taskEchoingBuilder{}
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{mustNotRunGate{t: t, name: "build-and-test"}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-echo"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-echo", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("a task-echoing subject should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "commit-message") {
		t.Fatalf("parked reason should name the commit-message gate: %q", item.Note)
	}
}

// TestProperCommitMessageReachesTheGates: a message that carries neither the
// trailer, an empty subject, nor the task's own first line is not a known-bad
// shape, and the loop proceeds to ask its gates exactly as it always has.
func TestProperCommitMessageReachesTheGates(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	asked := 0
	e.loop.Builder = scriptedBuilder{}
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{countingGate{name: "build-and-test", n: &asked}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-proper"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-proper", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a proper commit message should reach the gates and land, got %s (%s)", item.State, item.Note)
	}
	if asked != 1 {
		t.Fatalf("the gate should have been asked exactly once, got %d", asked)
	}
}

// carryingPlaceholderBuilder refuses with the placeholder shape on its
// first call and a proper commit on every call after — the shape a builder
// takes across a retry once it is told, via [agent.Prior], why the last
// attempt was refused.
type carryingPlaceholderBuilder struct {
	q      *queue.Queue
	tenant string
	n      int
	priors []*agent.Prior
}

func (b *carryingPlaceholderBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.n++
	item, err := b.q.Load(ctx, tn, ws.Run)
	if err != nil {
		return "", err
	}
	b.priors = append(b.priors, item.Prior)

	name := fmt.Sprintf("attempt-%d.txt", b.n)
	if err := os.WriteFile(filepath.Join(ws.Dir, name), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if b.n == 1 {
		if _, err := sh(ws.Dir, "git", "commit", "-m", task); err != nil {
			return "", err
		}
	} else {
		if _, err := sh(ws.Dir, "git", "commit", "-m", scriptedCommitSubject(task)); err != nil {
			return "", err
		}
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// TestPlaceholderRefusalCarriesToNextBuildUnderBudget: exactly like any
// other gate refusal under budget (issue #81), a commit-message refusal
// carries to the next build in the SAME claim, and the next builder's
// [agent.Prior] shows the commit-message verdict that refused the last one.
func TestPlaceholderRefusalCarriesToNextBuildUnderBudget(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Attempts = 2
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	builder := &carryingPlaceholderBuilder{q: q}
	e.loop.Builder = builder
	asked := 0
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{countingGate{name: "build-and-test", n: &asked}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-placeholder-budget"})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-placeholder-budget", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a refusal under budget should rebuild and land in the same claim, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2 (one refused pre-gate, one that landed)", builder.n)
	}
	if asked != 1 {
		t.Fatalf("the real gate should run exactly once, on the second (proper) commit: got %d", asked)
	}
	if len(builder.priors) != 2 || builder.priors[0] != nil {
		t.Fatalf("the first build should see no prior: %+v", builder.priors)
	}
	p := builder.priors[1]
	if p == nil {
		t.Fatal("the second build should have loaded the prior the first attempt's refusal left")
	}
	if !strings.Contains(p.Reason, "commit-message") {
		t.Fatalf("prior reason should name the commit-message verdict: %q", p.Reason)
	}
	found := false
	for _, c := range p.Checks {
		if c == "commit-message" {
			found = true
		}
	}
	if !found {
		t.Fatalf("prior checks should carry the commit-message gate: %+v", p.Checks)
	}
}
