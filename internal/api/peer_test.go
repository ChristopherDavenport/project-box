package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// parkedItem submits, claims, and parks one work item directly through the
// store — the exception-queue state a retry/resolve test needs, without
// going through ingress/pump plumbing this test has no use for.
func parkedItem(t *testing.T, w *world, id string) {
	t.Helper()
	ctx := context.Background()
	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	item, err := q.Submit(ctx, queue.WorkItem{ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: id})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := q.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, item, lease, queue.SeverityMedium, "parked for "+id); err != nil {
		t.Fatal(err)
	}
}

// retriedRecord reads back the one "retried" record off an item's own chain
// — the evidence a retry over the wire must have written.
func retriedRecord(t *testing.T, w *world, id string) evidenceRecordFields {
	t.Helper()
	for rec, err := range w.srv.Store.Log().Since(context.Background(), tn, "workitem/"+id, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "retried" {
			return evidenceRecordFields{Actor: rec.Actor, Pid: rec.Pid, OnBehalfOf: rec.OnBehalfOf}
		}
	}
	t.Fatalf("no retried record on %s's chain", id)
	return evidenceRecordFields{}
}

type evidenceRecordFields struct {
	Actor      string
	Pid        int32
	OnBehalfOf string
}

// dialUnix serves w.srv over a real unix socket with peer-credential wiring
// (ConnContext) — the transport mustActor actually resolves a peer over,
// unlike the TCP httptest servers this file's read-only tests use.
func dialUnix(t *testing.T, w *world, actor string) *Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler(), ConnContext: ConnContext}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", actor)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRetryOverSocketRecordsThePeerIdentity is issue #184's core assertion:
// a write with no X-Box-Actor claim records the wire's OWN peer — the test
// process's account name (or uid:<n>, unresolvable) as Actor, and its pid,
// never a header and never "local-operator".
func TestRetryOverSocketRecordsThePeerIdentity(t *testing.T) {
	w := rig(t)
	parkedItem(t, w, "wi-1")

	c := dialUnix(t, w, "")
	if _, err := c.Reopen(context.Background(), "retry", "wi-1", "try again"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	rec := retriedRecord(t, w, "wi-1")
	wantActor := accountName(uint32(os.Getuid()))
	if rec.Actor != wantActor {
		t.Fatalf("actor = %q, want the peer's own account %q", rec.Actor, wantActor)
	}
	if rec.Pid != int32(os.Getpid()) {
		t.Fatalf("pid = %d, want this process's own pid %d", rec.Pid, os.Getpid())
	}
	if rec.OnBehalfOf != "" {
		t.Fatalf("on_behalf_of = %q, want empty — no claim was made and the item named no requester", rec.OnBehalfOf)
	}
}

// TestMutationOverTCPIsRefusedForWantOfAPeer is issue #184's core rule, not
// just the allowlist corner of it: a connection with no resolvable peer —
// here, a plain TCP httptest server with no ConnContext at all, the shape a
// stray network listener or an unported transport would have — is REFUSED
// outright rather than defaulted to any name (never "local-operator", never
// the bare claim). Without this test, deleting mustActor's !cred.ok branch
// entirely (defaulting straight to a name) left every other test in this
// package green: the unix-socket tests always carry a real peer, so nothing
// else here can see the refusal's absence.
func TestMutationOverTCPIsRefusedForWantOfAPeer(t *testing.T) {
	w := rig(t)
	parkedItem(t, w, "wi-1")

	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/v1/items/wi-1/retry", "application/json",
		strings.NewReader(`{"reason":"try again"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (refused for want of a peer): %s", resp.StatusCode, http.StatusForbidden, body)
	}
	if !strings.Contains(string(body), "peer") {
		t.Fatalf("body = %s, want it to name the missing peer", body)
	}

	item, loadErr := w.srv.Queue.Load(context.Background(), tn, "wi-1")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if item.State != queue.StateParked {
		t.Fatalf("state = %s, want the refused call to leave the item parked", item.State)
	}
}

// TestActorClaimRefusedOutsideTheAllowlist: a claim naming a subject this
// box never allowlisted refuses the WHOLE request — not a silent fallback to
// acting as the bare peer — and the item is left exactly as it was.
func TestActorClaimRefusedOutsideTheAllowlist(t *testing.T) {
	w := rig(t)
	parkedItem(t, w, "wi-1")
	// No ActorClaims configured: every claim is refused, matching cmd/boxd's
	// flag-only path.

	c := dialUnix(t, w, "someone")
	_, err := c.Reopen(context.Background(), "retry", "wi-1", "try again")
	if err == nil {
		t.Fatal("a claim outside the allowlist was accepted")
	}
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusForbidden || !strings.Contains(apiErr.Message, "allowlist") {
		t.Fatalf("err = %v, want a 403 naming the allowlist", err)
	}

	item, loadErr := w.srv.Queue.Load(context.Background(), tn, "wi-1")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if item.State != queue.StateParked {
		t.Fatalf("state = %s, want the refused claim to leave the item parked", item.State)
	}
}

// TestActorClaimAllowedLandsInOnBehalfOf: an allowlisted claim narrows —
// OnBehalfOf carries it — but never replaces the peer, which stays Actor no
// matter what the caller claimed to be.
func TestActorClaimAllowedLandsInOnBehalfOf(t *testing.T) {
	w := rig(t)
	w.srv.ActorClaims = []string{"alice"}
	parkedItem(t, w, "wi-1")

	c := dialUnix(t, w, "alice")
	if _, err := c.Reopen(context.Background(), "retry", "wi-1", "try again"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	rec := retriedRecord(t, w, "wi-1")
	wantActor := accountName(uint32(os.Getuid()))
	if rec.Actor != wantActor {
		t.Fatalf("actor = %q, want the peer %q even with an allowed claim", rec.Actor, wantActor)
	}
	if rec.OnBehalfOf != "alice" {
		t.Fatalf("on_behalf_of = %q, want the allowed claim %q", rec.OnBehalfOf, "alice")
	}
}
