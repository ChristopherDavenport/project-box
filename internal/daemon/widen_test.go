package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/loop"
)

// TestAssembleThreadsWidenOntoEveryRepoLoop is issue #182 part 4's daemon
// wiring: box.yaml's widen: policy — resolved at boxcfg.Load, "tests" by
// default — must reach every repo's [loop.Loop] unchanged, the same
// box-wins-with-no-per-repo-override shape [loop.Loop.Attempts] already has.
func TestAssembleThreadsWidenOntoEveryRepoLoop(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	defaultRem, noneRem := bare(t, "default"), bare(t, "none")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: defaults
    repos:
      - name: app
        remote: %s
        build: '%s'
  - name: says-none
    widen: none
    repos:
      - name: app
        remote: %s
        build: '%s'
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"),
		defaultRem, buildCmd, noneRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	loops, _, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := loops["defaults/app"].Widen; got != loop.WidenTests {
		t.Errorf("defaults/app: widen = %q, want %q (the unset default)", got, loop.WidenTests)
	}
	if got := loops["says-none/app"].Widen; got != loop.WidenNone {
		t.Errorf("says-none/app: widen = %q, want %q", got, loop.WidenNone)
	}
}

// TestAssembleWiresReplannerOnlyForABoxWithAnAgent is issue #182 part 4's
// other daemon wiring: the box's acp.Planner — the same instance registered
// into d.planners — becomes the pump's [plan.Replanner], and a box with no
// agent leaves it nil, which [pump.Pump.Replan] treats exactly as it did
// before replanning existed (see noagent_test.go's TestTheFactoryRunsWithNoAgentInIt).
func TestAssembleWiresReplannerOnlyForABoxWithAnAgent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    agent: ['true']
    repos:
      - name: app
        remote: %s
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), bare(t, "app"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.pump.Replanner == nil {
		t.Fatal("box shop has an agent — the pump's Replanner must not be nil")
	}
	if _, _, _, _, _, replanner, err := d.assemble(context.Background(), d.cfg); err != nil {
		t.Fatal(err)
	} else if replanner == nil {
		t.Fatal("assemble did not return a replanner for a box with an agent")
	}

	dir2 := t.TempDir()
	cfgPath2 := filepath.Join(dir2, "box.yaml")
	cfg2 := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: crew
    repos:
      - name: app
        remote: %s
        build: '%s'
`, filepath.Join(dir2, "box.db"), filepath.Join(dir2, "forge"), bare(t, "app2"), buildCmd)
	if err := os.WriteFile(cfgPath2, []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, err := New(cfgPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if d2.pump.Replanner != nil {
		t.Fatal("box crew has no agent — the pump's Replanner must be nil")
	}
}
