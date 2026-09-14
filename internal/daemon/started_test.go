package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/loop"
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

// TestObserveSuspendOverTenMinuteJumpRecordsTheGap: one tick's observation
// over a wall clock that jumped ten minutes while the monotonic clock did
// not move at all — the pure-suspend shape, see [loop.SuspendWatch.Observe]
// — appends exactly one daemon-resumed record on the standing control/daemon
// chain, carrying the ten-minute gap in both the machine and human form the
// body promises.
func TestObserveSuspendOverTenMinuteJumpRecordsTheGap(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)

	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mono := time.Unix(0, 0)
	d.suspend = &loop.SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	d.observeSuspend(ctx) // baseline: first Observe always reports zero

	wall = wall.Add(10 * time.Minute)
	d.observeSuspend(ctx)

	recs := daemonStartedRecords(t, d, ctx)
	if len(recs) != 1 {
		t.Fatalf("control/daemon = %d records, want exactly 1: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Kind != "daemon-resumed" {
		t.Fatalf("kind = %q, want daemon-resumed", rec.Kind)
	}
	if rec.Actor != d.cfgRunner() {
		t.Fatalf("actor = %q, want the daemon's own runner identity %q", rec.Actor, d.cfgRunner())
	}
	var body daemonResumedBody
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.GapNS != int64(10*time.Minute) || body.Gap != (10*time.Minute).String() {
		t.Fatalf("body = %+v, want a ten-minute gap in both forms", body)
	}
}

// TestObserveSuspendWithNothingInFlightTouchesOnlyItsOwnRecord: the same
// ten-minute jump, with no run ever dispatched on this daemon, appends the
// one daemon-resumed record and nothing else — no parks, no reaps, no other
// chain touched. observeSuspend never calls Tick or the pump, so this is the
// noagent test rig's own "assert the chain quiet" shape applied to a single
// standing scope rather than the whole loop.
func TestObserveSuspendWithNothingInFlightTouchesOnlyItsOwnRecord(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)

	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mono := time.Unix(0, 0)
	d.suspend = &loop.SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	d.observeSuspend(ctx)
	wall = wall.Add(10 * time.Minute)
	d.observeSuspend(ctx)

	if recs := daemonStartedRecords(t, d, ctx); len(recs) != 1 {
		t.Fatalf("control/daemon = %d records, want exactly 1: %+v", len(recs), recs)
	}
	for _, scope := range []string{"control/claims", "control/capacity", "index/ingress", "index/pending"} {
		var n int
		for r, err := range d.store.Log().Since(ctx, d.tenant, scope, 1) {
			if err != nil {
				t.Fatal(err)
			}
			n++
			_ = r
		}
		if n != 0 {
			t.Fatalf("%s = %d records, want 0 — observing a suspend must not touch any other chain", scope, n)
		}
	}
}

// TestObserveSuspendWithNoGapAppendsNothing: a tick where the monotonic clock
// advanced by roughly as much as the wall clock did — ordinary elapsed time,
// not a suspend — must leave the control/daemon chain untouched. The record
// exists only when a gap was detected, so there is no zero-gap record to
// distinguish from an absent one.
func TestObserveSuspendWithNoGapAppendsNothing(t *testing.T) {
	ctx := context.Background()
	d, _ := plainDaemon(t)

	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mono := time.Unix(0, 0)
	d.suspend = &loop.SuspendWatch{
		Wall: func() time.Time { return wall },
		Mono: func() time.Time { return mono },
	}
	d.observeSuspend(ctx)

	wall = wall.Add(time.Second)
	mono = mono.Add(time.Second)
	d.observeSuspend(ctx)

	if recs := daemonStartedRecords(t, d, ctx); len(recs) != 0 {
		t.Fatalf("control/daemon = %d records, want 0 — no suspend was observed: %+v", len(recs), recs)
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
