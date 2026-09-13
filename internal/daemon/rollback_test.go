package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

func plainDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
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
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return d, root
}

// TestResolveSelfDeployMarkerClearsOnMatchingCommit: the ordinary case — the
// binary that just booted is the head the marker names, so the deploy
// worked and the standing self-deployed record already covers it. Nothing
// else is recorded; the marker just goes away.
func TestResolveSelfDeployMarkerClearsOnMatchingCommit(t *testing.T) {
	ctx := context.Background()
	d, root := plainDaemon(t)
	if err := writeSelfDeployMarker(root, selfDeployMarker{Head: "abc123", Reason: "self-deploy: box shop x -> abc123"}); err != nil {
		t.Fatal(err)
	}
	d.build = func() version.Build { return version.Build{Commit: "abc123", Known: true} }

	d.resolveSelfDeployMarker(ctx)

	if _, ok, err := readSelfDeployMarker(root); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("marker still present after the deployed head came up")
	}
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none — the deploy's own success record already covers this", got)
	}
}

// TestResolveSelfDeployMarkerRecordsRollbackOnMismatch: this binary is not
// the head the marker names, so it is the restored .prev, and the head that
// never came up gets recorded — on the standing chain, subject the bad
// head, and indexed into the room's exceptions — before the marker is
// removed.
func TestResolveSelfDeployMarkerRecordsRollbackOnMismatch(t *testing.T) {
	ctx := context.Background()
	d, root := plainDaemon(t)
	if err := writeSelfDeployMarker(root, selfDeployMarker{Head: "badhead", Reason: "self-deploy: box shop good -> badhead"}); err != nil {
		t.Fatal(err)
	}
	d.build = func() version.Build { return version.Build{Commit: "good", Known: true} }

	d.resolveSelfDeployMarker(ctx)

	if _, ok, err := readSelfDeployMarker(root); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("marker still present after being resolved")
	}

	var rolledBack *evidence.Record
	for rec, err := range d.store.Log().Since(ctx, d.tenant, selfDeployScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		r := rec
		rolledBack = &r
	}
	if rolledBack == nil || rolledBack.Kind != kindSelfDeployRolledBack {
		t.Fatalf("control/self-deploy latest = %+v, want a %s record", rolledBack, kindSelfDeployRolledBack)
	}
	if rolledBack.Subject != "badhead" {
		t.Fatalf("rollback subject = %q, want the bad head %q", rolledBack.Subject, "badhead")
	}
	if string(rolledBack.Body) != "self-deploy: box shop good -> badhead" {
		t.Fatalf("rollback body = %q, want the marker's reason", rolledBack.Body)
	}

	found := false
	for rec, err := range d.store.Log().Since(ctx, d.tenant, "index/exceptions", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject == selfDeployExceptionPrefix+"badhead" {
			found = true
		}
	}
	if !found {
		t.Fatal("no index/exceptions record for the rolled-back head — it would never appear in the room")
	}
}

// TestResolveSelfDeployMarkerLeavesItWhenBuildIsUnknown: an unstamped binary
// cannot be compared against the marker's head in either direction —
// unknown never reads as zero, so it neither clears the marker as a match
// nor records a rollback as a mismatch.
func TestResolveSelfDeployMarkerLeavesItWhenBuildIsUnknown(t *testing.T) {
	ctx := context.Background()
	d, root := plainDaemon(t)
	if err := writeSelfDeployMarker(root, selfDeployMarker{Head: "abc123", Reason: "why"}); err != nil {
		t.Fatal(err)
	}
	d.build = func() version.Build { return version.Build{} }

	d.resolveSelfDeployMarker(ctx)

	if _, ok, err := readSelfDeployMarker(root); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("marker removed despite an unknown build — this process cannot tell what happened")
	}
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none — an unknown build must not guess a rollback either", got)
	}
}

// TestResolveSelfDeployMarkerIsANoOpWithNoMarker: the overwhelmingly common
// start — no prior self-deploy, nothing to resolve, no error.
func TestResolveSelfDeployMarkerIsANoOpWithNoMarker(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)
	d.resolveSelfDeployMarker(ctx)
	if got := selfDeployChain(t, d, ctx); len(got) != 0 {
		t.Fatalf("control/self-deploy = %v, want none", got)
	}
}

// TestSelfDeployDoesNotRetryARolledBackHeadAcrossARestart: a fresh Daemon —
// its in-memory refused map necessarily empty, the shape a binary
// [RecoverFromFailedStart] just restored boots in — must still refuse a
// head whose latest control/self-deploy record is a rollback, by reading
// the chain rather than trusting memory alone.
func TestSelfDeployDoesNotRetryARolledBackHeadAcrossARestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	appRem, appHead := deployRepo(t, "app", validBoxdMain)
	counter := filepath.Join(dir, "counter")
	writeSelfDeployCfg(t, cfgPath, db, root, workRem, buildCmd, appRem, "echo x >> "+counter+" && true")

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.build = func() version.Build { return version.Build{Commit: fakeBuildCommit, Known: true} }

	if _, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: selfDeployScope, Kind: kindSelfDeployRolledBack,
		Subject: string(appHead), Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}

	if err := d.selfDeployChore(ctx); err != nil {
		t.Fatalf("selfDeployChore: %v", err)
	}
	if _, serr := os.Stat(counter); serr == nil {
		t.Fatal("check ran against a head already rolled back on the chain — durable refusal must consult it, not only memory")
	}
}

// TestSelfDeployRefusedIgnoresInterleavedPassRecords: issue #152's own named
// trap. A self-deploy-pass record's Subject must never be the bare head it
// inspected — [Daemon.selfDeployRefused] and internal/control's
// selfDeployException fold both take the LATEST record for a head's own
// subject as their answer, and a pass record landing there would flip both,
// silently, on every single pass. Proven by writing a refusal for a head and
// then several pass records that DO name that head (in their body, not their
// Subject) and checking the refusal still answers true.
func TestSelfDeployRefusedIgnoresInterleavedPassRecords(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)
	head := forge.Commit("badhead")

	if _, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: selfDeployScope, Kind: kindSelfDeployRefused,
		Subject: string(head), Actor: "boxd", Body: []byte("check failed"),
	}); err != nil {
		t.Fatal(err)
	}
	build := version.Build{Commit: fakeBuildCommit, Known: true}
	for i := 0; i < 3; i++ {
		d.recordSelfDeployPass(ctx, "shop", build, string(head), decisionRefused, "already refused on the chain", nil)
	}

	refused, err := d.selfDeployRefused(ctx, head)
	if err != nil {
		t.Fatal(err)
	}
	if !refused {
		t.Fatal("selfDeployRefused = false after interleaved pass records — a pass record must never become the LATEST answer for a head's own subject")
	}
}

// TestTargetForRollbackReadsRootFromConfig: cmd/boxd's rollback path has no
// live *Daemon to ask when daemon.New itself has failed, so it must be able
// to learn the state root straight from box.yaml.
func TestTargetForRollbackReadsRootFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	workRem := bare(t, "work")
	writeCfg(t, cfgPath, db, root, map[string]string{"work": workRem})

	target, err := TargetForRollback(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if target.Root != root {
		t.Fatalf("target.Root = %q, want %q", target.Root, root)
	}
	if target.Exe == "" {
		t.Fatal("target.Exe is empty")
	}
}

// TestTargetForRollbackErrorsOnUnreadableConfig: a config daemon.New itself
// could not load leaves nothing this can resolve either.
func TestTargetForRollbackErrorsOnUnreadableConfig(t *testing.T) {
	if _, err := TargetForRollback(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("want an error resolving a rollback target from a config that does not exist")
	}
}

// TestRecoverFromFailedStartRestoresAndAnnotatesTheMarker: the exe.prev
// restore path — the running (bad) binary is put back from its preserved
// sibling, and the marker's reason becomes the actual startup failure
// rather than the stale build/check context it was written with.
func TestRecoverFromFailedStartRestoresAndAnnotatesTheMarker(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "boxd-exe")
	if err := os.WriteFile(exe, []byte("bad binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".prev", []byte("good binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := dir
	if err := writeSelfDeployMarker(root, selfDeployMarker{Head: "badhead", Reason: "self-deploy: box shop good -> badhead"}); err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverFromFailedStart(RollbackTarget{Root: root, Exe: exe}, errors.New("listen tcp: address already in use"))
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("want recovered=true with a marker present")
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "good binary" {
		t.Fatalf("exe = %q after recovery, want the restored .prev content", got)
	}

	marker, ok, err := readSelfDeployMarker(root)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("marker removed by recovery — it must stay for the resolver on the restored binary's next start")
	}
	if marker.Head != "badhead" {
		t.Fatalf("marker head = %q, want unchanged %q", marker.Head, "badhead")
	}
	if marker.Reason != "listen tcp: address already in use" {
		t.Fatalf("marker reason = %q, want the startup failure", marker.Reason)
	}
}

// TestRecoverFromFailedStartWithNoMarkerDoesNothing: an ordinary startup
// failure unrelated to any self-deploy must not touch the executable at
// all.
func TestRecoverFromFailedStartWithNoMarkerDoesNothing(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "boxd-exe")
	if err := os.WriteFile(exe, []byte("only binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverFromFailedStart(RollbackTarget{Root: dir, Exe: exe}, errors.New("boom"))
	if err != nil {
		t.Fatal(err)
	}
	if recovered {
		t.Fatal("want recovered=false with no marker present")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "only binary" {
		t.Fatal("exe was touched despite there being nothing to roll back")
	}
}
