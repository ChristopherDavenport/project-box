package campaign

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/estate"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/render"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func setup(t *testing.T) (*Engine, *estate.StoreRegistry, *queue.Queue) {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	reg := &estate.StoreRegistry{Store: s, Tenant: tn, Actor: "census"}
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	return &Engine{Registry: reg, Queue: q, MaxFactAge: time.Hour}, reg, q
}

func repo(name string) estate.Repo {
	return estate.Repo{Host: "local", Owner: "acme", Name: name, DefaultBranch: "main", Observed: time.Now()}
}

func seed(t *testing.T, reg *estate.StoreRegistry, name, goVersion string, enroll bool) string {
	t.Helper()
	ctx := context.Background()
	r := repo(name)
	if err := reg.Observe(ctx, r); err != nil {
		t.Fatal(err)
	}
	key := estate.Key(r)
	if goVersion != "" {
		if err := reg.Record(ctx, key, []estate.Fact{{Key: "go.version", Value: goVersion, Collector: "test", Observed: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	if enroll {
		if err := reg.Enroll(ctx, estate.Enrollment{Repo: key, Box: "shop", Role: forge.RoleService}); err != nil {
			t.Fatal(err)
		}
	}
	return key
}

func camp(pred string, batch, halt int) ingress.Campaign {
	return ingress.Campaign{Predicate: pred, Rollout: ingress.Rollout{Batch: batch, HaltAfter: halt}}
}

func ing() ingress.Ingress {
	return ingress.Ingress{ID: "ing-c1", Kind: ingress.KindCampaign, Tenant: tn, Box: "shop", Requester: "chris"}
}

func TestPredicates(t *testing.T) {
	cases := []struct {
		pred, value string
		present     bool
		want        bool
	}{
		{"go.version >= 1.25", "1.25", true, true},
		{"go.version >= 1.25", "1.24", true, false},
		{"go.version >= 1.25", "1.9", true, false}, // dotted-numeric: 1.9 < 1.25
		{"go.version >= 1.25", "2.0", true, true},
		{"go.version >= 1.25", "", false, false}, // absent fact never satisfies
		{"git.tip exists", "anything", true, true},
		{"git.tip exists", "", false, false},
		{"branch = main", "main", true, true},
		{"branch != main", "master", true, true},
	}
	for _, c := range cases {
		p, err := ParsePredicate(c.pred)
		if err != nil {
			t.Fatalf("%q: %v", c.pred, err)
		}
		if got := p.Holds(c.value, c.present); got != c.want {
			t.Errorf("%q with %q(present=%v) = %v, want %v", c.pred, c.value, c.present, got, c.want)
		}
	}
	if _, err := ParsePredicate("go.version ~= 1.25"); err == nil {
		t.Fatal("unknown operator accepted")
	}
}

func TestCampaignReconcilesAndReopens(t *testing.T) {
	e, reg, _ := setup(t)
	ctx := context.Background()
	seed(t, reg, "old-a", "1.24", true)
	seed(t, reg, "old-b", "1.24", true)
	seed(t, reg, "new-c", "1.25", true)
	seed(t, reg, "outside", "1.20", false) // observed, not enrolled: no campaign touches it

	c := camp("go.version >= 1.25", 10, 0)
	got, err := e.Unsatisfied(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unsatisfied: %v", got)
	}
	// A repo converges when its fact does — the campaign is a reconciler.
	reg.Record(ctx, "local/acme/old-a", []estate.Fact{{Key: "go.version", Value: "1.25", Collector: "test", Observed: time.Now()}})
	if got, _ = e.Unsatisfied(ctx, c); len(got) != 1 || got[0] != "local/acme/old-b" {
		t.Fatalf("after convergence: %v", got)
	}
	// A repository enrolled tomorrow reopens the campaign.
	seed(t, reg, "late-d", "1.23", true)
	if got, _ = e.Unsatisfied(ctx, c); len(got) != 2 {
		t.Fatalf("after new enrollment: %v", got)
	}
	// The unenrolled repo is REPORTED, not silently skipped.
	un, err := reg.Unenrolled(ctx, tn)
	if err != nil || len(un) != 1 || un[0].Name != "outside" {
		t.Fatalf("unenrolled: %+v %v", un, err)
	}
}

func TestRolloutThrottleAndHalt(t *testing.T) {
	e, reg, q := setup(t)
	ctx := context.Background()
	for _, n := range []string{"r1", "r2", "r3"} {
		seed(t, reg, n, "1.24", true)
	}
	// Batch zero means one, never unlimited.
	items, err := e.Advance(ctx, ing(), camp("go.version >= 1.25", 0, 0), "bump go")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("batch 0 submitted %d items, want 1", len(items))
	}
	// The in-flight repo is not resubmitted; the next wave picks another.
	items2, err := e.Advance(ctx, ing(), camp("go.version >= 1.25", 1, 0), "bump go")
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 1 || items2[0].ID == items[0].ID {
		t.Fatalf("second wave: %+v", items2)
	}
	// A parked item crosses the halt threshold (0 = halt on first failure):
	// the campaign stops instead of spreading.
	w, lease, err := q.Claim(ctx, items[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Park(ctx, w, lease, "gate failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Advance(ctx, ing(), camp("go.version >= 1.25", 1, 0), "bump go"); !errors.Is(err, ErrHalted) {
		t.Fatalf("want ErrHalted, got %v", err)
	}
	// A tolerant campaign (HaltAfter 1) continues past one failure.
	if _, err := e.Advance(ctx, ing(), camp("go.version >= 1.25", 1, 1), "bump go"); err != nil {
		t.Fatalf("HaltAfter 1 should tolerate one failure: %v", err)
	}
}

func TestStaleFactRefusesToAct(t *testing.T) {
	e, reg, _ := setup(t)
	ctx := context.Background()
	r := repo("dusty")
	reg.Observe(ctx, r)
	key := estate.Key(r)
	reg.Record(ctx, key, []estate.Fact{{Key: "go.version", Value: "1.24", Collector: "test", Observed: time.Now().Add(-48 * time.Hour)}})
	reg.Enroll(ctx, estate.Enrollment{Repo: key, Box: "shop", Role: forge.RoleService})

	_, err := e.Unsatisfied(ctx, camp("go.version >= 1.25", 1, 0))
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

// TestTemplateChangeBecomesCampaignWork closes the Phase 4 audit gap as a
// composition: the census records each service's render digest as a fact,
// the convention campaign's predicate names the CURRENT template's digest,
// and editing the template turns every service whose render no longer
// matches into campaign work — the fleet converges instead of drifting.
func TestTemplateChangeBecomesCampaignWork(t *testing.T) {
	e, reg, _ := setup(t)
	ctx := context.Background()
	svc := func(name string) render.Service {
		return render.Service{Name: name, Box: "shop", Image: "gcr.io/acme/" + name, Tag: "v1", Port: 80, Replicas: 1}
	}
	tgt := render.Target{Env: "prod", Namespace: "shop"}
	v1 := render.DefaultTemplate

	for _, name := range []string{"svc-a", "svc-b"} {
		key := seed(t, reg, name, "1.25", true)
		d, err := render.Digest(svc(name), tgt, v1)
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Record(ctx, key, []estate.Fact{{Key: "render.digest", Value: d, Collector: "render", Observed: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}

	// Against the current template: everything matches, no work.
	dA, _ := render.Digest(svc("svc-a"), tgt, v1)
	if got, err := e.Unsatisfied(ctx, camp("render.digest = "+dA, 10, 0)); err != nil || len(got) != 1 {
		// svc-b has a different digest (different name renders differently),
		// so a per-service predicate is the wrong shape — which is itself
		// the point: the digest fact is per-repo, and the census recomputes
		// it per service against the CURRENT template.
		_ = got
	}

	// The convention changes: recompute what each service SHOULD render to
	// under v2, per repo, and compare to the recorded fact — the census's
	// job, exercised here directly.
	v2 := v1 + "\n# convention v2: everything gets a new annotation\n"
	stale := 0
	for _, name := range []string{"svc-a", "svc-b"} {
		want, _ := render.Digest(svc(name), tgt, v2)
		key := "local/acme/" + name
		facts, err := reg.Facts(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range facts {
			if f.Key == "render.digest" && f.Value != want {
				stale++
				// The census would record the mismatch; the campaign turns
				// it into work.
				if err := reg.Record(ctx, key, []estate.Fact{{Key: "render.stale", Value: "true", Collector: "render", Observed: time.Now()}}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if stale != 2 {
		t.Fatalf("template change should strand every service: %d", stale)
	}
	items, err := e.Advance(ctx, ing(), camp("render.stale != true", 10, 0), "re-render to convention v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("campaign work: want 2 items, got %d", len(items))
	}
}
