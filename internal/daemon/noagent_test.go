package daemon

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// writeAgentlessCfg writes a box.yaml with no `agent:` anywhere and a check
// per repo, so a repo can be made to park by configuration rather than by
// arranging a failure.
func writeAgentlessCfg(t *testing.T, path, db, root string, checks map[string]string, remotes map[string]string, budget string) {
	t.Helper()
	var repos strings.Builder
	for name, remote := range remotes {
		// Gated, because the optimistic lane lands a red check and summons a
		// repair rather than parking, and this test wants the exception
		// queue rather than the repair route.
		fmt.Fprintf(&repos, `
      - name: %s
        remote: %s
        lane: gated
        build: '%s'
        check: '%s'`, name, remote, buildCmd, checks[name])
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {%s}
boxes:
  - name: shop
    repos:%s
`, db, root, budget, repos.String())
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTheFactoryRunsWithNoAgentInIt is the invariant as a negative test:
// reliability-critical paths are imperative Go, and an agent occupies only
// the Builder seat, whose failure parks work.
//
// The property erodes invisibly, which is why it is asserted rather than
// stated. Gas Town's formula-driven maintenance dogs were silently broken
// for months — the ticker fired, and no agent existed to interpret it, and
// nothing errored. Every pressure here runs one way: the curation job and
// the continuity handoff both sit near paths that matter, and "just have an
// agent do it" is always the cheaper-looking option at the moment of
// writing. The first person to move a standing chore onto the agent plane
// should fail this test rather than discover it in a quiet quarter.
//
// So: zero agents configured, no ACP runtime assembled, and the daemon must
// still tick, drain, park, escalate, refuse, and seal.
func TestTheFactoryRunsWithNoAgentInIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	goodRem, sourRem := bare(t, "good"), bare(t, "sour")

	writeAgentlessCfg(t, cfgPath, db, root,
		// `false` is a check that always fails: the Builder seat does its
		// work and judgement refuses it, which is the park this test needs
		// and the one an operator actually gets.
		map[string]string{"good": "test -f out.txt", "sour": "false"},
		map[string]string{"good": goodRem, "sour": sourRem},
		"max_concurrent: 2, max_tokens_per_window: 1000, window: 1h")

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	// 1. No agent runtime was assembled. This is the structural half, and the
	// one that catches the erosion: a builder that is an *acp.Builder here
	// means an agent got onto a path this config never asked for one on.
	loops, _, _, kinds, err := d.assemble(ctx, d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 2 {
		t.Fatalf("executors: got %d, want 2", len(loops))
	}
	// And the decomposition stage on the critical path of every intake is
	// deterministic Go too. A judgement planner here would make ADMITTING work
	// depend on an agent being available, which is the invariant one level
	// further up the loop than the builder seat.
	for _, name := range plan.Default().Names() {
		d, err := plan.Default().Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Replayable() {
			t.Fatalf("default decomposer %s is not replayable — intake would depend on a judgement", name)
		}
	}
	if _, err := kinds.Lookup("shop", kind.SeedIssue); err != nil {
		t.Fatalf("the seeded issue declaration is missing: %v", err)
	}
	for name, lp := range loops {
		if _, isAgent := lp.Builder.(*acp.Builder); isAgent {
			t.Fatalf("%s: an ACP builder was assembled for a config with no agent", name)
		}
		if _, isShell := lp.Builder.(ShellBuilder); !isShell {
			t.Fatalf("%s: builder is %T, want ShellBuilder", name, lp.Builder)
		}
		// Continuity is an agent-plane feature. Its ABSENCE must be
		// ordinary — a settle with nowhere to write notes still settles.
		if lp.Handoff != nil {
			t.Fatalf("%s: a handoff hook with no seat behind it", name)
		}
	}

	// 2. It ticks, and the pump drains. The whole loop — intake, claim,
	// build, gate, land — with nothing agent-shaped in it.
	res, err := c.OpenIngress(ctx, "shop-good", "good-1", "a change that lands", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone {
		t.Fatalf("work did not land without an agent: %+v %v", item, err)
	}
	if out := mustGit(t, goodRem, "log", "--format=%s", "refs/heads/main"); !strings.Contains(out, "work") {
		t.Fatalf("the remote never moved:\n%s", out)
	}

	// 3. Exceptions still surface, age, and leave the room. A failing gate
	// parks; the room folds it; the escalator tells somebody out of band.
	sour, err := c.OpenIngress(ctx, "shop-sour", "sour-1", "a change that parks", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if it, err := c.Item(ctx, sour.Item); err != nil || it.State != queue.StateParked {
		t.Fatalf("a failed gate should park: %+v %v", it, err)
	}

	var told []control.Notice
	// The clock moves rather than the objective shrinking: an exception's age
	// is whole seconds, so a just-parked item is never overdue against any
	// objective an operator would actually set.
	room := &control.Room{Store: d.store, Tenant: d.tenant, Actor: "boxd",
		Objective: time.Minute,
		Now:       func() time.Time { return time.Now().Add(5 * time.Minute) }}
	esc := &control.Escalator{Room: room,
		Notify: control.NotifierFunc(func(_ context.Context, n control.Notice) error {
			told = append(told, n)
			return nil
		})}
	exceptions, err := room.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(exceptions) != 1 || exceptions[0].ID != sour.Item {
		t.Fatalf("exception queue: %+v", exceptions)
	}
	if !exceptions[0].Overdue {
		t.Fatal("an exception past its objective must report overdue")
	}
	if _, err := esc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0].Item != sour.Item {
		t.Fatalf("nobody was told out of band: %+v", told)
	}

	// 4. Budgets still refuse. Nothing in this configuration reports a token
	// cost — there is no stream to report one — so the charge is made by
	// hand, which is the honest shape of the claim: the brake is Go, and it
	// does not need an agent to exist in order to bite.
	d.meter.Charge(ctx, "manual", capacity.Spend{Total: 2000, Reported: true})
	if !d.meter.Report().Refusing {
		t.Fatalf("the meter is over its ceiling and not refusing: %+v", d.meter.Report())
	}
	after, err := c.OpenIngress(ctx, "shop-good", "good-2", "a change the budget must refuse", "")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("the budget is exhausted and the pump settled %d items", n)
	}
	if it, err := c.Item(ctx, after.Item); err != nil || it.State != queue.StatePending {
		t.Fatalf("over-budget work must wait, not run: %+v %v", it, err)
	}

	// 5. Evidence still seals — every scope the run above touched, and the
	// control scopes the standing chores write to.
	for _, scope := range []string{
		"workitem/" + res.Item, "workitem/" + sour.Item, "workitem/" + after.Item,
		"index/ingress", "index/pending", "control/claims", "control/capacity",
	} {
		if err := d.store.Log().Verify(ctx, tenant.ID("acme"), scope); err != nil {
			t.Fatalf("%s does not verify: %v", scope, err)
		}
	}

	// 6. The standing chores are chores, not agent errands. Both run to
	// completion with no runtime configured, which is what makes the
	// housekeeping tick above more than a timer.
	if err := d.mirror.Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := d.escalator.Sweep(ctx); err != nil {
		t.Fatalf("escalation sweep: %v", err)
	}
}
