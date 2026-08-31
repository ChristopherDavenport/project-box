package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func setup(t *testing.T) (*Room, *queue.Queue) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Room{Store: s, Tenant: tn, Actor: "chris"},
		&queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
}

func submit(t *testing.T, q *queue.Queue, id string) queue.WorkItem {
	t.Helper()
	w, err := q.Submit(context.Background(), queue.WorkItem{ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestStopHaltsNewClaimsImmediately(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	w := submit(t, q, "wi-1")
	if err := q.Stop(ctx, tn, "chris", "investigating an incident"); err != nil {
		t.Fatal(err)
	}
	// The stated bound: not one new claim after the stop record lands.
	if _, _, err := q.Claim(ctx, w); !errors.Is(err, queue.ErrStopped) {
		t.Fatalf("claim after stop: want ErrStopped, got %v", err)
	}
	if err := q.Resume(ctx, tn, "chris", "root cause found"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Claim(ctx, w); err != nil {
		t.Fatalf("claim after resume: %v", err)
	}
	// Stop and resume are both records: who, when, why — never an erasure.
	kinds := []string{}
	for rec, err := range r.Store.Log().Since(ctx, tn, "control/claims", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, rec.Kind)
	}
	if len(kinds) != 2 || kinds[0] != "stop" || kinds[1] != "resume" {
		t.Fatalf("control chain: %v", kinds)
	}
}

func TestExceptionsListsOnlyWhatStillNeedsEyes(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	for _, id := range []string{"wi-a", "wi-b"} {
		w := submit(t, q, id)
		w, lease, err := q.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Park(ctx, w, lease, "gate failed"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exceptions: %+v", got)
	}
	// An item that moves on drops out of the room; the index remembers, the
	// fold shows what still needs eyes.
	w, err := (&queue.Queue{Store: r.Store, Runner: "runner-1", TTL: time.Hour}).Load(ctx, tn, "wi-a")
	if err != nil {
		t.Fatal(err)
	}
	w, lease, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, w, lease, forge.Commit("resolvedsha")); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Exceptions(ctx)
	if len(got) != 1 || got[0].ID != "wi-b" {
		t.Fatalf("after resolution: %+v", got)
	}
}

func TestSampleIsDeterministicAndRecorded(t *testing.T) {
	r, q := setup(t)
	ctx := context.Background()
	for _, id := range []string{"wi-1", "wi-2", "wi-3", "wi-4", "wi-5"} {
		w := submit(t, q, id)
		w, lease, err := q.Claim(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Done(ctx, w, lease, forge.Commit("sha-"+id)); err != nil {
			t.Fatal(err)
		}
	}
	a, err := r.Sample(ctx, 2, "2026-08-review")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Sample(ctx, 2, "2026-08-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0].ID != b[0].ID || a[1].ID != b[1].ID {
		t.Fatalf("same seed, different sample: %v vs %v", a, b)
	}
	c, _ := r.Sample(ctx, 2, "2026-09-review")
	if a[0].ID == c[0].ID && a[1].ID == c[1].ID {
		t.Fatal("different seed, same sample — suspicious for 5 choose 2")
	}
	if err := r.SignOff(ctx, a[0].ID, "pass", "looks right"); err != nil {
		t.Fatal(err)
	}
	if err := r.SignOff(ctx, a[1].ID, "", ""); err == nil {
		t.Fatal("a verdictless sign-off was accepted")
	}
	// The selections and the sign-off are all on one reviewable chain.
	kinds := map[string]int{}
	for rec, err := range r.Store.Log().Since(ctx, tn, "control/review", 1) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[rec.Kind]++
	}
	if kinds["review-sample"] != 3 || kinds["review-signoff"] != 1 {
		t.Fatalf("review chain: %v", kinds)
	}
}
