package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// appendFlake writes one "flaky" record on index/flakes exactly as
// [Loop.proveFlake] does, so these tests exercise the room against the
// landed shape rather than a guess at it.
func appendFlake(t *testing.T, r *Room, subject string, body flakeBody) evidence.Record {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := r.Store.Log().Append(context.Background(), evidence.Record{
		Tenant: tn, Scope: "index/flakes", Kind: "flaky",
		Subject: subject, Actor: "runner-1", Body: b,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// TestFlakesGroupsBySubjectNewestFirst: the same test proven flaky more
// than once is one row, not one per proof, and a different test's more
// recent proof sorts ahead of an older one for a test proven flaky first.
func TestFlakesGroupsBySubjectNewestFirst(t *testing.T) {
	r, _ := setup(t)

	subjectA := "acme/repo/pkg/TestFlipFlop"
	subjectB := "acme/repo/pkg/TestOther"

	// subjectA proves flaky twice first, so it is also the first subject
	// this fold ever sees. subjectB's single proof lands last and must
	// still sort ahead of it — the only way that happens is the fold
	// actually sorting on LastSeen rather than falling out of insertion
	// order, which a fixture where the newest subject also inserted first
	// could never tell apart from a no-op sort.
	appendFlake(t, r, subjectA, flakeBody{
		Test: "TestFlipFlop", Package: "pkg", Repo: "acme/repo",
		RefusedHead: "aaa111", PassedHead: "aaa222",
		RefusedRecord: "r1", PassedRecord: "r2", WorkItem: "wi-1",
	})
	appendFlake(t, r, subjectA, flakeBody{
		Test: "TestFlipFlop", Package: "pkg", Repo: "acme/repo",
		RefusedHead: "aaa333", PassedHead: "aaa444",
		RefusedRecord: "r5", PassedRecord: "r6", WorkItem: "wi-3",
	})
	appendFlake(t, r, subjectB, flakeBody{
		Test: "TestOther", Package: "pkg", Repo: "acme/repo",
		RefusedHead: "bbb111", PassedHead: "bbb222",
		RefusedRecord: "r3", PassedRecord: "r4", WorkItem: "wi-2",
	})

	got, err := r.Flakes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("flakes: %+v, want 2 grouped rows", got)
	}
	// subjectB's proof is the newest record overall, so it leads despite
	// subjectA having appeared first.
	if got[0].Subject != subjectB {
		t.Fatalf("first row = %q, want the newest occurrence %q", got[0].Subject, subjectB)
	}
	if got[0].Count != 1 {
		t.Fatalf("count for %q = %d, want 1", subjectB, got[0].Count)
	}
	if got[1].Subject != subjectA {
		t.Fatalf("second row = %+v, want %q", got[1], subjectA)
	}
	if got[1].Count != 2 {
		t.Fatalf("count for %q = %d, want 2", subjectA, got[1].Count)
	}
	if got[1].RefusedHead != "aaa333" || got[1].PassedHead != "aaa444" {
		t.Fatalf("row for %q did not carry the LATEST proof's detail: %+v", subjectA, got[1])
	}
	if got[1].WorkItem != "wi-3" {
		t.Fatalf("work item = %q, want the latest proof's wi-3", got[1].WorkItem)
	}
}

// TestFlakesCountIsTheListingCounted pins issue #131's own rule for this
// surface: any count this package exposes must come from the same fold
// that produced the listing, never a second walk of the chain (precedent:
// TestTheCountIsTheListingCounted in backlog_test.go).
func TestFlakesCountIsTheListingCounted(t *testing.T) {
	r, _ := setup(t)
	subject := "acme/repo/pkg/TestFlipFlop"
	body := flakeBody{Test: "TestFlipFlop", Package: "pkg", Repo: "acme/repo"}
	appendFlake(t, r, subject, body)
	appendFlake(t, r, subject, body)
	appendFlake(t, r, subject, body)

	got, err := r.Flakes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("flakes: %+v", got)
	}
	// The row's count is exactly the number of records this test appended
	// for the subject — there is no other source it could have read.
	if got[0].Count != 3 {
		t.Fatalf("count = %d, want 3 — the same three records the listing folded", got[0].Count)
	}
}

// TestFlakeExceptionAppearsAfterTwoProofs: [Loop.foldFlakeException] writes
// the index/exceptions marker at the SECOND flaky record for a subject.
// This is control's half — the fold must recognize the "flake/" subject
// shape and render it in the same JSON shape the other branches use, with
// zero web change.
func TestFlakeExceptionAppearsAfterTwoProofs(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	test := "acme/repo/pkg/TestFlipFlop"
	body := flakeBody{
		Test: "TestFlipFlop", Package: "pkg", Repo: "acme/repo",
		RefusedHead: "aaa111", PassedHead: "aaa222",
		RefusedRecord: "r1", PassedRecord: "r2", WorkItem: "wi-1",
	}
	appendFlake(t, r, test, body)
	appendFlake(t, r, test, body)

	// The write [Loop.foldFlakeException] performs once the count crosses
	// the line.
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: flakeSubjectPrefix + test, Actor: "runner-1",
		Body: []byte(test + " flaked twice — needs fixing, not re-rolling"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v, want the flake exception", got)
	}
	ex := got[0]
	if ex.ID != flakeSubjectPrefix+test {
		t.Fatalf("id = %q, want the full flake/ subject so escalation keys agree", ex.ID)
	}
	if ex.Severity != queue.SeverityMedium {
		t.Fatalf("severity = %q, want medium", ex.Severity)
	}
	if ex.State != queue.StateParked {
		t.Fatalf("state = %q, want parked", ex.State)
	}
	if !strings.Contains(ex.Note, test) || !strings.Contains(ex.Note, "2") {
		t.Fatalf("note = %q, want it to name the test and its count", ex.Note)
	}
}

// TestFlakeExceptionCountKeepsClimbing: the exception's note reads the
// live index/flakes count rather than trusting the index record's own
// body, so a third and fourth proof move the count even though
// [Loop.foldFlakeException] only ever writes the marker once.
func TestFlakeExceptionCountKeepsClimbing(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	test := "acme/repo/pkg/TestFlipFlop"
	body := flakeBody{Test: "TestFlipFlop", Package: "pkg", Repo: "acme/repo"}
	appendFlake(t, r, test, body)
	appendFlake(t, r, test, body)
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: flakeSubjectPrefix + test, Actor: "runner-1",
		Body: []byte("marker"),
	}); err != nil {
		t.Fatal(err)
	}
	appendFlake(t, r, test, body)

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v", got)
	}
	if !strings.Contains(got[0].Note, "3") {
		t.Fatalf("note = %q, want it to reflect the third proof", got[0].Note)
	}
}
