package loop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// alwaysRefuseLand is a Lander that always errors landing — the shape of a
// GitHub 500 opening a pull request (issue #162) — optionally cancelling the
// claim's own ctx right as it hands back the error, so a test can prove the
// detached-context releases still run even though the ctx that spawned the
// claim is already dead by the time the park happens.
type alwaysRefuseLand struct {
	cancel context.CancelFunc
}

func (l *alwaysRefuseLand) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if l.cancel != nil {
		l.cancel()
	}
	return forge.Landing{}, errors.New("github: 500 Internal Server Error opening the pull request")
}

// claimLeaseRefs names every lease [Loop.Run] takes across one attempt in a
// repository sectioned as [sectioned] declares it: the section(s) named,
// the branch, and the item's own workspace.
func claimLeaseRefs(item queue.WorkItem, sections ...string) []string {
	refs := make([]string, 0, len(sections)+2)
	for _, s := range sections {
		refs = append(refs, SectionRef(s))
	}
	return append(refs, "refs/heads/main", "run/"+item.ID)
}

// assertNoneHeld probes each ref with a context of its own — never the
// claim's, which a canceled-ctx test deliberately kills — since what is
// under test is the STORE's state after the claim returned, not whether a
// probe sharing the dead ctx would itself succeed.
func assertNoneHeld(t *testing.T, e *env, refs []string) {
	t.Helper()
	for _, ref := range refs {
		if _, ok, err := e.store.Leaser("runner-1").Inspect(context.Background(), tn, e.repo.Name, ref); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Errorf("lease %s is still held; a claim's park must release everything it holds", ref)
		}
	}
}

// TestAParkAtLandReleasesEveryLeaseTheClaimHeld is issue #162's part (1): a
// GitHub 500 opening the PR parks the item at the land step, and in the SAME
// call every lease the claim took — its section, the branch, and its own
// workspace — is gone. Before this fix the deferred releases ran under the
// stage's own ctx; here nothing cancels it, so this variant alone would have
// passed even before the fix. Its purpose is the baseline for the canceled
// variant below.
func TestAParkAtLandReleasesEveryLeaseTheClaimHeld(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return &alwaysRefuseLand{} }

	item := e.submit(t, "land-refused", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateParked || !strings.HasPrefix(item.Note, "land: ") {
		t.Fatalf("a refused landing should park at the land step, got %s (%s)", item.State, item.Note)
	}
	assertNoneHeld(t, e, claimLeaseRefs(item, "api"))
}

// TestAParkAtLandUnderACanceledCtxStillReleasesEveryLease is issue #162's
// part (1) in the shape the incident actually took: the process does not
// merely return an error, the ctx driving the claim dies mid-park (a
// restart, a drain window elapsing — see daemon.go's workCtx cancellation).
// The lander cancels the very ctx [Loop.Run] is using right as it reports
// the land failure, before the park's deferred releases run. Every release
// [Loop.Run] owes uses context.WithoutCancel plus its own short timeout, so
// it must free the section, the branch, and the workspace regardless of
// whether the ctx that spawned the claim is already dead — and regardless
// of whether ParkKeeping's own write against that dead ctx succeeds, which
// is not what this test is about.
func TestAParkAtLandUnderACanceledCtxStillReleasesEveryLease(t *testing.T) {
	e := sectioned(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.loop.PRLander = func(inner *git.Lander) forge.Lander { return &alwaysRefuseLand{cancel: cancel} }

	item := e.submit(t, "land-refused-canceled", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	item, _ = e.loop.Run(ctx, item) // the ctx died mid-park; this call's own error is not the assertion.
	assertNoneHeld(t, e, claimLeaseRefs(item, "api"))
}

// TestAGhostHeldSectionIsReapedRecordedAndClaimedInTheSameTick is issue
// #162's part (2) and (3): a section held by a run whose own item is not
// [queue.StateClaimed] — left pending here, the simplest of the states that
// disqualify a holder from protection — is not a live claim to wait out. The
// claiming unit reaps it, writes a "lease-reaped" evidence record naming the
// ref, the ghost run, the state that condemned it, and the item that reaped
// it, and proceeds through to a landed commit in the same tick rather than
// waiting a turn.
func TestAGhostHeldSectionIsReapedRecordedAndClaimedInTheSameTick(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	ghost := e.submit(t, "ghost-holder", nil) // left pending: never claimed, so its lease is a ghost
	hold(t, e, "api", ghost.ID)

	item := e.submit(t, "reaps-the-ghost", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a ghost-held section should be reaped and the claim completed in the same tick, got %s: %s", item.State, item.Note)
	}

	found := false
	for r, err := range e.store.Log().BySubject(ctx, tn, SectionRef("api")) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != "lease-reaped" {
			continue
		}
		found = true
		body := string(r.Body)
		for _, want := range []string{`"ref":"section/api"`, `"ghost_run":"` + ghost.ID + `"`, `"state":"pending"`, `"reaped_by":"` + item.ID + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("lease-reaped body does not say %q: %s", want, body)
			}
		}
	}
	if !found {
		t.Fatal("no lease-reaped record was written for the ghost section")
	}

	// The section is free again, held under a fresh grant and released at
	// settle like any other hold.
	if _, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef("api"), Run: "probe", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("api stayed held after the reaped claim settled: %v", err)
	}
}

// TestASectionHeldByAClaimedRunIsNotReaped is issue #162's other half of
// part (2): a run that IS [queue.StateClaimed] is genuinely in flight, and
// nothing here may touch its lease. The claiming unit reports busy exactly
// as it did before this fix, and the held section survives untouched.
func TestASectionHeldByAClaimedRunIsNotReaped(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	holder := e.submit(t, "genuinely-running", nil)
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}
	holder, holderLease, err := q.Claim(ctx, holder)
	if err != nil {
		t.Fatal(err)
	}
	if holder.State != queue.StateClaimed {
		t.Fatalf("setup: want the holder claimed, got %s", holder.State)
	}
	hold(t, e, "api", holder.ID)

	item := e.submit(t, "waits-on-the-real-claim", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	item, err = e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("a section held by a claimed run should wait, got %s: %s", item.State, item.Note)
	}
	for _, want := range []string{"section api", "held by " + holder.ID} {
		if !strings.Contains(item.Note, want) {
			t.Errorf("the wait does not say %q: %s", want, item.Note)
		}
	}
	if strings.Contains(item.Note, "not running") {
		t.Errorf("a genuinely claimed holder must not be reported as not running: %s", item.Note)
	}
	if _, ok, err := e.store.Leaser("runner-1").Inspect(ctx, tn, e.repo.Name, SectionRef("api")); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("the section lease of a genuinely claimed run must survive untouched")
	}
	e.store.Leaser("runner-1").Release(ctx, holderLease)
}

// TestASelfGhostSectionIsReapedWithAFreshToken is issue #162's part (2),
// self case: a leaked earlier attempt of the SAME item — the retry never
// inherits a lease from the attempt that parked — is reaped exactly like any
// other ghost, not preempted in place, so the section it takes over carries
// a strictly newer fencing token than the leaked grant it replaced.
func TestASelfGhostSectionIsReapedWithAFreshToken(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	item := e.submit(t, "self-ghost", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	leaked := hold(t, e, "api", item.ID) // this run's own grant, surviving a lost release

	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("a self-held section should be reaped and the run completed, got %s: %s", item.State, item.Note)
	}

	probe, err := e.store.Leaser("runner-1").Acquire(ctx, forge.LeaseRequest{
		Tenant: tn, Repo: e.repo.Name, Ref: SectionRef("api"), Run: "probe", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("api stayed held after the self-reap settled: %v", err)
	}
	if probe.Token <= leaked.Token+1 {
		// The reap's own Acquire bumps the token once (leaked+1); this probe's
		// Acquire, after settle released that grant, bumps it once more.
		t.Fatalf("want the section reissued past the leaked token, leaked=%d probe=%d", leaked.Token, probe.Token)
	}
}

// TestAWaitingLandReadyReservationIsNotAGhost is issue #233's part (b): a
// Waiting land-ready holder still naming the contested section is
// [Loop.acquireSections]'s partial grant, kept on purpose across the wait —
// not a ghost, however identical the shape looks to reapGhost from the
// outside. A contender waits behind it exactly as it would behind a
// genuinely claimed run, and the section survives untouched.
func TestAWaitingLandReadyReservationIsNotAGhost(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}

	owner := e.submit(t, "reservation-owner", func(w *queue.WorkItem) { w.Sections = []string{"api", "web"} })
	claimed, lease, err := q.Claim(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	claimed.KeepCandidate("deadbeefdeadbeef", "base", nil)
	owner, err = q.Wait(ctx, claimed, lease, "web is busy; api kept")
	if err != nil {
		t.Fatal(err)
	}
	hold(t, e, "api", owner.ID) // the partial grant acquireSections would have left behind

	contender := e.submit(t, "waits-on-the-reservation", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	contender, err = e.loop.Run(ctx, contender)
	if err != nil {
		t.Fatal(err)
	}
	if contender.State != queue.StateWaiting {
		t.Fatalf("a contender should wait behind a live reservation, got %s: %s", contender.State, contender.Note)
	}
	if !strings.Contains(contender.Note, "held by "+owner.ID) {
		t.Fatalf("the wait should name the reservation's owner: %s", contender.Note)
	}
	if strings.Contains(contender.Note, "not running") {
		t.Fatalf("a live reservation must not be described as a ghost: %s", contender.Note)
	}
	if _, ok, err := e.store.Leaser("runner-1").Inspect(ctx, tn, e.repo.Name, SectionRef("api")); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("a live reservation must survive untouched, not be reaped out from under its Waiting owner")
	}
}

// TestAParkedOwnersReservationStillReaps is issue #233's part (b), the other
// side: the exemption above is narrow. Parked, an owner is never coming back
// to collect its own lease, so its held section reaps exactly as any other
// ghost's does — the convergent case the issue's design calls out: even a
// genuine leak from a land-ready waiter is bounded by the next contender's
// reap, or by the lease TTL, never left held forever.
func TestAParkedOwnersReservationStillReaps(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	q := &queue.Queue{Store: e.store, Runner: "runner-1", TTL: time.Hour}

	owner := e.submit(t, "parked-owner", func(w *queue.WorkItem) { w.Sections = []string{"api", "web"} })
	claimed, lease, err := q.Claim(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	claimed.KeepCandidate("deadbeefdeadbeef", "base", nil)
	owner, err = q.Park(ctx, claimed, lease, queue.SeverityMedium, "gate could not answer")
	if err != nil {
		t.Fatal(err)
	}
	hold(t, e, "api", owner.ID)

	contender := e.submit(t, "reaps-a-parked-reservation", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	contender, err = e.loop.Run(ctx, contender)
	if err != nil {
		t.Fatal(err)
	}
	if contender.State != queue.StateDone {
		t.Fatalf("a parked owner's reservation should reap like any other ghost, got %s: %s", contender.State, contender.Note)
	}
}

// TestAnUnclassifiableHolderIsNeverReaped is issue #162's "unknown never
// reads as zero" rule: a section held by a run this store has never heard
// of — queue.Load fails rather than answering with a state — must be left
// exactly as held. A reap must never happen on a lease nobody proved
// abandoned.
func TestAnUnclassifiableHolderIsNeverReaped(t *testing.T) {
	e := sectioned(t)
	ctx := context.Background()
	hold(t, e, "api", "wi-nonexistent")

	item := e.submit(t, "cannot-classify", func(w *queue.WorkItem) { w.Sections = []string{"api"} })
	item, err := e.loop.Run(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != queue.StateWaiting {
		t.Fatalf("an unclassifiable holder must not be reaped, got %s: %s", item.State, item.Note)
	}
	if !strings.Contains(item.Note, "held by wi-nonexistent") {
		t.Errorf("the wait does not name the unclassifiable holder: %s", item.Note)
	}
	if _, ok, err := e.store.Leaser("runner-1").Inspect(ctx, tn, e.repo.Name, SectionRef("api")); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("an unclassifiable holder's lease must survive untouched")
	}
}
