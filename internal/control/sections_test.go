package control

import (
	"context"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// submitFor is [submit] with the repo and held sections a section census
// test needs to control, which the plain fixture in control_test.go leaves
// at their zero values.
func submitFor(t *testing.T, q *queue.Queue, id, repo string, sections []string) queue.WorkItem {
	t.Helper()
	w, err := q.Submit(context.Background(), queue.WorkItem{
		ID: id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t",
		Repo: repo, Sections: sections,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// waitOnceThenReclaim drives one section-wait cycle exactly as
// [loop.Loop.WaitOnSection] does — claim, increment SectionWaits, wait —
// then advances the clock by held (simulating the time actually spent
// waiting) so the [land] call that follows produces the "claimed" transition
// record a section census's fold reads the wait's end from.
func waitOnceThenReclaim(t *testing.T, q *queue.Queue, c *clock, w queue.WorkItem, busy string, held time.Duration) queue.WorkItem {
	t.Helper()
	ctx := context.Background()
	w, lease, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	w.SectionWaits++
	w, err = q.Wait(ctx, w, lease, busy)
	if err != nil {
		t.Fatal(err)
	}
	c.advance(held)
	return w
}

func land(t *testing.T, q *queue.Queue, w queue.WorkItem, head string, landedSections, landedPaths []string) {
	t.Helper()
	w.LandedSections, w.LandedPaths = landedSections, landedPaths
	ctx := context.Background()
	_, lease, err := q.Claim(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, w, lease, forge.Commit(head)); err != nil {
		t.Fatal(err)
	}
}

// TestSectionsFoldsLandingsWaitsAndCoChanges is the issue's own fixture:
// three landings and two section-waits report the per-section landed
// counts, the wait counts and durations, and the one co-change pair. A
// fourth landing in a different repository proves the fold is scoped to the
// repo asked about, not the whole tenant.
func TestSectionsFoldsLandingsWaitsAndCoChanges(t *testing.T) {
	r, q, c := setupAt(t, time.Hour)
	const repo = "acme/repo"

	// wi-1 lands in alpha alone, no wait.
	w1 := submitFor(t, q, "wi-1", repo, []string{"alpha"})
	land(t, q, w1, "sha-1", []string{"alpha"}, []string{"alpha/x.go"})

	// wi-2 holds alpha, waits once for 90s, then lands in alpha and beta —
	// the co-change pair.
	w2 := submitFor(t, q, "wi-2", repo, []string{"alpha"})
	w2 = waitOnceThenReclaim(t, q, c, w2, "busy: alpha held by wi-9", 90*time.Second)
	land(t, q, w2, "sha-2", []string{"alpha", "beta"}, []string{"alpha/x.go", "beta/y.go"})

	// wi-3 holds beta, waits once for 30s, then lands in beta alone.
	w3 := submitFor(t, q, "wi-3", repo, []string{"beta"})
	w3 = waitOnceThenReclaim(t, q, c, w3, "busy: beta held by wi-2", 30*time.Second)
	land(t, q, w3, "sha-3", []string{"beta"}, []string{"beta/y.go"})

	// wi-4 lands in alpha too, but in a different repository — it must not
	// count toward acme/repo's tally.
	w4 := submitFor(t, q, "wi-4", "acme/other", []string{"alpha"})
	land(t, q, w4, "sha-4", []string{"alpha"}, []string{"alpha/x.go"})

	census, err := r.Sections(context.Background(), repo, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]SectionStat{}
	for _, s := range census.Sections {
		byName[s.Name] = s
	}
	if len(byName) != 2 {
		t.Fatalf("want exactly alpha and beta, got %v", census.Sections)
	}
	alpha, beta := byName["alpha"], byName["beta"]
	if alpha.Landed != 2 {
		t.Fatalf("alpha landed: want 2, got %d", alpha.Landed)
	}
	if beta.Landed != 2 {
		t.Fatalf("beta landed: want 2, got %d", beta.Landed)
	}
	if alpha.Waits != 1 || alpha.WaitSeconds == nil || *alpha.WaitSeconds != 90 {
		t.Fatalf("alpha waits: got %d waits, seconds %v", alpha.Waits, alpha.WaitSeconds)
	}
	if beta.Waits != 1 || beta.WaitSeconds == nil || *beta.WaitSeconds != 30 {
		t.Fatalf("beta waits: got %d waits, seconds %v", beta.Waits, beta.WaitSeconds)
	}
	if len(census.CoChanges) != 1 {
		t.Fatalf("want exactly one co-change pair, got %v", census.CoChanges)
	}
	if got := census.CoChanges[0]; got.Sections != [2]string{"alpha", "beta"} || got.Count != 1 {
		t.Fatalf("co-change: got %+v", got)
	}
}

// TestSectionsShowsWholeRepositoryAndUnsectionedBuckets: a landing that held
// no declared section (the whole repository) and a landing whose changed
// paths matched none of them ("unsectioned") each appear as their own
// bucket, distinct from every declared name.
func TestSectionsShowsWholeRepositoryAndUnsectionedBuckets(t *testing.T) {
	r, q := setup(t)
	const repo = "acme/repo"

	// A whole-repository unit: it held no section to build, and its
	// changed paths happen to land inside a declared one anyway — the
	// WholeRepository bucket is about what it HELD, not what it touched.
	whole := submitFor(t, q, "wi-whole", repo, nil)
	land(t, q, whole, "sha-whole", []string{"alpha"}, []string{"alpha/x.go"})

	// An ordinary unit whose changed path matches no declared section.
	stray := submitFor(t, q, "wi-stray", repo, []string{"alpha"})
	land(t, q, stray, "sha-stray", []string{"unsectioned"}, []string{"README.md"})

	census, err := r.Sections(context.Background(), repo, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]SectionStat{}
	for _, s := range census.Sections {
		byName[s.Name] = s
	}
	whole1, ok := byName[WholeRepository]
	if !ok || whole1.Landed != 1 {
		t.Fatalf("want the whole-repository bucket landed once, got %+v (all: %v)", whole1, census.Sections)
	}
	stray1, ok := byName["unsectioned"]
	if !ok || stray1.Landed != 1 {
		t.Fatalf("want the unsectioned bucket landed once, got %+v (all: %v)", stray1, census.Sections)
	}
	// The whole-repository unit's own landed path still counted for alpha,
	// same as always — WholeRepository is additive, not a substitute.
	if byName["alpha"].Landed != 1 {
		t.Fatalf("want alpha landed once (from the whole-repository unit's own touch), got %+v", byName["alpha"])
	}
}

// TestSectionsDeclaredSectionsAppearAtZero: a section nothing has landed or
// waited on yet is still reported, at zero — declared and known-zero, never
// simply absent.
func TestSectionsDeclaredSectionsAppearAtZero(t *testing.T) {
	r, q := setup(t)
	const repo = "acme/repo"
	w := submitFor(t, q, "wi-1", repo, []string{"alpha"})
	land(t, q, w, "sha-1", []string{"alpha"}, []string{"alpha/x.go"})

	census, err := r.Sections(context.Background(), repo, []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]SectionStat{}
	for _, s := range census.Sections {
		byName[s.Name] = s
	}
	for _, name := range []string{"beta", "gamma"} {
		s, ok := byName[name]
		if !ok {
			t.Fatalf("declared section %s missing from the census entirely", name)
		}
		if s.Landed != 0 || s.Waits != 0 || s.WaitSeconds != nil {
			t.Fatalf("declared, untouched section %s: want all-zero/nil, got %+v", name, s)
		}
	}
}
