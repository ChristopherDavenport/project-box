package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

// daemonStartedRecords reads back every record on the standing
// control/daemon chain, for tests to assert against.
func daemonStartedRecords(t *testing.T, d *Daemon, ctx context.Context) []evidence.Record {
	t.Helper()
	var recs []evidence.Record
	for r, err := range d.store.Log().Since(ctx, d.tenant, daemonStartedScope, 1) {
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	return recs
}

// TestRecordStartedAppendsBootIDAndBuild: the ordinary case — a readable
// boot-id file and a known build both land in the one daemon-started record,
// on the standing control/daemon chain, authored by the daemon's own runner
// identity.
func TestRecordStartedAppendsBootIDAndBuild(t *testing.T) {
	ctx := context.Background()
	d, dir := plainDaemon(t)
	d.build = func() version.Build { return version.Build{Commit: "abc123", Dirty: true, Known: true} }
	bootPath := filepath.Join(dir, "boot_id")
	if err := os.WriteFile(bootPath, []byte("deadbeef-boot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.bootIDPath = bootPath

	if err := d.recordStarted(ctx); err != nil {
		t.Fatal(err)
	}

	recs := daemonStartedRecords(t, d, ctx)
	if len(recs) != 1 {
		t.Fatalf("control/daemon = %d records, want exactly 1: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Actor != d.cfgRunner() {
		t.Fatalf("actor = %q, want the daemon's own runner identity %q", rec.Actor, d.cfgRunner())
	}
	var body daemonStartedBody
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatal(err)
	}
	if !body.BootIDKnown || body.BootID != "deadbeef-boot" {
		t.Fatalf("body = %+v, want boot id known as deadbeef-boot", body)
	}
	if !body.BuildKnown || body.BuildCommit != "abc123" || !body.BuildDirty {
		t.Fatalf("body = %+v, want build known as abc123/dirty", body)
	}
}

// TestRecordStartedUnreadableBootIDIsUnknownNotEmpty: an absent boot-id file
// must record unknown explicitly — a caller reading the body back can tell
// "the kernel could not be asked" apart from "this boot has no id", which an
// empty string would not.
func TestRecordStartedUnreadableBootIDIsUnknownNotEmpty(t *testing.T) {
	ctx := context.Background()
	d, dir := plainDaemon(t)
	d.build = func() version.Build { return version.Build{Commit: "abc123", Known: true} }
	d.bootIDPath = filepath.Join(dir, "no-such-boot-id-file")

	if err := d.recordStarted(ctx); err != nil {
		t.Fatal(err)
	}

	recs := daemonStartedRecords(t, d, ctx)
	if len(recs) != 1 {
		t.Fatalf("control/daemon = %d records, want exactly 1", len(recs))
	}
	var body daemonStartedBody
	if err := json.Unmarshal(recs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.BootIDKnown || body.BootID != "" {
		t.Fatalf("body = %+v, want boot id unknown, not an empty string presented as fact", body)
	}
	if recs[0].Subject != "unknown" {
		t.Fatalf("subject = %q, want the explicit unknown marker", recs[0].Subject)
	}
}

// TestRecordStartedUnknownBuildIsNotZero: an unstamped test binary's build
// must record unknown rather than an empty commit that would read as a real,
// if blank, one.
func TestRecordStartedUnknownBuildIsNotZero(t *testing.T) {
	ctx := context.Background()
	d, dir := plainDaemon(t)
	d.build = func() version.Build { return version.Build{} }
	bootPath := filepath.Join(dir, "boot_id")
	if err := os.WriteFile(bootPath, []byte("deadbeef-boot"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.bootIDPath = bootPath

	if err := d.recordStarted(ctx); err != nil {
		t.Fatal(err)
	}

	recs := daemonStartedRecords(t, d, ctx)
	if len(recs) != 1 {
		t.Fatalf("control/daemon = %d records, want exactly 1", len(recs))
	}
	var body daemonStartedBody
	if err := json.Unmarshal(recs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.BuildKnown || body.BuildCommit != "" || body.BuildDirty {
		t.Fatalf("body = %+v, want build unknown, not a zero commit/dirty presented as fact", body)
	}
}
