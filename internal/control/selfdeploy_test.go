package control

import (
	"context"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestSelfDeployRollbackAppearsAsAnException: internal/daemon's rollback
// resolver writes exactly two records — the standing control/self-deploy
// chain, and an index/exceptions record naming the same head — with no web
// change needed to show it. This is the room's half: the fold has to
// recognize the shape and render it like any other exception.
func TestSelfDeployRollbackAppearsAsAnException(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	head := "deadbeefcafefeed"

	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployRolledBackKind,
		Subject: head, Actor: "boxd", Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeploySubjectPrefix + head, Actor: "boxd",
		Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("exceptions: %+v", got)
	}
	ex := got[0]
	if want := selfDeploySubjectPrefix + shortCommit(head); ex.ID != want {
		t.Fatalf("id = %q, want %q", ex.ID, want)
	}
	if ex.Severity != queue.SeverityHigh {
		t.Fatalf("severity = %q, want high", ex.Severity)
	}
	if ex.Note != "self-deploy: box shop good -> deadbeefcafefeed" {
		t.Fatalf("note = %q, want the marker's reason", ex.Note)
	}
	if ex.State != queue.StateParked {
		t.Fatalf("state = %q, want parked — the room's existing shape", ex.State)
	}
}

// TestSelfDeployExceptionDropsOutAfterALaterSuccessfulDeploy: the fold reads
// the standing chain for the head, not the index record's own body, so a
// later record for the same subject settles it — same as a work item that
// moved past parked.
func TestSelfDeployExceptionDropsOutAfterALaterSuccessfulDeploy(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	head := "deadbeefcafefeed"

	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: selfDeployRolledBackKind,
		Subject: head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeploySubjectPrefix + head, Actor: "boxd", Body: []byte("did not come up"),
	}); err != nil {
		t.Fatal(err)
	}
	// A later record for the same head — e.g. an operator forced a retry
	// that this time came up.
	if _, err := r.Store.Log().Append(ctx, evidence.Record{
		Tenant: tn, Scope: selfDeployScope, Kind: "self-deployed",
		Subject: head, Actor: "boxd", Body: []byte("self-deploy: box shop good -> deadbeefcafefeed"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := r.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("exceptions: %+v, want none — the rollback was superseded", got)
	}
}
