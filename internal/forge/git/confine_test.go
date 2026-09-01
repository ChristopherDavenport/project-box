package git

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// probeImage stands in for the image that would carry the agent. Anything
// with a shell proves the property, and pinning keeps the test from changing
// meaning when a tag moves.
const probeImage = "alpine:3.21"

// TestConfinedRunnerCannotReadOutsideItsContextSet is the property issue #6
// asks for, asserted by a test that tries.
//
// It runs the same probe twice against one real factory layout — a mirror, a
// leased workspace, a second run's workspace, the store — and the two legs
// are the whole point. Unconfined, every path is reachable: that is not a
// hypothetical, it is the seat-continuity experiment's finding 5b reproduced,
// where an agent explained itself by citing what it had read in "the factory
// mirror, and the sibling workspace". Confined, the granted two are reachable
// and the rest are not there at all.
//
// Docker-gated the way the collector and Spanner control tests are: skip
// locally without it, FAIL in CI. A confinement test that skips reads as a
// confinement that holds, which is the exact failure mode the plan forbids.
func TestConfinedRunnerCannotReadOutsideItsContextSet(t *testing.T) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("docker required in CI for the runner-confinement test: %v", err)
		}
		t.Skip("docker unavailable; the runner-confinement test needs it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	f := fix(t)
	m := f.mirror(t, "runner-a")
	leaser := f.store.Leaser("runner-a")

	// The run under test.
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-confined", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := m.Checkout(ctx, lease, f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// Its context set, materialized exactly where the builder puts it: a
	// sibling of the workspace, pinned, narrowed, read-only.
	ctxDir := ws.Dir + ".context"
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreForCleanup(t, ctxDir)
	if err := m.MaterializeContext(ctx, ctxDir, agentContext(f.base, "README.md")); err != nil {
		t.Fatalf("materialize context: %v", err)
	}

	// Another run's workspace, on the same root — the neighbour the
	// experiment caught an agent reading.
	other, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/other", Run: "run-neighbour", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire neighbour: %v", err)
	}
	neighbour, err := m.Checkout(ctx, other, f.base)
	if err != nil {
		t.Fatalf("checkout neighbour: %v", err)
	}
	if err := os.WriteFile(filepath.Join(neighbour.Dir, "SECRET"), []byte("another run's work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And the factory's own store, which knows every tenant's everything.
	store := filepath.Join(f.root, "box.db")
	if err := os.WriteFile(store, []byte("every lease, every record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mirrorDir := m.mirrorDir("app")

	reads := probeScript(map[string]string{
		"workspace":         "cat " + q(filepath.Join(ws.Dir, "README.md")),
		"context":           "cat " + q(filepath.Join(ctxDir, "app", "README.md")),
		"store":             "cat " + q(store),
		"mirror":            "ls " + q(mirrorDir),
		"sibling-workspace": "cat " + q(filepath.Join(neighbour.Dir, "SECRET")),
	})

	// Leg one: the runner as it stands. Everything is reachable, which is why
	// this issue exists.
	unconfined := probeResults(t, exec.CommandContext(ctx, "/bin/sh", "-c", reads))
	for _, path := range []string{"workspace", "context", "store", "mirror", "sibling-workspace"} {
		if unconfined[path] != "reachable" {
			t.Fatalf("unconfined %s = %q, want reachable — the control leg must show the exposure it is comparing against, or the confined leg proves nothing",
				path, unconfined[path])
		}
	}

	// Leg two: the same probe, through the profile. The writes ride along
	// here only — the control must not mutate what it is a control for.
	marker := filepath.Join(ws.Dir, "the-agent-wrote-this")
	ctxFile := filepath.Join(ctxDir, "app", "README.md")
	script := reads + "\n" + probeScript(map[string]string{
		"workspace-write": "echo confined > " + q(marker),
		// chmod first: the materializer already sets 0444, so a test that only
		// tried to write would pass on the file mode and say nothing about the
		// mount. A read-only bind mount refuses the chmod too.
		"context-write": "chmod u+w " + q(ctxFile) + " && echo scribble > " + q(ctxFile),
	})

	argv, err := confine.Profile{
		Image: probeImage,
		User:  fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
	}.Wrap([]string{"/bin/sh", "-c", script})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	argv, err = confine.Substitute(argv, ws.Dir, ctxDir)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	got := probeResults(t, exec.CommandContext(ctx, argv[0], argv[1:]...))

	for path, want := range map[string]string{
		"workspace":         "reachable", // the tree it was leased
		"context":           "reachable", // the material it was granted
		"workspace-write":   "reachable", // it can do the work
		"context-write":     "denied",    // read-only is a mount, not a file mode
		"store":             "denied",
		"mirror":            "denied",
		"sibling-workspace": "denied",
	} {
		if got[path] != want {
			t.Errorf("confined %s = %q, want %q", path, got[path], want)
		}
	}

	// Confinement that produces work nobody can land is not confinement, it is
	// an outage. The agent runs as the daemon, so what it wrote in the
	// workspace is the daemon's to commit — the failure this guards against is
	// docker's default user, which fills the tree with root-owned files and is
	// discovered at the end of an expensive run.
	if err := os.WriteFile(marker, []byte("the daemon owns this\n"), 0o644); err != nil {
		t.Fatalf("the confined agent's file is not the daemon's to touch: %v", err)
	}
	dirty, err := Dirty(ctx, ws.Dir)
	if err != nil || !dirty {
		t.Fatalf("workspace dirty = %v (err %v), want the confined run's change to be visible to git", dirty, err)
	}
	if err := CommitAll(ctx, ws.Dir, "what the confined agent left"); err != nil {
		t.Fatalf("the confined run's work is not landable: %v", err)
	}
}

// probeScript renders one shell line per named check, each reporting
// reachable or denied rather than exiting, so one failure does not hide the
// rest. Sorted, because the script shows up in failure output.
func probeScript(checks map[string]string) string {
	names := slices.Sorted(maps.Keys(checks))
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "if %s >/dev/null 2>&1; then echo %s=reachable; else echo %s=denied; fi\n",
			checks[name], name, name)
	}
	return b.String()
}

// probeResults reads the verdicts off stdout. Stdout only: docker's pull
// progress goes to stderr, and a probe's answer must not depend on what the
// runtime felt like narrating.
func probeResults(t *testing.T, cmd *exec.Cmd) map[string]string {
	t.Helper()
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("probe %v: %v\n%s%s", cmd.Args, err, out, stderr)
	}
	res := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			res[name] = verdict
		}
	}
	if len(res) == 0 {
		t.Fatalf("probe said nothing:\n%s", out)
	}
	return res
}

func q(path string) string { return "'" + path + "'" }

// restoreForCleanup hands a materialized context tree back writable so
// TempDir's cleanup can remove it. Cleanups run LIFO, so this must be
// registered before anything writes into the directory.
func restoreForCleanup(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				os.Chmod(path, 0o755)
			}
			return nil
		})
	})
}
