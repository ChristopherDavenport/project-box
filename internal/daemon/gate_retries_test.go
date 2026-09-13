package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGateRetriesRepoWinsOverBox is issue #83's wiring: assemble threads the
// resolved repo-else-box gate-retry bound onto the loop, repo winning, box
// the fallback, and neither leaving the loop's own default untouched.
func TestGateRetriesRepoWinsOverBox(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	appRem, infraRem, bareRem := bare(t, "app"), bare(t, "infra"), bare(t, "bare")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    gate_retries: 5
    repos:
      - name: app
        remote: %s
        build: '%s'
        gate_retries: 2
      - name: infra
        remote: %s
        build: '%s'
      - name: bare
        remote: %s
        build: '%s'
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"),
		appRem, buildCmd, infraRem, buildCmd, bareRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	loops, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		key  string
		want int
	}{
		{"shop/app", 2},   // the repo's own override wins over the box's
		{"shop/infra", 5}, // no repo override: the box's default applies
	}
	for _, c := range cases {
		lp, ok := loops[c.key]
		if !ok {
			t.Fatalf("%s: no loop assembled", c.key)
		}
		if lp.GateRetries != c.want {
			t.Errorf("%s: gate retries = %d, want %d", c.key, lp.GateRetries, c.want)
		}
	}

	// A third box, naming neither, leaves the loop's own field untouched —
	// zero — which is exactly what the loop resolves to its own default.
	cfg2 := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: crew
    repos:
      - name: bare
        remote: %s
        build: '%s'
`, filepath.Join(dir, "box2.db"), filepath.Join(dir, "forge2"), bareRem, buildCmd)
	cfgPath2 := filepath.Join(dir, "box2.yaml")
	if err := os.WriteFile(cfgPath2, []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, err := New(cfgPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	loops2, _, _, _, _, err := d2.assemble(context.Background(), d2.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if lp := loops2["crew/bare"]; lp.GateRetries != 0 {
		t.Errorf("crew/bare: gate retries = %d, want 0 (absent, the loop's own default applies)", lp.GateRetries)
	}
}
