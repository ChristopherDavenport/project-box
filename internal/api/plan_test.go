package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestRetryReachesAFailedPlan: the one exception that exists before any item
// does takes the same verbs, over the same route, and answers in the item's
// shape.
func TestRetryReachesAFailedPlan(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	w.srv.Plans = &plan.Store{Log: w.srv.Store.Log(), Actor: "d"}
	ing, _, err := w.srv.Router.Open(ctx, w.srv.Routes["issues"], ingress.Delivery{Key: "issue-plan", Payload: []byte("plan me")})
	if err != nil {
		t.Fatal(err)
	}
	_ = kind.SeedIssue
	if err := w.srv.Plans.Fail(ctx, ing, "session limit"); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	exs, err := c.Exceptions(ctx)
	if err != nil || len(exs) != 1 || exs[0].ID != ing.ID {
		t.Fatalf("the failed plan is not on the wire as an exception: %+v %v", exs, err)
	}
	item, err := c.Reopen(ctx, "retry", ing.ID, "budget is back")
	if err != nil || item.ID != ing.ID || item.State != queue.StatePending {
		t.Fatalf("retry over the wire: %+v %v", item, err)
	}
	st, err := w.srv.Plans.Standing(ctx, tn, ing.ID)
	if err != nil || st.State != "retried" {
		t.Fatalf("standing after retry: %+v %v", st, err)
	}
	if exs, _ := c.Exceptions(ctx); len(exs) != 0 {
		t.Fatalf("still an exception after retry: %+v", exs)
	}
}
