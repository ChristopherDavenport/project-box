package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/loop"
)

// TestClaimTTLBoundedBuildTimeoutHasReviewMargin is issue #267's own
// property: a claim lease must outlast the build it fences by enough that
// review and land fit inside it too, so the two clocks can never be set
// equal again. 2x is the floor asserted — 3x is what claimTTL actually
// returns.
func TestClaimTTLBoundedBuildTimeoutHasReviewMargin(t *testing.T) {
	cfg := boxcfg.Config{BuildTimeout: "10m"}
	got := claimTTL(cfg)
	build := cfg.BuildTimeoutDuration()
	if got <= build {
		t.Fatalf("claimTTL(%s) = %s, want strictly greater than the build timeout", build, got)
	}
	if got < 2*build {
		t.Fatalf("claimTTL(%s) = %s, want at least 2x the build timeout", build, got)
	}
}

// TestClaimTTLEmptyBuildTimeoutDerivesFromLoopDefault: an unset build_timeout
// leaves the loop bounded by loop.DefaultBuildTimeout, so the lease must
// follow it there rather than reverting to some TTL of its own.
func TestClaimTTLEmptyBuildTimeoutDerivesFromLoopDefault(t *testing.T) {
	got := claimTTL(boxcfg.Config{})
	if got <= loop.DefaultBuildTimeout {
		t.Fatalf("claimTTL(empty) = %s, want strictly greater than loop.DefaultBuildTimeout (%s)", got, loop.DefaultBuildTimeout)
	}
}

// TestClaimTTLNegativeBuildTimeoutIsFinite: boxcfg documents a negative
// build_timeout as "no bound" on the build, but a lease has no way to
// express "forever" — it must still come back finite and positive.
func TestClaimTTLNegativeBuildTimeoutIsFinite(t *testing.T) {
	cfg := boxcfg.Config{BuildTimeout: "-1s"}
	if d := cfg.BuildTimeoutDuration(); d >= 0 {
		t.Fatalf("test setup: BuildTimeoutDuration() = %s, want negative", d)
	}
	got := claimTTL(cfg)
	if got <= 0 {
		t.Fatalf("claimTTL(negative build_timeout) = %s, want a finite positive TTL", got)
	}
}

// TestClaimTTLSharedByQueueAndAssembledLoops is the two-sites-one-derivation
// property: the daemon's own queue (what Claim grants) and every assembled
// Loop (what Retain renews) must read the same TTL off the same config, so a
// future edit cannot re-split the two clocks the way #267 found them split.
func TestClaimTTLSharedByQueueAndAssembledLoops(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	appRem := bare(t, "app")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
build_timeout: 5m
boxes:
  - name: shop
    repos:
      - name: app
        remote: %s
        build: '%s'
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), appRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	want := claimTTL(d.cfg)
	if want <= 5*time.Minute {
		t.Fatalf("claimTTL(d.cfg) = %s, want strictly more than 5m", want)
	}
	if d.queue.TTL != want {
		t.Fatalf("daemon queue TTL = %s, want claimTTL(cfg) = %s", d.queue.TTL, want)
	}

	loops, _, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := loops["shop/app"]
	if !ok {
		t.Fatal("shop/app: no loop assembled")
	}
	if lp.TTL != want {
		t.Fatalf("assembled loop TTL = %s, want claimTTL(cfg) = %s", lp.TTL, want)
	}
}
