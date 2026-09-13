package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
)

// ghIssueCloseServer answers the three calls resolveEscalatedIssue's closer
// makes — GET the issue, GET its comments, POST a comment, PATCH it closed
// — recording each so a test can assert on what it saw. state seeds the
// issue's own state (open, by default, unless a test says otherwise).
type ghIssueCloseServer struct {
	mu       sync.Mutex
	state    string
	comments []string
	patched  bool
}

func newGHIssueCloseServer(t *testing.T, state string) (*httptest.Server, *ghIssueCloseServer) {
	t.Helper()
	if state == "" {
		state = "open"
	}
	g := &ghIssueCloseServer{state: state}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42":
			json.NewEncoder(w).Encode(struct {
				State string `json:"state"`
			}{State: g.state})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/42/comments":
			out := make([]struct {
				Body string `json:"body"`
			}, len(g.comments))
			for i, c := range g.comments {
				out[i].Body = c
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/issues/42/comments":
			var in struct {
				Body string `json:"body"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			g.comments = append(g.comments, in.Body)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/app/issues/42":
			g.patched = true
			g.state = "closed"
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, g
}

// newResolveDaemon builds a Daemon with one repo (shop/app) configured for
// pull_request landing against a local bare remote — so New's own internal
// assemble stays offline, exactly as newGHLandDaemon's doc comment explains
// — then points its escalationCloser at ts and its remotes/landPR entries at
// a github.com remote string, the same swap newGHLandDaemon does for
// landProbe. The remote is never fetched here: resolveEscalatedIssue only
// parses it for owner/repo.
func newResolveDaemon(t *testing.T, ts *httptest.Server) *Daemon {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "pull_request", "gh")

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	d.escalationCloser.apiBase = ts.URL
	d.escalationCloser.httpClient = ts.Client()
	d.escalationCloser.token = func(context.Context) (string, error) { return "tok", nil }
	d.remotes["shop-app"] = "https://github.com/acme/app.git"
	return d
}

// openAndFail opens an origination on shop-app's route with key and fails
// its planning — the standing a person's resolve reaches from.
func openAndFail(t *testing.T, d *Daemon, key, reason string) ingress.Ingress {
	t.Helper()
	ctx := context.Background()
	route := d.routes["shop-app"]
	ing, _, err := d.router.Open(ctx, route, ingress.Delivery{Key: key})
	if err != nil {
		t.Fatalf("open %s: %v", key, err)
	}
	if err := d.plans.Fail(ctx, ing, reason); err != nil {
		t.Fatalf("fail %s: %v", key, err)
	}
	return ing
}

// postResolve drives POST /v1/items/{id}/resolve over the daemon's own API
// handler, the same surface a person's `box resolve` reaches — over a real
// unix socket, since a resolve is a mutating call and only a socket peer can
// satisfy it (see internal/api's mustActor).
func postResolve(t *testing.T, d *Daemon, id, note string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := api.Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: d.API.Handler(), ConnContext: api.ConnContext}
	go hs.Serve(ln)
	defer hs.Close()
	c, err := api.Dial("unix:"+sock, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reopen(context.Background(), "resolve", id, note); err != nil {
		t.Fatalf("POST resolve: %v", err)
	}
}

// TestResolveClosesTheDrivingIssue is issue #108 part 2's core assertion: a
// person resolving an escalated, issue-keyed origination on a box that lands
// by pull request closes that issue on GitHub — one comment naming the
// plan-failed reason and the resolver's note, then a close — and records
// issue-closed on the INGRESS chain.
func TestResolveClosesTheDrivingIssue(t *testing.T) {
	ts, g := newGHIssueCloseServer(t, "open")
	d := newResolveDaemon(t, ts)
	ing := openAndFail(t, d, "issue-42", "planner says this is already satisfied")

	postResolve(t, d, ing.ID, "confirmed by hand, closing it")

	g.mu.Lock()
	patched, comments := g.patched, append([]string(nil), g.comments...)
	g.mu.Unlock()
	if !patched {
		t.Fatal("issue was never PATCHed closed")
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %v, want exactly one", comments)
	}
	if !strings.Contains(comments[0], "already satisfied") || !strings.Contains(comments[0], "confirmed by hand") {
		t.Fatalf("comment %q carries neither the plan-failed reason nor the resolver's note", comments[0])
	}

	found := false
	ctx := context.Background()
	for r, err := range d.store.Log().Since(ctx, d.tenant, ingress.Scope(ing.ID), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "issue-closed" {
			if !strings.Contains(string(r.Body), "42") {
				t.Fatalf("issue-closed body %s does not name the issue", r.Body)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no issue-closed record on the ingress chain after a confirmed close")
	}
}

// TestResolveAlreadyClosedIssuePostsCommentOnlyOnce confirms idempotence: an
// issue GitHub already shows closed is left as is — no PATCH — and the
// comment still posts (once).
func TestResolveAlreadyClosedIssuePostsCommentOnlyOnce(t *testing.T) {
	ts, g := newGHIssueCloseServer(t, "closed")
	d := newResolveDaemon(t, ts)
	ing := openAndFail(t, d, "issue-42", "planner says this is already satisfied")

	postResolve(t, d, ing.ID, "already closed manually")

	g.mu.Lock()
	patched, comments := g.patched, len(g.comments)
	g.mu.Unlock()
	if patched {
		t.Fatal("PATCHed an issue that was already closed")
	}
	if comments != 1 {
		t.Fatalf("comments = %d, want exactly one", comments)
	}
}

// TestResolveNonIssueKeyClosesNothing is issue #108 part 2's negative case:
// an origination whose delivery key does not follow the issue-<n>
// convention closes nothing — no request reaches GitHub at all.
func TestResolveNonIssueKeyClosesNothing(t *testing.T) {
	ts, g := newGHIssueCloseServer(t, "open")
	d := newResolveDaemon(t, ts)
	ing := openAndFail(t, d, "manual-1", "planner says this is already satisfied")

	postResolve(t, d, ing.ID, "not an issue")

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.patched || len(g.comments) != 0 {
		t.Fatalf("closer touched GitHub for a non-issue-keyed origination: patched=%v comments=%v", g.patched, g.comments)
	}
}

// TestResolveNilHookChangesNothing confirms the seam is truly optional: a
// Server with EscalationResolved left nil (today's behavior, and every
// existing api test's) resolves the escalation exactly as before — the
// queue/plan state changes, and nothing else does.
func TestResolveNilHookChangesNothing(t *testing.T) {
	ts, g := newGHIssueCloseServer(t, "open")
	d := newResolveDaemon(t, ts)
	d.API.EscalationResolved = nil
	ing := openAndFail(t, d, "issue-42", "planner says this is already satisfied")

	postResolve(t, d, ing.ID, "resolved with the hook off")

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.patched || len(g.comments) != 0 {
		t.Fatalf("closer touched GitHub with EscalationResolved nil: patched=%v comments=%v", g.patched, g.comments)
	}
}
