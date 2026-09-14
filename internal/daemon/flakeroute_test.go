package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
)

// TestAssembleWiresFlakeRouteWhenConfigured: issue #275's daemon half —
// a repo's flake_route must both register as an addressable [ingress.Route]
// (Sections and Opens carried through, the same way the primary route's
// are) and wire [loop.Loop.Flaky] non-nil, so a proven CI flake actually
// summons work instead of only being recorded.
func TestAssembleWiresFlakeRouteWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	appRem := bare(t, "app")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    kinds:
      - name: hotfix
        decompose: {decomposer: identity}
        completion: {graph: true}
    repos:
      - name: app
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
        sections:
          - name: daemon
            purpose: the daemon
            paths: [internal/daemon]
        flake_route: {name: app-flakes, sections: [daemon], opens: hotfix}
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), appRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	loops, routes, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}

	lp, ok := loops["shop/app"]
	if !ok {
		t.Fatal("shop/app: no loop assembled")
	}
	if lp.Flaky == nil {
		t.Fatal("flake_route was configured but Loop.Flaky is nil")
	}

	r, ok := routes["app-flakes"]
	if !ok {
		t.Fatal("route app-flakes was not assembled")
	}
	if len(r.Sections) != 1 || r.Sections[0] != "daemon" {
		t.Fatalf("flake route sections = %v, want [daemon]", r.Sections)
	}
	if r.Opens != ingress.Kind("hotfix") {
		t.Fatalf("flake route opens = %q, want hotfix", r.Opens)
	}
	if r.Box != "shop" || r.Repo != "app" {
		t.Fatalf("flake route box/repo = %s/%s, want shop/app", r.Box, r.Repo)
	}
}

// TestAssembleLeavesFlakyNilWithoutFlakeRoute: no flake_route configured
// must leave Loop.Flaky nil — the loop.PRLander nil-safe precedent, so an
// operator who never opted in sees byte-for-byte today's behavior.
func TestAssembleLeavesFlakyNilWithoutFlakeRoute(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	appRem := bare(t, "app")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    repos:
      - name: app
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), appRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	loops, routes, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := loops["shop/app"]
	if !ok {
		t.Fatal("shop/app: no loop assembled")
	}
	if lp.Flaky != nil {
		t.Fatal("no flake_route was configured but Loop.Flaky is non-nil")
	}
	if _, ok := routes["shop-app-flakes"]; ok {
		t.Fatal("no flake_route was configured but a flake route was registered anyway")
	}
}
