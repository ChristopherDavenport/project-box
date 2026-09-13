package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

// fakeBuildCommit stands in for "the running binary was built from a commit
// that is not the target's head" — any well-formed sha that the target repo
// never produces works, since the chore only ever compares it for equality.
const fakeBuildCommit = "0000000000000000000000000000000000000000"

const validBoxdMain = "package main\n\nfunc main() {}\n"

// brokenBoxdMain fails to compile — the failing-build scenario's target.
const brokenBoxdMain = "package main\n\nfunc main() { this is not go\n"

// deployRepo seeds a bare repo with a trivial Go module containing exactly
// what the chore's own build step needs: a `cmd/boxd` package. It is
// deliberately not project-box itself — the chore only cares that
// `go build ./cmd/boxd` succeeds or fails in the checked-out tree, and a
// tiny synthetic module makes that fast and the failure modes explicit.
func deployRepo(t *testing.T, name, mainSrc string) (remote string, head forge.Commit) {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), name+".git")
	mustGit(t, "", "init", "--bare", bareDir)
	mustGit(t, bareDir, "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), name+"-seed")
	mustGit(t, "", "clone", bareDir, seed)
	if err := os.WriteFile(filepath.Join(seed, "go.mod"), []byte("module selfdeploytarget\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seed, "cmd", "boxd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "cmd", "boxd", "main.go"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "seed")
	mustGit(t, seed, "push", "origin", "HEAD:refs/heads/main")
	out := mustGit(t, seed, "rev-parse", "HEAD")
	return bareDir, forge.Commit(strings.TrimSpace(out))
}

// writeSelfDeployCfg is writeCfg's shape plus a self_deploy: block naming the
// "app" repo explicitly (two repos, so the box.yaml ambiguity refusal does
// not apply) and a "work" repo for ordinary, unrelated ingress.
func writeSelfDeployCfg(t *testing.T, path, db, root, workRemote, workBuild, appRemote, appCheck string) {
	t.Helper()
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
drain: 5s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    self_deploy: {from: main, repo: app}
    repos:
      - name: work
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
      - name: app
        remote: %s
        build: 'true'
        check: '%s'
`, db, root, workRemote, workBuild, appRemote, appCheck)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addRepoCommit clones remote fresh, writes relPath with content (creating
// parent directories as needed), commits, and pushes to main, returning the
// new head — a further landing on top of whatever deployRepo seeded.
func addRepoCommit(t *testing.T, remote, relPath, content string) forge.Commit {
	t.Helper()
	seed := filepath.Join(t.TempDir(), "seed")
	mustGit(t, "", "clone", remote, seed)
	full := filepath.Join(seed, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "add "+relPath)
	mustGit(t, seed, "push", "origin", "HEAD:refs/heads/main")
	out := mustGit(t, seed, "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out))
}

// writeSelfDeploySectionedCfg is writeSelfDeployCfg's shape, plus the "app"
// repo declaring two sections — "code" (cmd) and "docs" (docs) — and
// self_deploy naming which of them, via warrant, are candidates for deploy.
func writeSelfDeploySectionedCfg(t *testing.T, path, db, root, workRemote, appRemote, appCheck string, warrant []string) {
	t.Helper()
	warrantYAML := "[]"
	if len(warrant) > 0 {
		warrantYAML = "[" + strings.Join(warrant, ", ") + "]"
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
drain: 5s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    self_deploy: {from: main, repo: app, sections: %s}
    repos:
      - name: work
        remote: %s
        build: '%s'
        check: 'test -f out.txt'
      - name: app
        remote: %s
        build: 'true'
        check: '%s'
        sections:
          - name: code
            purpose: code
            paths: [cmd]
          - name: docs
            purpose: docs
            paths: [docs]
`, db, root, warrantYAML, workRemote, buildCmd, appRemote, appCheck)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never appeared within %s", path, timeout)
}

func claimsChain(t *testing.T, d *Daemon, ctx context.Context) []string {
	t.Helper()
	var kinds []string
	for r, err := range d.store.Log().Since(ctx, tenant.ID("acme"), "control/claims", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, r.Kind)
	}
	return kinds
}

func selfDeployChain(t *testing.T, d *Daemon, ctx context.Context) []string {
	t.Helper()
	var kinds []string
	for r, err := range d.store.Log().Since(ctx, tenant.ID("acme"), selfDeployScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, r.Kind)
	}
	return kinds
}

// TestSelfDeployUnchangedHeadDoesNothing: the common case, and it must run
// with no side effect at all — no stop, no resume, no refusal.
func TestSelfDeployUnchangedHeadDoesNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, appHead := deployRepo(t, "app", validBoxdMain)

	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "true")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: string(appHead), Known: true} }

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if got := claimsChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/claims = %v, want none", got)
	}
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none", got)
	}
}

// TestSelfDeployUnknownOrDirtyBuildDoesNothing: unknown never reads as zero
// — neither an unstamped binary nor one built from a modified tree may be
// compared against a branch head, in either direction.
func TestSelfDeployUnknownOrDirtyBuildDoesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build version.Build
	}{
		{"unknown", version.Build{}},
		{"dirty", version.Build{Commit: "deadbeef", Dirty: true, Known: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "box.yaml")
			db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
			workRem := bare(t, "work")
			appRem, _ := deployRepo(t, "app", validBoxdMain)
			writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "true")
			d, err := New(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.Close() })
			d.build = func() version.Build { return tc.build }

			if err := d.selfDeployChore(ctx); err != nil {
				t.Fatalf("selfDeployChore: %v", err)
			}
			if got := claimsChain(t, d, ctx); len(got) != 0 {
				t.Fatalf("control/claims = %v, want none", got)
			}
			if got := selfDeployChain(t, d, ctx); len(got) != 0 {
				t.Fatalf("control/self-deploy = %v, want none", got)
			}
		})
	}
}

// TestSelfDeployNoBlockDoesNothing: a box that never turned this on behaves
// exactly as it always has.
func TestSelfDeployNoBlockDoesNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	writeCfg(t, cfgPath, db, root, map[string]string{"work": workRem})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if got := claimsChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/claims = %v, want none", got)
	}
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none", got)
	}
}

// TestSelfDeployRefusesAFailingCheck: a check that fails is a refusal, on
// the chain, with claims resumed — and the exec seam is never reached.
func TestSelfDeployRefusesAFailingCheck(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, _ := deployRepo(t, "app", validBoxdMain)
	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "exit 1")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }
	execCalled := false
	d.Exec = func() error { execCalled = true; return nil }

	err = d.selfDeployChore(ctx)
	if errors.Is(err, ErrSelfDeployed) {
		t.Fatal("selfDeployChore returned ErrSelfDeployed on a failing check — no restart may follow a refusal")
	}
	if err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if execCalled {
		t.Fatal("Exec was called on a refused commit — the exec seam must never be reached")
	}
	if _, serr := os.Stat(exe); serr == nil {
		t.Fatal("the running executable's path was written to on a refused commit")
	}

	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deploy-refused" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-refused", kinds)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 || got[0] != "stop" || got[1] != "resume" {
		t.Fatalf("control/claims = %v, want [stop resume]", got)
	}
}

// TestSelfDeployRefusesAFailingBuild: the check can pass and the build can
// still fail — same refusal shape, and the running binary is untouched.
func TestSelfDeployRefusesAFailingBuild(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, _ := deployRepo(t, "app", brokenBoxdMain)
	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "true")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }
	execCalled := false
	d.Exec = func() error { execCalled = true; return nil }

	err = d.selfDeployChore(ctx)
	if errors.Is(err, ErrSelfDeployed) {
		t.Fatal("selfDeployChore returned ErrSelfDeployed on a failing build")
	}
	if err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if execCalled {
		t.Fatal("Exec was called on a refused commit — the exec seam must never be reached")
	}
	if _, serr := os.Stat(exe); serr == nil {
		t.Fatal("the running executable's path was written to on a refused build")
	}

	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deploy-refused" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-refused", kinds)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 || got[0] != "stop" || got[1] != "resume" {
		t.Fatalf("control/claims = %v, want [stop resume]", got)
	}
}

// TestSelfDeployDoesNotRetryARefusedCommit: a bad commit is checked exactly
// once, not every housekeeping pass forever.
func TestSelfDeployDoesNotRetryARefusedCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, _ := deployRepo(t, "app", validBoxdMain)
	counter := filepath.Join(dir, "counter")
	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "echo x >> "+counter+" && exit 1")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }

	for i := 0; i < 2; i++ {
		if err := d.selfDeployChore(ctx); err != nil {
			t.Fatalf("pass %d: selfDeployChore: %v", i, err)
		}
	}
	b, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(b), "x"); got != 1 {
		t.Fatalf("check ran %d times across two passes, want exactly 1 — a refused commit must not be retried", got)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 {
		t.Fatalf("control/claims = %v, want exactly one stop/resume pair", got)
	}
}

// TestSelfDeploySettlesBeforeCheckingAndSucceeds: a moved head stops claims,
// waits for the in-flight run on a wholly unrelated repo to settle, and only
// then checks and builds — and on success installs the binary, records it,
// resumes claims, and signals the sentinel for re-exec. The record order
// (stop, then resume) and the settle (nothing begins checking while a run is
// in flight) are both asserted here, on the one path that reaches resume by
// succeeding rather than by refusing.
func TestSelfDeploySettlesBeforeCheckingAndSucceeds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, _ := deployRepo(t, "app", validBoxdMain)
	started := filepath.Join(dir, "started")
	checked := filepath.Join(dir, "checked")
	slowBuild := fmt.Sprintf("touch %s && sleep 0.4 && echo done > out.txt && git add -A && git -c user.name=b -c user.email=b@i commit -m work", started)

	writeSelfDeployCfg(t, cfgPath, db, root, workRem, slowBuild, appRem, "touch "+checked)
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "shop-work", "work-1", "slow change", ""); err != nil {
		t.Fatal(err)
	}

	tickDone := make(chan struct{})
	var buildSettledAt time.Time
	go func() {
		d.Tick(ctx)
		buildSettledAt = time.Now()
		close(tickDone)
	}()
	waitForFile(t, started, 3*time.Second) // the build is now mid-sleep

	err = d.selfDeployChore(ctx)
	<-tickDone
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed (check and build both succeed here)", err)
	}

	info, serr := os.Stat(checked)
	if serr != nil {
		t.Fatalf("self-deploy check never ran: %v", serr)
	}
	if info.ModTime().Before(buildSettledAt) {
		t.Fatalf("self-deploy checked at %v, before the in-flight build settled at %v — began checking while a run was still going", info.ModTime(), buildSettledAt)
	}

	if _, serr := os.Stat(exe); serr != nil {
		t.Fatalf("the new binary was never installed: %v", serr)
	}

	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deployed" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deployed", kinds)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 || got[0] != "stop" || got[1] != "resume" {
		t.Fatalf("control/claims = %v, want [stop resume]", got)
	}
}

// TestSelfDeploySkipsWhenDiffIsEntirelyOutsideNamedSections: a landing whose
// diff sits wholly inside a declared section self_deploy.sections does not
// warrant is skipped — and claims are never stopped for it.
func TestSelfDeploySkipsWhenDiffIsEntirelyOutsideNamedSections(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, base := deployRepo(t, "app", validBoxdMain)
	addRepoCommit(t, appRem, "docs/readme.md", "docs change\n")

	writeSelfDeploySectionedCfg(t, cfgPath, db, root, workRem, appRem, "true", []string{"code"})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: string(base), Known: true} }

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if got := claimsChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/claims = %v, want none — a docs-only landing must never stop claims", got)
	}
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none", got)
	}
}

// TestSelfDeployDeploysWhenDiffTouchesANamedSection: a diff that touches a
// section named in self_deploy.sections deploys, same as an unsectioned
// landing always has.
func TestSelfDeployDeploysWhenDiffTouchesANamedSection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, base := deployRepo(t, "app", validBoxdMain)
	addRepoCommit(t, appRem, "cmd/boxd/extra.go", "package main\n\n// extra\n")

	writeSelfDeploySectionedCfg(t, cfgPath, db, root, workRem, appRem, "true", []string{"code"})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: string(base), Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deployed" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deployed", kinds)
	}
}

// TestSelfDeployDeploysWhenDiffTouchesPathInNoDeclaredSection: a path that
// matches no declared section counts as touching the daemon's tree and
// deploys, even though self_deploy.sections names something else entirely —
// unknown never reads as zero.
func TestSelfDeployDeploysWhenDiffTouchesPathInNoDeclaredSection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, base := deployRepo(t, "app", validBoxdMain)
	addRepoCommit(t, appRem, "VERSION", "1.0.0\n")

	writeSelfDeploySectionedCfg(t, cfgPath, db, root, workRem, appRem, "true", []string{"code"})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: string(base), Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deployed" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deployed", kinds)
	}
}

// TestSelfDeployEmptySectionsDeploysOnAnyMovement: self_deploy with no
// sections: at all keeps #64's original behavior — even a docs-only landing
// deploys.
func TestSelfDeployEmptySectionsDeploysOnAnyMovement(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, base := deployRepo(t, "app", validBoxdMain)
	addRepoCommit(t, appRem, "docs/readme.md", "docs change\n")

	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "true")
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: string(base), Known: true} }
	exe := filepath.Join(dir, "boxd-exe")
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed — no sections: means every moved head is a candidate", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != "self-deployed" {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deployed", kinds)
	}
}

// TestReexecClosesTheStoreAndCallsExec: the seam cmd/boxd drives after Run
// returns ErrSelfDeployed.
func TestReexecClosesTheStoreAndCallsExec(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	writeCfg(t, cfgPath, db, root, map[string]string{"work": workRem})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	d.Exec = func() error { called = true; return nil }

	if err := d.Reexec(); err != nil {
		t.Fatalf("Reexec: %v", err)
	}
	if !called {
		t.Fatal("Reexec did not call Exec")
	}
}

// TestReexecIsNilSafeWithNoExec: a Daemon nobody wired for self-deploy has
// no seam to call, and Reexec must not panic on it.
func TestReexecIsNilSafeWithNoExec(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	writeCfg(t, cfgPath, db, root, map[string]string{"work": workRem})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Reexec(); err != nil {
		t.Fatalf("Reexec with nil Exec: %v", err)
	}
}
