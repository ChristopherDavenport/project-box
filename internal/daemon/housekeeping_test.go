package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHousekeepingDueIsWallClockNotTickCount: Run's housekeeping cadence
// used to be ticks%housekeepingEvery==0 — counted in ticks, so one
// long-blocked tick could cross several multiples of that count in a single
// evaluation and fire the standing chores that many times back to back the
// moment it finally woke. housekeepingDue is a single boolean check against
// elapsed wall-clock time instead: however late the check arrives, "has the
// interval elapsed" is answered once, not once per interval that has passed
// (issue #152).
func TestHousekeepingDueIsWallClockNotTickCount(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	interval := 100 * time.Millisecond

	if housekeepingDue(start, start.Add(50*time.Millisecond), interval) {
		t.Fatal("due before the interval elapsed")
	}
	if !housekeepingDue(start, start.Add(100*time.Millisecond), interval) {
		t.Fatal("not due exactly at the interval")
	}
	// A single tick arriving long after the interval — the scenario a
	// blocked tick or a slow build produces — must still answer true
	// exactly once per call, never a count of how many intervals fit.
	if !housekeepingDue(start, start.Add(10*interval), interval) {
		t.Fatal("not due long after the interval — a single late check must still fire")
	}
}

// TestDaemonHousekeepEveryReadsConfig: the accessor mirrors Every and drain
// — hot-reloadable from the same cfg the wall-clock cadence in Run reads
// every time, never captured once at start.
func TestDaemonHousekeepEveryReadsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	cfg := "tenant: acme\nrunner: boxd\ndb: " + db + "\nroot: " + root +
		"\nlisten: unix:/tmp/never-used.sock\nevery: 1s\nhousekeep_every: 42s\ndrain: 5s\nboxes:\n" +
		"  - name: shop\n    repos:\n      - name: work\n        remote: " + workRem + "\n        build: 'true'\n        check: 'true'\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if got := d.housekeepEvery(); got != 42*time.Second {
		t.Fatalf("housekeepEvery() = %v, want 42s", got)
	}
}
