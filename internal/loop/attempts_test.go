package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// priorAwareBuilder is revisingBuilder plus the one thing these tests need
// and nothing else does: what the ITEM looked like, as loaded fresh from the
// queue, at the moment each build started. acp.Builder loads its item this
// same way (see internal/agent/acp/builder.go's b.item), so this is the
// direct check that a retry's [agent.Prior] and attempt count reached the
// queue before the builder ran again, not just the in-memory item Run holds.
type priorAwareBuilder struct {
	q        *queue.Queue
	tenant   tenant.ID
	n        int
	priors   []*agent.Prior
	attempts []int
}

func (b *priorAwareBuilder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.n++
	item, err := b.q.Load(ctx, b.tenant, ws.Run)
	if err != nil {
		return "", err
	}
	b.priors = append(b.priors, item.Prior)
	b.attempts = append(b.attempts, item.Attempt)
	name := fmt.Sprintf("attempt-%d.txt", b.n)
	if err := os.WriteFile(filepath.Join(ws.Dir, name), []byte(task+"\n"), 0o644); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
		return "", err
	}
	if _, err := sh(ws.Dir, "git", "commit", "-m", fmt.Sprintf("%s (attempt %d)", task, b.n)); err != nil {
		return "", err
	}
	out, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// TestRefusalUnderBudgetRebuildsInClaim is issue #81's headline case: a gate
// refusal under the budget goes straight to the next build in the SAME
// claim, not to the exception queue, and the next build is told it is a
// retry through the carried [agent.Prior] — persisted to the queue itself
// before the builder runs again.
func TestRefusalUnderBudgetRebuildsInClaim(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Attempts = 2
	e.loop.Meter = &capacity.Meter{Store: e.store, Tenant: tn, Actor: "runner-1"}
	calls := 0
	builder := &priorAwareBuilder{q: &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}, tenant: tn}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{failNTimesGate{name: "build-and-test", n: &calls, failFor: 1}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-81a"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-81a", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a refusal under budget should rebuild and land in the same claim, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2 (one refused, one that landed)", builder.n)
	}
	if len(builder.priors) != 2 || builder.priors[0] != nil {
		t.Fatalf("the first build should see no prior: %+v", builder.priors)
	}
	p := builder.priors[1]
	if p == nil {
		t.Fatal("the second build should have loaded the prior the first attempt's refusal left")
	}
	if !strings.Contains(p.Reason, "attempt 2 of 2") || !strings.Contains(p.Reason, "attempt 1 was refused") {
		t.Fatalf("prior reason should state the count plainly: %q", p.Reason)
	}
	if p.Sha == "" || p.Repo != "app" {
		t.Fatalf("prior should name the refused head and repo: %+v", p)
	}
	if builder.attempts[1] != 2 {
		t.Fatalf("the second build should see itself as attempt 2, got %d", builder.attempts[1])
	}
	if item.Attempt != 2 {
		t.Fatalf("item.Attempt = %d, want 2", item.Attempt)
	}

	var kinds []string
	counts := map[string]int{}
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/wi-81a", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, r.Kind)
		counts[r.Kind]++
	}
	if counts["built"] != 2 {
		t.Fatalf("expected two builds on the chain: %v", kinds)
	}
	if counts["gate"] != 2 {
		t.Fatalf("expected two gate verdicts, one under each head: %v", kinds)
	}
	if counts["attempt"] != 1 {
		t.Fatalf("expected exactly one retry-in-claim marker, not one per build: %v", kinds)
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != "done" {
		t.Fatalf("chain should end in done: %v", kinds)
	}

	started := 0
	for r, err := range e.store.Log().Since(ctx, tn, "control/capacity", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "spend" {
			started++
		}
	}
	if started != 2 {
		t.Fatalf("meter started %d times, want 2 — an attempt is a build, and Start fires per build pass", started)
	}
}

// TestLastRefusalParksKeepingTheCommit: a refusal is never a pass by
// exhaustion. The last attempt's commit is kept as a resume point exactly
// like any gate-refusal park's is, and the note names the count.
func TestLastRefusalParksKeepingTheCommit(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Attempts = 2
	calls := 0
	builder := &revisingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{failNTimesGate{name: "build-and-test", n: &calls, failFor: 1000}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-81b"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-81b", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("budget exhausted should park, got %s (%s)", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "refused on attempt 2 of 2") {
		t.Fatalf("park note should name the count: %q", item.Note)
	}
	if item.Built == "" {
		t.Fatal("the last attempt's commit should be kept as a resume point")
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times, want 2 (the full budget, not one and not three)", builder.n)
	}
	if item.Attempt != 2 {
		t.Fatalf("item.Attempt = %d, want 2", item.Attempt)
	}
}

// TestGateInfraErrorSpendsNoAttempt: infrastructure trouble is never a
// judgement the attempt budget can act on, so it must never be retried
// in-claim as a build however much budget remains — it parks, keeping the
// commit (issue #76), once every gate retry this bound allows is spent, and
// the builder never runs again to buy that park (issue #83).
func TestGateInfraErrorSpendsNoAttempt(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Attempts = 3
	e.loop.GateBackoff = func(int) time.Duration { return 0 }
	builder := &countingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{erroringGate{name: "review"}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-81c"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-81c", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked {
		t.Fatalf("infra trouble exhausting its retries should park, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("builder ran %d times, want exactly 1 — infra trouble must never spend budget on a retry", builder.n)
	}
}

// TestFreeRecheckSpendsNoAttempt: the free recheck a resumed park buys costs
// no build, so a budget of exactly one still gets its one real build on a
// fresh claim — not zero, because the recheck consumed it, and not two.
func TestFreeRecheckSpendsNoAttempt(t *testing.T) {
	e := setup(t, []string{"true"})
	ctx := context.Background()
	e.loop.Attempts = 1
	calls := 0
	builder := &revisingBuilder{}
	e.loop.Builder = builder
	e.loop.Gates = func(forge.Workspace) []forge.Gate {
		return []forge.Gate{failNTimesGate{name: "build-and-test", n: &calls, failFor: 1000}}
	}

	ing, _, err := e.router.Open(ctx, issueRoute(), ingress.Delivery{Key: "issue-81d"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	item, _ := q.Submit(ctx, ItemFor(ing, "wi-81d", "add a thing"))

	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "attempt 1 of 1") {
		t.Fatalf("first claim should exhaust its budget of one, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 1 {
		t.Fatalf("builder ran %d times before the first park, want 1", builder.n)
	}

	retried, err := q.Retry(ctx, tn, "wi-81d", "chris", "try again")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Built == "" {
		t.Fatal("a plain retry should keep the resume point")
	}

	item, err = e.loop.Run(ctx, retried)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "attempt 1 of 1") {
		t.Fatalf("the fresh claim's own budget of one should exhaust the same way, got %s (%s)", item.State, item.Note)
	}
	if builder.n != 2 {
		t.Fatalf("builder ran %d times across both claims, want 2 — the free recheck must not have spent the second claim's only attempt", builder.n)
	}
}
