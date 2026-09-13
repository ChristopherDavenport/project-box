package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeLandCfg writes a box.yaml with one repo whose `land` (and, if
// non-empty, `land_token`) are exactly what the test passes — the two knobs
// [Daemon.assemble] reads to decide whether to wrap the loop's Lander with
// git.NewPRLander.
func writeLandCfg(t *testing.T, path, db, root, remote, land, landToken string) {
	t.Helper()
	var extra string
	if land != "" {
		extra += "\n        land: " + land
	}
	if landToken != "" {
		extra += "\n        land_token: " + landToken
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    repos:
      - name: app
        remote: %s
        build: '%s'
        check: 'test -f out.txt'%s
`, db, root, remote, buildCmd, extra)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAssembleWiresPRLanderForPullRequestLanding is the daemon half of issue
// #22's wiring: a repo configured for pull_request landing gets a Loop whose
// PRLander seam is filled, and a repo that says nothing about landing gets
// today's behavior — PRLander left nil, a direct fenced push.
func TestAssembleWiresPRLanderForPullRequestLanding(t *testing.T) {
	ctx := context.Background()

	t.Run("pull_request configured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "pull_request", "gh")

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		if lp.PRLander == nil {
			t.Fatal("land: pull_request configured, but Loop.PRLander is nil")
		}
	})

	t.Run("unconfigured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "box.yaml")
		db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
		writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "", "")

		d, err := New(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })

		loops, _, _, _, _, _, err := d.assemble(ctx, d.cfg)
		if err != nil {
			t.Fatal(err)
		}
		lp, ok := loops["shop/app"]
		if !ok {
			t.Fatal("no loop assembled for shop/app")
		}
		if lp.PRLander != nil {
			t.Fatal("land left unset, but Loop.PRLander is non-nil — today's plain-push behavior would change")
		}
	})
}

// TestPullRequestLandingRefusedWithoutToken confirms the boxcfg refusal
// reaches all the way through daemon.New: a config that asks for
// pull_request landing with no way to obtain a token never gets far enough
// to assemble a Loop at all.
func TestPullRequestLandingRefusedWithoutToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "pull_request", "")

	if _, err := New(cfgPath); err == nil {
		t.Fatal("pull_request landing with no land_token was accepted")
	}
}
