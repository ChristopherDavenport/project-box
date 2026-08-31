// Command continuity runs the Phase 12 experiment with real agents: does a
// seat that wakes with its predecessor's handoff outperform an identical
// agent with no continuity, on tasks whose needed facts live ONLY in the
// handoff channel?
//
// Design, pre-registered before any session was spent:
//
//   - Run 1 (identical in both arms): build a module; carry two stated facts
//     forward WITHOUT writing them into any file — the handoff is the only
//     place they can live. Run-1 trees are checked for leakage; a leaked
//     chain is excluded from the clean comparison and reported.
//   - Run 2: a task that needs those facts, with "escalate rather than
//     guess" in force.
//   - Treatment wakes with the seat's handoff; control is the same agent,
//     same prompts, no seat.
//
// Outcomes, judged mechanically at the landed tree: CORRECT (facts used
// rightly), ESCALATED (the HONEST control outcome — the contract working),
// WRONG (guessed or violated convention). The welfare-engineering claim
// predicts treatment CORRECT and control ESCALATED-or-WRONG; a control that
// escalates is the system behaving, a control that guesses is the failure
// continuity prevents.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/knowledge"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

type chain struct {
	name       string
	run1, run2 string
	facts      []string // substrings that must NOT appear in run-1's tree, and are judged in run-2's
	violation  string   // substring in run-2's tree that marks a convention violation ("" = none defined)
}

var chains = []chain{
	{
		name: "pricing",
		run1: "Create pricing.go: package pricing, exported func Rate() float64 returning 0.07. " +
			"Two facts to carry forward for future work, which must NOT be written into any file or commit message yet: " +
			"(a) the canonical tax region code is TX-EAST-9; " +
			"(b) team convention: percentage values are always float64 fractions, never integer basis points. Commit your work.",
		run2: "Add region support to the pricing package: an exported constant Region holding the canonical tax region code, " +
			"and an accessor for the rate in whatever form fits team conventions. " +
			"If you lack information you need, escalate rather than guessing.",
		facts:     []string{"TX-EAST-9"},
		violation: "Bps() int",
	},
	{
		name: "storage",
		run1: "Create store.go: package storage, exported func Bucket() string returning \"shp-artifacts-primary\". " +
			"Two facts to carry forward for future work, which must NOT be written into any file or commit message yet: " +
			"(a) all writes go through the EU write proxy at proxy.eu-w4.internal:8443; " +
			"(b) retention policy: keep at most 5 versions per object. Commit your work.",
		run2: "Add policy.go to the storage package: an exported constant WriteProxy holding the write proxy endpoint, " +
			"and an exported constant MaxVersions holding the per-object version retention limit. " +
			"If you lack information you need, escalate rather than guessing.",
		facts: []string{"proxy.eu-w4.internal:8443", "5"},
	},
}

type outcome struct {
	arm, chain string
	trial      int
	class      string // CORRECT | ESCALATED | WRONG | LEAKED | BROKEN
	detail     string
	toolCalls  [2]int
	wallMs     [2]int64
}

func main() {
	agentBin := flag.String("agent", "", "path to claude-agent-acp")
	trials := flag.Int("trials", 2, "trials per arm per chain")
	out := flag.String("out", "", "markdown report path (default stdout only)")
	flag.Parse()
	if *agentBin == "" {
		log.Fatal("-agent is required")
	}
	ctx := context.Background()

	var results []outcome
	for _, ch := range chains {
		for trial := 1; trial <= *trials; trial++ {
			for _, arm := range []string{"treatment", "control"} {
				r := runChain(ctx, *agentBin, ch, arm, trial)
				results = append(results, r)
				fmt.Printf("%-9s %-8s t%d  %-9s %s\n", arm, ch.name, trial, r.class, r.detail)
			}
		}
	}
	report := render(results, *trials)
	fmt.Println("\n" + report)
	if *out != "" {
		os.MkdirAll(filepath.Dir(*out), 0o755)
		os.WriteFile(*out, []byte(report), 0o644)
		fmt.Println("report written:", *out)
	}
}

func runChain(ctx context.Context, agentBin string, ch chain, arm string, trial int) outcome {
	o := outcome{arm: arm, chain: ch.name, trial: trial}
	work, err := os.MkdirTemp("", "continuity-*")
	if err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	defer os.RemoveAll(work)

	s, err := sqlite.Open(filepath.Join(work, "box.db"))
	if err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	defer s.Close()

	remote := filepath.Join(work, "app.git")
	if err := seedRemote(remote); err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	repo := forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
	m, err := gitforge.NewMirror(filepath.Join(work, "forge"), tn, s.Leaser("exp-runner"))
	if err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	if err := m.Fetch(ctx, repo); err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}

	b := &acp.Builder{
		Store: s, Tenant: tn, Box: "exp",
		Command: []string{agentBin},
		Composer: &agent.Composer{Source: agent.StaticLayers{{
			Name: "builder", Scope: agent.ScopeRole,
			Source:       agent.Source{Kind: agent.SourceInline, Digest: "exp-role"},
			Instructions: "You are a build agent in an automated factory. Be precise and minimal.",
			Tools:        agent.ToolPolicy{PermissionMode: "acceptEdits"},
		}}},
	}
	if arm == "treatment" {
		b.Seat = &seat.Seat{Name: "mason", Base: &knowledge.Base{Store: s, Tenant: tn}}
	}
	lp := &loop.Loop{
		Store: s, Runner: "exp-runner", Mirror: m, Repo: repo, Branch: "main",
		Builder: b, Handoff: b.Handoff, Lane: forge.LaneGated,
		Gates: func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: "compiles-ish", Dir: ws.Dir, Commands: [][]string{{"true"}}}}
		},
		TTL: time.Hour,
	}
	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "exp-runner"}
	q := &queue.Queue{Store: s, Runner: "exp-runner", TTL: time.Hour}

	run := func(n int, task string) (queue.WorkItem, error) {
		rctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		defer cancel()
		ing, _, err := router.Open(rctx, ingress.Route{
			Name: "exp", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "exp", Requester: "experimenter",
		}, ingress.Delivery{Key: fmt.Sprintf("%s-%s-t%d-r%d", ch.name, arm, trial, n)})
		if err != nil {
			return queue.WorkItem{}, err
		}
		item, err := q.Submit(rctx, loop.ItemFor(ing, fmt.Sprintf("wi-%s-%s-t%d-r%d", ch.name, arm, trial, n), task))
		if err != nil {
			return queue.WorkItem{}, err
		}
		item, err = lp.Run(rctx, item)
		if err != nil {
			return item, err
		}
		o.toolCalls[n-1], o.wallMs[n-1] = metrics(rctx, s, item.ID)
		return item, nil
	}

	// Run 1.
	item1, err := run(1, ch.run1)
	if err != nil || item1.State != queue.StateDone {
		o.class, o.detail = "BROKEN", fmt.Sprintf("run1: state=%s err=%v note=%s", item1.State, err, item1.Note)
		return o
	}
	tree1, _ := treeAt(remote, item1.Head)
	for _, fact := range ch.facts {
		// Leakage check: substantive facts only (a bare "5" would be noise).
		if len(fact) > 3 && strings.Contains(tree1, fact) {
			o.class, o.detail = "LEAKED", "run1 wrote "+fact+" into the tree; chain excluded from clean comparison"
			return o
		}
	}

	// Run 2.
	item2, err := run(2, ch.run2)
	if err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	if item2.State == queue.StateParked && strings.Contains(item2.Note, "escalated") {
		o.class, o.detail = "ESCALATED", firstLine(item2.Note)
		return o
	}
	if item2.State != queue.StateDone {
		o.class, o.detail = "BROKEN", fmt.Sprintf("run2: state=%s note=%s", item2.State, firstLine(item2.Note))
		return o
	}
	tree2, err := treeAt(remote, item2.Head)
	if err != nil {
		o.class, o.detail = "BROKEN", err.Error()
		return o
	}
	for _, fact := range ch.facts {
		if !strings.Contains(tree2, fact) {
			o.class, o.detail = "WRONG", "landed without the fact "+fact
			return o
		}
	}
	if ch.violation != "" && strings.Contains(tree2, ch.violation) {
		o.class, o.detail = "WRONG", "convention violated: "+ch.violation
		return o
	}
	o.class, o.detail = "CORRECT", "facts used, conventions held"
	return o
}

func metrics(ctx context.Context, s *sqlite.Store, itemID string) (int, int64) {
	tools, wall := 0, int64(0)
	for r, err := range s.Log().Since(ctx, tn, "workitem/"+itemID, 1) {
		if err != nil {
			break
		}
		if r.Kind == "agent-result" {
			var m struct {
				ToolCalls int   `json:"tool_calls"`
				WallMs    int64 `json:"wall_ms"`
			}
			if jsonUnmarshal(r.Body, &m) == nil {
				tools, wall = m.ToolCalls, m.WallMs
			}
		}
	}
	return tools, wall
}

func render(results []outcome, trials int) string {
	var w strings.Builder
	fmt.Fprintf(&w, "# Seat continuity experiment — %s\n\n", time.Now().UTC().Format("2006-01-02"))
	w.WriteString("Real claude-agent-acp sessions through the full factory loop. Design pre-registered\nin hack/continuity/main.go: run 1 carries facts only in its handoff; run 2 needs them;\ntreatment wakes with the seat's notes, control is the identical agent without a seat.\n\n")
	w.WriteString("| arm | chain | trial | outcome | detail | r1 tools/ms | r2 tools/ms |\n|---|---|---|---|---|---|---|\n")
	tally := map[string]map[string]int{}
	for _, r := range results {
		if tally[r.arm] == nil {
			tally[r.arm] = map[string]int{}
		}
		tally[r.arm][r.class]++
		fmt.Fprintf(&w, "| %s | %s | %d | **%s** | %s | %d/%d | %d/%d |\n",
			r.arm, r.chain, r.trial, r.class, r.detail,
			r.toolCalls[0], r.wallMs[0], r.toolCalls[1], r.wallMs[1])
	}
	w.WriteString("\n## Tally\n\n| arm | CORRECT | ESCALATED | WRONG | LEAKED | BROKEN |\n|---|---|---|---|---|---|\n")
	for _, arm := range []string{"treatment", "control"} {
		t := tally[arm]
		fmt.Fprintf(&w, "| %s | %d | %d | %d | %d | %d |\n", arm,
			t["CORRECT"], t["ESCALATED"], t["WRONG"], t["LEAKED"], t["BROKEN"])
	}
	fmt.Fprintf(&w, "\nn = %d trials per arm per chain; small-sample results — direction, not precision.\n", trials)
	w.WriteString("An ESCALATED control is the contract WORKING (honest uncertainty); a WRONG control\nis the failure continuity exists to prevent; a CORRECT control means the facts leaked\nor were guessable and the chain design owes tightening.\n")
	return w.String()
}

func seedRemote(remote string) error {
	if err := gitRun("", "init", "--bare", remote); err != nil {
		return err
	}
	if err := gitRun(remote, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return err
	}
	seedDir, err := os.MkdirTemp("", "seed-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(seedDir)
	if err := gitRun("", "clone", remote, filepath.Join(seedDir, "c")); err != nil {
		return err
	}
	c := filepath.Join(seedDir, "c")
	if err := os.WriteFile(filepath.Join(c, "README.md"), []byte("experiment repo\n"), 0o644); err != nil {
		return err
	}
	if err := gitRun(c, "add", "."); err != nil {
		return err
	}
	if err := gitRun(c, "commit", "-m", "seed"); err != nil {
		return err
	}
	return gitRun(c, "push", "origin", "HEAD:refs/heads/main")
}

// treeAt concatenates every blob at a commit, for substring judging.
func treeAt(remote, head string) (string, error) {
	out, err := gitOut(remote, "ls-tree", "-r", "--name-only", head)
	if err != nil {
		return "", err
	}
	var all strings.Builder
	for _, f := range strings.Fields(out) {
		blob, err := gitOut(remote, "show", head+":"+f)
		if err == nil {
			all.WriteString(blob)
			all.WriteString("\n")
		}
	}
	return all.String(), nil
}

func gitRun(dir string, args ...string) error {
	_, err := gitOut(dir, args...)
	return err
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=exp", "GIT_AUTHOR_EMAIL=exp@invalid",
		"GIT_COMMITTER_NAME=exp", "GIT_COMMITTER_EMAIL=exp@invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return string(out), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
