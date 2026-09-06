package boxcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// dogfoodConfig is the committed box that points this factory at its own
// repository (issue #7). It is loaded here rather than described, because a
// configuration nobody validates is a configuration that stops matching what
// runs.
const dogfoodConfig = "../../hack/dogfood/box.yaml"

// TestDogfoodBoxIsNarrow holds the dogfood to the posture issue #7 specified:
// one repo, one seat, MaxConcurrent 1, the gated lane.
//
// The posture is worth a test rather than a comment because two of its parts
// are DEFAULTS POINTING THE OTHER WAY. An unstated lane resolves to
// optimistic — land first, judge after — and an unstated branch resolves to
// main. So deleting two lines from box.yaml would silently turn a gated
// factory landing on refs/heads/factory into an optimistic one landing on
// the branch humans work in, and nothing would say so. Widening the dogfood
// is now a test failure, which is the only form in which "start narrow"
// survives contact with a config file.
func TestDogfoodBoxIsNarrow(t *testing.T) {
	c, err := Load(dogfoodConfig)
	if err != nil {
		t.Fatalf("the committed dogfood config does not load: %v", err)
	}
	if len(c.Boxes) != 1 {
		t.Fatalf("boxes = %d, want one box", len(c.Boxes))
	}
	bx := c.Boxes[0]
	if len(bx.Repos) != 1 {
		t.Fatalf("repos = %d, want one repo", len(bx.Repos))
	}
	r := bx.Repos[0]

	if len(bx.Agent) == 0 {
		t.Error("no agent: the dogfood exists to put a real agent in the loop, not a shell command")
	}
	if bx.Seat == "" {
		t.Error("no seat: without one there is no continuity to carry between runs")
	}
	// Two, not one, since sections: the point of the dogfood is now also
	// that two units in disjoint regions run side by side, and one seat
	// cannot show that. Still bounded small — the spend is real.
	if c.Budget.MaxConcurrent < 1 || c.Budget.MaxConcurrent > 2 {
		t.Errorf("max_concurrent = %d, want 1 or 2", c.Budget.MaxConcurrent)
	}
	if c.Budget.WindowReserve <= 0 {
		t.Error("no window_reserve: the account's window is the brake that actually bound in round 4")
	}
	if c.Budget.Credential != "plan" {
		t.Errorf("budget.credential = %q, want %q: this factory runs on the subscription on this machine, and window_reserve only means what it claims when the box says so", c.Budget.Credential, "plan")
	}
	if forge.Lane(r.Lane) != forge.LaneGated {
		t.Errorf("lane = %q, want %q — gates then land, until the optimistic lane is rehearsed on a scratch mirror",
			r.Lane, forge.LaneGated)
	}
	if reason := narrowForMain(r); reason != "" {
		t.Error(reason)
	}
	if !strings.HasSuffix(r.Remote, "project-box.git") {
		t.Errorf("remote = %q — the point of the dogfood is that it is pointed at THIS repository", r.Remote)
	}
}

// narrowForMain is the posture's branch half, since 2026-09-06 (PLAN §6): the
// factory is meant to reach `main`, but only through a pull request a
// judgement has reviewed, never by a bare fenced push. A repo not naming
// `main` is unaffected — it says nothing about how main is reached.
func narrowForMain(r Repo) string {
	if r.Branch != "main" {
		return ""
	}
	if r.Land != LandPullRequest || r.Review != ReviewJudgement {
		return "branch = main without land: pull_request and review: judgement — main is reached through a pull request a judgement has reviewed, never by a bare fenced push"
	}
	return ""
}

// TestMainWithoutTheLanderOrTheReviewIsNotNarrow proves narrowForMain actually
// fires: the committed dogfood still says `branch: factory` (the flip to
// `main` is a later, separate step per docs/PLAN.md §10), so
// TestDogfoodBoxIsNarrow's use of it above never exercises the main-branch
// case against real data. This constructs its own configs instead of editing
// hack/dogfood/box.yaml.
func TestMainWithoutTheLanderOrTheReviewIsNotNarrow(t *testing.T) {
	base := `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        branch: main
        lane: gated
`
	for _, tc := range []struct{ name, extra string }{
		{"missing both", ""},
		{"missing review", "        land: pull_request\n        land_token: gh\n"},
		{"missing lander", "        review: judgement\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "box.yaml")
			if err := os.WriteFile(p, []byte(base+tc.extra), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := Load(p)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if reason := narrowForMain(c.Boxes[0].Repos[0]); reason == "" {
				t.Fatal("branch: main missing the lander or the review should not read as narrow")
			}
		})
	}

	p := filepath.Join(t.TempDir(), "box.yaml")
	full := base + "        land: pull_request\n        land_token: gh\n        review: judgement\n"
	if err := os.WriteFile(p, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if reason := narrowForMain(c.Boxes[0].Repos[0]); reason != "" {
		t.Fatalf("both halves present should read as narrow: %s", reason)
	}
}

// TestDogfoodMeasuresWhatItSpends: round 1's finding, held. A run that will
// not say what it cost must stop the factory, so the dogfood carries a real
// token ceiling and never buys its way past silence with allow_unmetered.
// The number itself is free to move — the brake existing is not.
func TestDogfoodMeasuresWhatItSpends(t *testing.T) {
	c, err := Load(dogfoodConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Budget.MaxTokensPerWindow <= 0 {
		t.Error("no token ceiling: the brake has to be denominated in the currency the invoice is")
	}
	if c.WindowDuration() <= 0 {
		t.Error("no budget window: a ceiling with no window never turns over")
	}
	if c.Budget.AllowUnmetered {
		t.Error("allow_unmetered: an agent whose spend is invisible is the position round 1 cost a morning to learn")
	}
}

// TestDogfoodDeclaresItsModel: round 4's finding, held. A box that does not
// say which model it runs runs whatever the machine's settings say — which
// for four rounds was the most expensive model with a million-token window,
// chosen by nobody and, until the session record existed, recorded nowhere.
// The value is free to move; the field being set is not.
func TestDogfoodDeclaresItsModel(t *testing.T) {
	c, err := Load(dogfoodConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Boxes[0].Model == "" {
		t.Error("no model: the builders would run whatever ~/.claude says, and the record would not say what that was")
	}
}

// TestDogfoodExpressesWorkInSections: the dogfood is where fan-out and
// scoping are measured, so it declares where its own work is expressed and
// plans every issue by judgement. The section list is free to move; the
// mechanism being exercised is not.
func TestDogfoodExpressesWorkInSections(t *testing.T) {
	c, err := Load(dogfoodConfig)
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if len(bx.Repos[0].Sections) < 3 {
		t.Errorf("sections = %d: the dogfood exists to run units side by side, and one region cannot", len(bx.Repos[0].Sections))
	}
	planned := false
	for _, k := range bx.Kinds {
		if k.Name == "issue" && k.Decompose.Decomposer == "planner" {
			planned = true
		}
	}
	if !planned {
		t.Error("issues are not planned by judgement: every unit would be the whole repository on one tier")
	}
	if _, ok := bx.Roles["planner"]; !ok {
		t.Error("no planner tier: the dial to raise first when plans come back wrong is not on the record")
	}
}
