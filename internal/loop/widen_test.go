package loop

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// widened is the loop's repository with two regions declared, named for
// issue #182's own example: a unit expressed in loop escalating for agent.
func widened(t *testing.T) *env {
	t.Helper()
	e := setup(t, []string{"git", "log", "-1"})
	e.loop.Repo.Sections = []forge.Section{
		{Name: "loop", Purpose: "the loop's own transaction", Paths: []string{"internal/loop"}},
		{Name: "agent", Purpose: "the agent plane", Paths: []string{"internal/agent"}},
	}
	return e
}

// escalatingWith always escalates with the fields given — every call, not
// just the first — so a test asserting a guard denied a widen is not merely
// watching the retry budget run out with the same "escalated by" note
// either way.
type escalatingWith Escalation

func (e escalatingWith) Build(context.Context, forge.Workspace, string) (forge.Commit, error) {
	esc := Escalation(e)
	return "", &esc
}

// escalateThenBuild escalates once, then builds like scriptedBuilder — the
// shape a granted widen's rebuild takes. inspect, when set, runs at the very
// start of the SECOND call, the one point in the whole run where a widen's
// extra section lease is held and the first call's kept commit is still the
// tree's HEAD, before the rebuild adds anything of its own.
type escalateThenBuild struct {
	esc   *Escalation
	calls int
	// partialSHA is the commit the first call left, captured the moment it
	// is made — the kept commit the rebuild's base should equal.
	partialSHA string
	inspect    func(ws forge.Workspace, keptHead string)
}

func (b *escalateThenBuild) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	b.calls++
	if b.calls == 1 {
		if err := os.WriteFile(filepath.Join(ws.Dir, "partial.txt"), []byte("partial\n"), 0o644); err != nil {
			return "", err
		}
		if _, err := sh(ws.Dir, "git", "add", "."); err != nil {
			return "", err
		}
		if _, err := sh(ws.Dir, "git", "commit", "-m", "build: partial progress before escalating"); err != nil {
			return "", err
		}
		head, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		b.partialSHA = strings.TrimSpace(head)
		return "", b.esc
	}
	if b.inspect != nil {
		head, err := sh(ws.Dir, "git", "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		b.inspect(ws, strings.TrimSpace(head))
	}
	return scriptedBuilder{}.Build(ctx, ws, task)
}

// TestEscalationWidenTestsRebuildsInWidenedSections is issue #182's part (1)
// and (3): a widen: tests loop grants a structured, test-files-only
// escalation in-claim — widening item.Sections, holding both section leases
// through the rebuild, and rebuilding on top of the kept commit — instead of
// parking.
func TestEscalationWidenTestsRebuildsInWidenedSections(t *testing.T) {
	e := widened(t)
	e.loop.Widen = WidenTests
	e.loop.Attempts = 2
	ctx := context.Background()

	item := e.submit(t, "widen-1", func(w *queue.WorkItem) { w.Sections = []string{"loop"} })

	esc := &Escalation{
		Seat: "mason", Reason: "need the agent section too",
		NeedsSections: []string{"agent"},
		Paths:         []string{"internal/agent/foo_test.go"},
	}
	var sawLoop, sawAgent bool
	var rebuildBase string
	builder := &escalateThenBuild{esc: esc}
	builder.inspect = func(ws forge.Workspace, head string) {
		rebuildBase = head
		leaser := e.store.Leaser("runner-1")
		if cur, ok, err := leaser.Inspect(ctx, tn, e.repo.Name, SectionRef("loop")); err == nil && ok && cur.Run == item.ID {
			sawLoop = true
		}
		if cur, ok, err := leaser.Inspect(ctx, tn, e.repo.Name, SectionRef("agent")); err == nil && ok && cur.Run == item.ID {
			sawAgent = true
		}
	}
	e.loop.Builder = builder

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("widened rebuild did not land: %s (%s)", item.State, item.Note)
	}
	if want := []string{"loop", "agent"}; !slices.Equal(item.Sections, want) {
		t.Fatalf("sections after widen = %v, want %v", item.Sections, want)
	}
	if !sawLoop || !sawAgent {
		t.Fatalf("both section leases were not held through the rebuild: loop=%v agent=%v", sawLoop, sawAgent)
	}
	if rebuildBase == "" || rebuildBase != builder.partialSHA {
		t.Fatalf("the rebuild's base = %q, want the kept commit %q", rebuildBase, builder.partialSHA)
	}

	var amended int
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/"+item.ID, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "plan-amended" {
			amended++
			if !strings.Contains(string(r.Body), "agent") {
				t.Fatalf("plan-amended record does not name the escalation it answers: %s", r.Body)
			}
		}
	}
	if amended != 1 {
		t.Fatalf("expected exactly one plan-amended record, got %d", amended)
	}

	// The rebuild was based on top of the kept commit from the escalating
	// attempt, not a fresh checkout of base: the workspace is one per claim.
	gitLog := mustSh(t, e.remote, "git", "log", "--oneline", "-5", "refs/heads/main")
	if strings.Contains(gitLog, "partial progress before escalating") {
		t.Fatalf("the partial commit should have been folded by the squash, not landed separately: %s", gitLog)
	}
}

// TestEscalationWidenTestsSectionBusyWaits: a grantable widen whose extra
// section is held by another run does not spin and does not park — it
// persists the amendment and waits, so the next claim's leaseSections takes
// the union in one sorted order instead of the guard ever being asked
// twice.
func TestEscalationWidenTestsSectionBusyWaits(t *testing.T) {
	e := widened(t)
	e.loop.Widen = WidenTests
	e.loop.Attempts = 2
	ctx := context.Background()
	hold(t, e, "agent", "wi-elsewhere")

	item := e.submit(t, "widen-busy", func(w *queue.WorkItem) { w.Sections = []string{"loop"} })
	e.loop.Builder = escalatingWith{
		Seat: "mason", Reason: "need the agent section too",
		NeedsSections: []string{"agent"},
		Paths:         []string{"internal/agent/foo_test.go"},
	}

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("expected the item to wait on the busy section rather than park, got %s: %s", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "agent") || !strings.Contains(item.Note, "wi-elsewhere") {
		t.Fatalf("wait note does not name the busy section or its holder: %q", item.Note)
	}
	if want := []string{"loop", "agent"}; !slices.Equal(item.Sections, want) {
		t.Fatalf("the amendment did not persist despite the wait: %v", item.Sections)
	}
	assertNoPlanAmended(t, e, item.ID)
}

// TestEscalationWidenNoneOrEmptyParksExactlyAsToday: with no widen policy —
// "none" or the zero value, which must mean the same thing — a structured
// escalation still parks exactly as a free-text one always has, and
// item.Sections is untouched: nothing was amended just because the
// escalation happened to carry needs_sections and test-only paths.
func TestEscalationWidenNoneOrEmptyParksExactlyAsToday(t *testing.T) {
	for _, policy := range []string{WidenNone, ""} {
		t.Run("policy="+policy, func(t *testing.T) {
			e := widened(t)
			e.loop.Widen = policy
			e.loop.Attempts = 2
			ctx := context.Background()

			item := e.submit(t, "widen-none-"+policy, func(w *queue.WorkItem) { w.Sections = []string{"loop"} })
			e.loop.Builder = escalatingWith{
				Seat: "mason", Reason: "need the agent section too",
				NeedsSections: []string{"agent"},
				Paths:         []string{"internal/agent/foo_test.go"},
			}

			item, err := e.loop.Run(ctx, item)
			if err != nil {
				t.Fatal(err)
			}
			if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
				t.Fatalf("expected a park attributed to the seat, got %s: %s", item.State, item.Note)
			}
			if want := []string{"loop"}; !slices.Equal(item.Sections, want) {
				t.Fatalf("sections widened anyway: %v", item.Sections)
			}
			assertNoPlanAmended(t, e, item.ID)
		})
	}
}

// TestEscalationWidenTestsNoPathsParks: a NeedsSections with no Paths is not
// readable as test-only by omission — this repository's rule that unknown
// never reads as zero — so it parks exactly as today, unwidened.
func TestEscalationWidenTestsNoPathsParks(t *testing.T) {
	e := widened(t)
	e.loop.Widen = WidenTests
	e.loop.Attempts = 2
	ctx := context.Background()

	item := e.submit(t, "widen-nopaths", func(w *queue.WorkItem) { w.Sections = []string{"loop"} })
	e.loop.Builder = escalatingWith{
		Seat: "mason", Reason: "need the agent section too",
		NeedsSections: []string{"agent"},
	}

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("expected a park attributed to the seat, got %s: %s", item.State, item.Note)
	}
	if want := []string{"loop"}; !slices.Equal(item.Sections, want) {
		t.Fatalf("sections widened anyway: %v", item.Sections)
	}
	assertNoPlanAmended(t, e, item.ID)
}

// TestEscalationWidenTestsUndeclaredSectionParks: a NeedsSections name the
// repository never declared is the plan and the repository disagreeing
// about where this unit works — parks exactly as today, unwidened.
func TestEscalationWidenTestsUndeclaredSectionParks(t *testing.T) {
	e := widened(t)
	e.loop.Widen = WidenTests
	e.loop.Attempts = 2
	ctx := context.Background()

	item := e.submit(t, "widen-undeclared", func(w *queue.WorkItem) { w.Sections = []string{"loop"} })
	e.loop.Builder = escalatingWith{
		Seat: "mason", Reason: "need a section that does not exist",
		NeedsSections: []string{"ghost"},
		Paths:         []string{"ghost/foo_test.go"},
	}

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("expected a park attributed to the seat, got %s: %s", item.State, item.Note)
	}
	if want := []string{"loop"}; !slices.Equal(item.Sections, want) {
		t.Fatalf("sections widened anyway: %v", item.Sections)
	}
	assertNoPlanAmended(t, e, item.ID)
}

// TestEscalationWithANonTestPathParks: one path outside test shape means
// the need is not provably test-files-only, so this parks exactly as today
// — part 2 of issue #182 moves this case to the planner instead; this unit
// only has to leave it parking.
func TestEscalationWithANonTestPathParks(t *testing.T) {
	e := widened(t)
	e.loop.Widen = WidenTests
	e.loop.Attempts = 2
	ctx := context.Background()

	item := e.submit(t, "widen-nontest", func(w *queue.WorkItem) { w.Sections = []string{"loop"} })
	e.loop.Builder = escalatingWith{
		Seat: "mason", Reason: "need the agent section too",
		NeedsSections: []string{"agent"},
		Paths:         []string{"internal/agent/foo.go"},
	}

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.Contains(item.Note, "escalated by mason") {
		t.Fatalf("expected a park attributed to the seat, got %s: %s", item.State, item.Note)
	}
	if want := []string{"loop"}; !slices.Equal(item.Sections, want) {
		t.Fatalf("sections widened anyway: %v", item.Sections)
	}
	assertNoPlanAmended(t, e, item.ID)
}

func assertNoPlanAmended(t *testing.T, e *env, id string) {
	t.Helper()
	ctx := context.Background()
	for r, err := range e.store.Log().Since(ctx, tn, "workitem/"+id, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "plan-amended" {
			t.Fatalf("plan-amended record exists though the guard should have refused to widen: %s", r.Body)
		}
	}
}
