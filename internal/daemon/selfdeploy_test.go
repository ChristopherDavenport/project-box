package daemon

import (
	"context"
	"encoding/json"
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

// selfDeployPasses decodes every self-deploy-pass record on the chain, in
// order, for assertions on what a pass decided and why — and, via r.Subject,
// that it never lands on the bare head (issue #152's own named trap).
func selfDeployPasses(t *testing.T, d *Daemon, ctx context.Context) []selfDeployPassBody {
	t.Helper()
	var out []selfDeployPassBody
	for r, err := range d.store.Log().Since(ctx, tenant.ID("acme"), selfDeployScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != kindSelfDeployPass {
			continue
		}
		if !strings.HasPrefix(r.Subject, "pass/") {
			t.Fatalf("self-deploy-pass record Subject = %q, want the pass/<box> form — never the bare head", r.Subject)
		}
		var body selfDeployPassBody
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatalf("decoding pass record body: %v", err)
		}
		out = append(out, body)
	}
	return out
}

// TestSelfDeployUnchangedHeadDoesNothing: the common case has no operational
// side effect — no stop, no resume, no refusal — but it still leaves a
// "current" pass record (issue #152): a pass that decided nothing is still
// a pass that ran.
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
	if got := selfDeployChain(t, d, ctx); len(got) != 1 || got[0] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-pass", got)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionCurrent {
		t.Fatalf("passes = %+v, want exactly one with decision %q", passes, decisionCurrent)
	}
	if passes[0].Head != string(appHead) {
		t.Fatalf("pass head = %q, want %q", passes[0].Head, appHead)
	}

	at, decision, reason, ok := d.lastPassForAPI()
	if !ok || decision != decisionCurrent || reason == "" {
		t.Fatalf("lastPassForAPI = at=%v decision=%q reason=%q ok=%v, want a current decision", at, decision, reason, ok)
	}
}

// TestSelfDeployUnknownOrDirtyBuildDoesNothing: unknown never reads as zero
// — neither an unstamped binary nor one built from a modified tree may be
// compared against a branch head, in either direction — but each is exactly
// the "why has the box not upgraded" answer a pass record exists for
// (issue #152), so the chore-level decline still leaves one.
func TestSelfDeployUnknownOrDirtyBuildDoesNothing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		build    version.Build
		decision string
	}{
		{"unknown", version.Build{}, decisionBuildUnknown},
		{"dirty", version.Build{Commit: "deadbeef", Dirty: true, Known: true}, decisionBuildDirty},
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
			if got := selfDeployChain(t, d, ctx); len(got) != 1 || got[0] != kindSelfDeployPass {
				t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-pass", got)
			}
			passes := selfDeployPasses(t, d, ctx)
			if len(passes) != 1 || passes[0].Decision != tc.decision {
				t.Fatalf("passes = %+v, want exactly one with decision %q", passes, tc.decision)
			}
			if passes[0].Head != "" {
				t.Fatalf("pass head = %q, want absent — the branch was never resolved", passes[0].Head)
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
	if len(kinds) != 2 || kinds[0] != "self-deploy-refused" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deploy-refused self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionRefused || passes[0].Reason == "" {
		t.Fatalf("passes = %+v, want exactly one refused with a reason", passes)
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
	if len(kinds) != 2 || kinds[0] != "self-deploy-refused" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deploy-refused self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionRefused || passes[0].Reason == "" {
		t.Fatalf("passes = %+v, want exactly one refused with a reason", passes)
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
	appRem, appHead := deployRepo(t, "app", validBoxdMain)
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
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.exePath = func() (string, error) { return exe, nil }

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "shop-work", "work-1", "slow change", "", ""); err != nil {
		t.Fatal(err)
	}

	tickDone := make(chan struct{})
	var buildSettledAt time.Time
	go func() {
		// Tick itself only dispatches and returns (issue #196); it is
		// [pump.Pump.Quiesce] — the same barrier selfDeployChore's own
		// settle step below uses — that marks when the slow build this tick
		// started has actually finished.
		d.Tick(ctx)
		d.pump.Quiesce(ctx)
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

	prev, perr := os.ReadFile(exe + ".prev")
	if perr != nil {
		t.Fatalf("the running binary was not preserved as .prev: %v", perr)
	}
	if string(prev) != "old binary" {
		t.Fatalf(".prev = %q, want the binary that was running before install", prev)
	}

	marker, ok, merr := readSelfDeployMarker(root)
	if merr != nil {
		t.Fatalf("reading self-deploy marker: %v", merr)
	}
	if !ok {
		t.Fatal("no self-deploy marker written for the installed head")
	}
	if marker.Head != string(appHead) {
		t.Fatalf("marker head = %q, want %q", marker.Head, appHead)
	}

	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 2 || kinds[0] != "self-deployed" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deployed self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionDeploying || passes[0].Head != string(appHead) {
		t.Fatalf("passes = %+v, want exactly one deploying at head %s", passes, appHead)
	}
	at, decision, _, ok := d.lastPassForAPI()
	if !ok || decision != decisionDeploying || at.IsZero() {
		t.Fatalf("lastPassForAPI = at=%v decision=%q ok=%v, want a deploying decision", at, decision, ok)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 || got[0] != "stop" || got[1] != "resume" {
		t.Fatalf("control/claims = %v, want [stop resume]", got)
	}
}

// TestSelfDeployAbandonsWhenQuiesceDoesNotDrain: a run still in flight when
// the drain window elapses is abandoned rather than built on top of — the
// head is not refused (it was never attempted), and claims are resumed so
// the run in flight can finish. Abandon-on-busy behavior itself is
// unchanged (issue #152); it now also leaves a pass record saying so.
func TestSelfDeployAbandonsWhenQuiesceDoesNotDrain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, appHead := deployRepo(t, "app", validBoxdMain)
	started := filepath.Join(dir, "started")
	slowBuild := fmt.Sprintf("touch %s && sleep 1 && echo done > out.txt && git add -A && git -c user.name=b -c user.email=b@i commit -m work", started)

	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
drain: 50ms
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
        check: 'true'
`, db, root, workRem, slowBuild, appRem)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "shop-work", "work-1", "slow change", "", ""); err != nil {
		t.Fatal(err)
	}

	// Tick only dispatches and returns (issue #196), so tickDone is closed
	// only once [pump.Pump.Quiesce] confirms the slow build actually
	// finished — otherwise "let the in-flight run finish before the test
	// tears down" below would wait on nothing and race d.Close().
	tickDone := make(chan struct{})
	go func() { d.Tick(ctx); d.pump.Quiesce(ctx); close(tickDone) }()
	waitForFile(t, started, 3*time.Second) // the build is now mid-sleep, well past the 50ms drain

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	<-tickDone // let the in-flight run finish before the test tears down

	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-pass", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionAbandoned || passes[0].Head != string(appHead) {
		t.Fatalf("passes = %+v, want exactly one abandoned at head %s", passes, appHead)
	}
	if got := claimsChain(t, d, ctx); len(got) != 2 || got[0] != "stop" || got[1] != "resume" {
		t.Fatalf("control/claims = %v, want [stop resume] — claims must still be resumed on abandon", got)
	}

	refused, err := d.selfDeployRefused(ctx, appHead)
	if err != nil {
		t.Fatal(err)
	}
	if refused {
		t.Fatal("an abandoned head must not be remembered as refused — it was never attempted")
	}
}

// TestSelfDeployRecordsAnErroredPassOnFetchFailure: a pass that cannot even
// reach the point of deciding still owes the chain an answer (issue #152) —
// mapping a fetch failure onto "current" would be unknown reading as zero.
func TestSelfDeployRecordsAnErroredPassOnFetchFailure(t *testing.T) {
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
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }

	// New's own assemble already mirrored app once; removing its remote now
	// makes the chore's next Fetch (a `git remote update` against a mirror
	// that already exists) fail, without touching the mirror directory
	// selfDeployChore itself reads from.
	if err := os.RemoveAll(appRem); err != nil {
		t.Fatal(err)
	}

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v — an unexpected failure is logged and left for the next pass, not propagated", err)
	}
	if got := claimsChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/claims = %v, want none — a fetch failure never reaches the stop", got)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 1 || kinds[0] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-pass", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionErrored || passes[0].Reason == "" {
		t.Fatalf("passes = %+v, want exactly one errored with a reason", passes)
	}
	if passes[0].Head != "" {
		t.Fatalf("pass head = %q, want absent — the branch was never resolved", passes[0].Head)
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
	if got := selfDeployChain(t, d, ctx); len(got) != 1 || got[0] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want exactly one self-deploy-pass", got)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionSkipped {
		t.Fatalf("passes = %+v, want exactly one skipped", passes)
	}
	if len(passes[0].Sections) != 1 || passes[0].Sections[0] != "docs" {
		t.Fatalf("pass sections = %v, want [docs]", passes[0].Sections)
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
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 2 || kinds[0] != "self-deployed" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deployed self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionDeploying || len(passes[0].Sections) != 1 || passes[0].Sections[0] != "code" {
		t.Fatalf("passes = %+v, want exactly one deploying touching [code]", passes)
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
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 2 || kinds[0] != "self-deployed" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deployed self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionDeploying || len(passes[0].Sections) != 0 {
		t.Fatalf("passes = %+v, want exactly one deploying with no sections — VERSION matches no declared section", passes)
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
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.exePath = func() (string, error) { return exe, nil }

	err = d.selfDeployChore(ctx)
	if !errors.Is(err, ErrSelfDeployed) {
		t.Fatalf("selfDeployChore = %v, want ErrSelfDeployed — no sections: means every moved head is a candidate", err)
	}
	kinds := selfDeployChain(t, d, ctx)
	if len(kinds) != 2 || kinds[0] != "self-deployed" || kinds[1] != kindSelfDeployPass {
		t.Fatalf("control/self-deploy = %v, want [self-deployed self-deploy-pass]", kinds)
	}
	passes := selfDeployPasses(t, d, ctx)
	if len(passes) != 1 || passes[0].Decision != decisionDeploying || len(passes[0].Sections) != 0 {
		t.Fatalf("passes = %+v, want exactly one deploying with no sections — self_deploy declared none", passes)
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

// TestStatusReportsTheFollowedBranchHead: self_deploy set means /v1/status
// carries the followed branch and the commit this daemon's own mirror
// resolves it to — the mirror's own record, populated by assemble's fetch at
// New, not a fresh look the status handler takes itself.
func TestStatusReportsTheFollowedBranchHead(t *testing.T) {
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

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.FollowBranch != "main" || st.FollowHead != string(appHead) {
		t.Fatalf("status = branch %q head %q, want main %s", st.FollowBranch, st.FollowHead, appHead)
	}
}

// TestStatusOmitsTheFollowedBranchWithoutSelfDeploy: a box that never turned
// self-deploy on has nothing to compare a build against, and /v1/status must
// say so by leaving the fields absent rather than naming a branch nobody
// configured.
func TestStatusOmitsTheFollowedBranchWithoutSelfDeploy(t *testing.T) {
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

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.FollowBranch != "" || st.FollowHead != "" {
		t.Fatalf("status = branch %q head %q, want both absent", st.FollowBranch, st.FollowHead)
	}
}

// TestStatusReportsTheRunningBuild: the daemon threads its own build seam —
// the same one the self-deploy chore compares against — into /v1/status,
// rather than the API reading version.ThisBuild on its own.
func TestStatusReportsTheRunningBuild(t *testing.T) {
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
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Dirty: true, Known: true} }

	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, err := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.BuildCommit != fakeBuildCommit || !st.BuildDirty {
		t.Fatalf("status = commit %q dirty %v, want %s true", st.BuildCommit, st.BuildDirty, fakeBuildCommit)
	}
}
