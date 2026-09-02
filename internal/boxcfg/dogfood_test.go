package boxcfg

import (
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
	if c.Budget.MaxConcurrent != 1 {
		t.Errorf("max_concurrent = %d, want 1", c.Budget.MaxConcurrent)
	}
	if forge.Lane(r.Lane) != forge.LaneGated {
		t.Errorf("lane = %q, want %q — gates then land, until the optimistic lane is rehearsed on a scratch mirror",
			r.Lane, forge.LaneGated)
	}
	if r.Branch == "main" {
		t.Error("branch = main: the factory lands on a branch of its own, never the one a human is working on")
	}
	if !strings.HasSuffix(r.Remote, "project-box.git") {
		t.Errorf("remote = %q — the point of the dogfood is that it is pointed at THIS repository", r.Remote)
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
