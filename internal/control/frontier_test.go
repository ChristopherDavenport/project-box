package control

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
)

// clock is one movable now shared by the store and the room. Both have to see
// the same time or a lease test measures the skew between two clocks rather
// than the property.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func setupAt(t *testing.T, ttl time.Duration) (*Room, *queue.Queue, *clock) {
	t.Helper()
	c := &clock{at: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)}
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"), sqlite.WithClock(c.now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Room{Store: s, Tenant: tn, Actor: "chris", Now: c.now},
		&queue.Queue{Store: s, Runner: "runner-1", TTL: ttl}, c
}

func rows(t *testing.T, r *Room) map[string]QueueEntry {
	t.Helper()
	entries, err := r.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]QueueEntry{}
	for _, e := range entries {
		out[e.ID] = e
	}
	return out
}

func submitDeps(t *testing.T, q *queue.Queue, id string, deps ...string) queue.WorkItem {
	t.Helper()
	w, err := q.Submit(context.Background(), queue.WorkItem{
		ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t", DependsOn: deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// The frontier's headline question: of the work nobody is doing, which of it
// COULD be done. "Pending" says the same word for an item that is next and an
// item behind something dead, so the state alone cannot answer it — and a
// factory with capacity and nothing moving is indistinguishable from a
// factory with nothing to do.
func TestFrontierSeparatesReadyFromBlocked(t *testing.T) {
	r, q, _ := setupAt(t, time.Hour)
	ctx := context.Background()

	root := submitDeps(t, q, "wi-root")
	submitDeps(t, q, "wi-leaf", "wi-root")
	held := submitDeps(t, q, "wi-held")
	resumed := submitDeps(t, q, "wi-resumed")

	// Held on a gate's answer: claimable by the drain, but only so it can ask
	// again. Counting it as runnable would report capacity spent on somebody
	// else's decision as work the factory could be doing.
	_, lease, err := q.Claim(ctx, held)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Wait(ctx, held, lease, "reviewer has not answered"); err != nil {
		t.Fatal(err)
	}
	// Handed off mid-change: a fresh session is owed and nobody is waited on.
	_, lease, err = q.Claim(ctx, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.HandOff(ctx, resumed, lease, "out of context"); err != nil {
		t.Fatal(err)
	}

	got := rows(t, r)
	for id, want := range map[string]bool{
		"wi-root": true, "wi-leaf": false, "wi-held": false, "wi-resumed": true,
	} {
		if got[id].Ready != want {
			t.Errorf("%s (%s): ready=%v, want %v", id, got[id].State, got[id].Ready, want)
		}
	}
	if b := got["wi-leaf"].BlockedBy; len(b) != 1 || b[0] != "wi-root" {
		t.Fatalf("a blocked row must name what blocks it: %v", b)
	}

	// The root lands and the leaf joins the frontier — the edge moving is the
	// whole point of the view.
	_, lease, err = q.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(t, r)["wi-root"]; got.Ready {
		t.Fatal("a claimed item is not on the frontier: somebody is already running it")
	}
	if _, err := q.Done(ctx, root, lease, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := rows(t, r)["wi-leaf"]; !got.Ready || len(got.BlockedBy) != 0 {
		t.Fatalf("leaf after its dependency settled: ready=%v blocked=%v", got.Ready, got.BlockedBy)
	}
}

// Who holds a claim and how long they have left is the one row fact that is
// NOT a fold: an item's chain records that a runner claimed it and can never
// record that the runner went away.
func TestFrontierNamesTheClaimHolderAndWhenItLapses(t *testing.T) {
	r, q, c := setupAt(t, 30*time.Minute)
	ctx := context.Background()
	w := submitDeps(t, q, "wi-1")
	if _, _, err := q.Claim(ctx, w); err != nil {
		t.Fatal(err)
	}

	got := rows(t, r)["wi-1"]
	if got.Runner != "runner-1" {
		t.Fatalf("claim holder: got %q, want runner-1", got.Runner)
	}
	if got.LeaseExpiresSec != int64((30*time.Minute).Seconds()) || got.LeaseLapsed {
		t.Fatalf("fresh claim: %ds left, lapsed=%v", got.LeaseExpiresSec, got.LeaseLapsed)
	}

	// The runner dies. Nothing records that, and the state stays "claimed"
	// until a drain recovers it — so a lapsed lease is the only evidence the
	// room has that this row is work nobody is doing.
	c.advance(time.Hour)
	got = rows(t, r)["wi-1"]
	if got.State != queue.StateClaimed {
		t.Fatalf("state should still read claimed, got %q", got.State)
	}
	if !got.LeaseLapsed || got.LeaseExpiresSec != -int64((30*time.Minute).Seconds()) {
		t.Fatalf("dead runner: lapsed=%v, %ds left", got.LeaseLapsed, got.LeaseExpiresSec)
	}
	if got.Runner != "runner-1" {
		t.Fatalf("a lapsed claim must still name who took it, got %q", got.Runner)
	}

	// Settling releases the claim, and no row may then read as held: a stale
	// runner name beside a done item is worse than none, because it names work
	// in progress that nobody is doing.
	w, lease, err := q.Claim(ctx, queue.WorkItem{
		ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, w, lease, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := rows(t, r)["wi-1"]; got.Runner != "" || got.LeaseLapsed {
		t.Fatalf("settled item still reads as held: %+v", got)
	}
}

// A cascade parks leaf by leaf, and Claim writes each cause as prose nesting
// the one above it. The root's reason is in the string and is not a field, so
// nothing can group the queue by what broke or count what one failure holds up.
func TestCascadeNamesTheRootAndNotTheNeighbour(t *testing.T) {
	r, q, _ := setupAt(t, time.Hour)
	ctx := context.Background()

	root := submitDeps(t, q, "wi-root")
	middle := submitDeps(t, q, "wi-middle", "wi-root")
	leaf := submitDeps(t, q, "wi-leaf", "wi-middle")
	loose := submitDeps(t, q, "wi-loose")

	_, lease, err := q.Claim(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, root, lease, queue.SeverityHigh, "the credential was revoked"); err != nil {
		t.Fatal(err)
	}
	// Each claim cascades one level; that is the queue's own behaviour and the
	// reason the room has to walk further than the note does.
	if _, _, err := q.Claim(ctx, middle); err == nil {
		t.Fatal("claiming behind a parked dependency must refuse")
	}
	if _, _, err := q.Claim(ctx, leaf); err == nil {
		t.Fatal("claiming behind a cascaded dependency must refuse")
	}
	// A parking of its own, sharing nothing with the cascade above.
	_, lease, err = q.Claim(ctx, loose)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, loose, lease, queue.SeverityLow, "flaky test"); err != nil {
		t.Fatal(err)
	}

	got := rows(t, r)
	// The prose does carry the origin — nested, and longer at every level.
	// Pinned here because it is what Root sits beside rather than replaces:
	// the fact is in a string, so nothing can group by it or count what one
	// failure is holding up.
	if n := got["wi-leaf"].Note; !strings.Contains(n, "dependency wi-middle parked: dependency wi-root parked:") {
		t.Fatalf("the cascade note is expected to nest its cause: %q", n)
	}
	if got["wi-leaf"].Root != "wi-root" {
		t.Fatalf("the leaf must name the ROOT, not its neighbour: %q", got["wi-leaf"].Root)
	}
	if got["wi-middle"].Root != "wi-root" {
		t.Fatalf("middle: %q", got["wi-middle"].Root)
	}
	// An item that died on its own account is not a cascade and must not
	// invent an origin for itself.
	if got["wi-root"].Root != "" || got["wi-loose"].Root != "" {
		t.Fatalf("own-account parkings must name no root: root=%q loose=%q",
			got["wi-root"].Root, got["wi-loose"].Root)
	}
}
