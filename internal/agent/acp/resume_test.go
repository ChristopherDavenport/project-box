package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestResumeContinuesTheSameSession: a run started with Runtime.Resume set
// calls session/load instead of session/new, replays the prior conversation
// as notifications, and runs exactly the same wire assertions a cold session
// runs (set_mode, configure) before the prompt goes out.
func TestResumeContinuesTheSameSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "resume")
	rt.Resume = "sess-1"
	res, err := rt.Run(ctx, bundle(), "fix it and commit")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason: %q", res.StopReason)
	}
	if !strings.Contains(res.Text, "(replayed) the earlier turn") {
		t.Fatalf("the replayed notification did not reach Result.Text: %q", res.Text)
	}
	if !strings.Contains(res.Text, "resumed:sess-1") {
		t.Fatalf("session/load was not sent the resumed id: %q", res.Text)
	}
	if !strings.Contains(res.Text, "cwd:"+rt.Dir) {
		t.Fatalf("session/load was not sent the workspace cwd: %q", res.Text)
	}
	// set_mode and configure ran again, exactly as they would for a cold
	// session — the continuation's bundle is asserted at the wire again or
	// it does not run.
	if res.Session.Modes.Current != "acceptEdits" {
		t.Fatalf("mode not asserted on the resumed session: %+v", res.Session.Modes)
	}
}

// TestSessionIDIsPopulatedOnBothPaths: Result.Session.ID is the id the
// runtime actually used — session/new's answer on a cold run, and the id
// asked for on a resume — either way the fact a later attempt would need to
// continue this one.
func TestSessionIDIsPopulatedOnBothPaths(t *testing.T) {
	coldCtx, coldCancel := context.WithTimeout(context.Background(), time.Minute)
	defer coldCancel()
	cold, err := runtimeFor(t, "session").Run(coldCtx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("cold run: %v", err)
	}
	if cold.Session.ID != "sess-1" {
		t.Fatalf("cold session id = %q, want sess-1", cold.Session.ID)
	}
	rt := runtimeFor(t, "resume")
	rt.Resume = "sess-1"
	warmCtx, warmCancel := context.WithTimeout(context.Background(), time.Minute)
	defer warmCancel()
	warm, err := rt.Run(warmCtx, bundle(), "fix it and commit")
	if err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if warm.Session.ID != "sess-1" {
		t.Fatalf("resumed session id = %q, want sess-1", warm.Session.ID)
	}
}

// TestResumeWithoutCapabilityFailsBeforeLoad: an agent that never advertises
// loadSession must never be asked for it — the refusal happens before
// session/load is even sent, which is the difference between "spent nothing
// finding out" and "asked and was refused".
func TestResumeWithoutCapabilityFailsBeforeLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "session") // advertises nothing about loadSession
	rt.Resume = "sess-1"
	res, err := rt.Run(ctx, bundle(), "fix it and commit")
	if !errors.Is(err, ErrResume) {
		t.Fatalf("want ErrResume, got %v (res %+v)", err, res)
	}
	if res.Session.Opened {
		t.Fatalf("a refusal before session/load must not open a session: %+v", res.Session)
	}
}

// TestResumeLoadErrorWrapsErrResume: session/load itself refusing — the
// agent no longer holds that transcript, say — is a resume failure like any
// other, distinguishable by the same sentinel so a caller can fall back cold.
func TestResumeLoadErrorWrapsErrResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "resume-load-fails")
	rt.Resume = "sess-1"
	if _, err := rt.Run(ctx, bundle(), "fix it and commit"); !errors.Is(err, ErrResume) {
		t.Fatalf("want ErrResume, got %v", err)
	}
}

// TestResumeModeRefusalWrapsErrResume: the continuation's bundle is asserted
// at the wire again exactly as a fresh session's is, and a mode refused on
// the loaded session is a resume failure, not an ordinary session-config
// failure — the caller must be able to tell it apart and fall back cold
// rather than fail the build outright.
func TestResumeModeRefusalWrapsErrResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := runtimeFor(t, "resume-mode-refused")
	rt.Resume = "sess-1"
	if _, err := rt.Run(ctx, bundle(), "fix it and commit"); !errors.Is(err, ErrResume) {
		t.Fatalf("want ErrResume, got %v", err)
	}
}
