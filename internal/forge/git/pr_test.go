package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// githubRemote points a repo at a URL shaped like a real GitHub remote, so
// PRLander's owner/repo parsing accepts it, while every git operation
// against that URL is redirected — via git's own url.insteadOf, applied
// through GIT_CONFIG_COUNT/KEY/VALUE so the redirect is live even for the
// very first clone — back to the fixture's real local bare repo. No network
// call is ever made: this is the standard git mechanism for standing a local
// repo in for a remote one, not a new test dependency.
func githubRemote(t *testing.T, real string) string {
	t.Helper()
	const fake = "https://github.com/acme/app.git"
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+real+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", fake)
	return fake
}

// fakeGitHub stands in for the GitHub REST API's pull request endpoints.
// Pull requests are keyed by (owner, repo, head branch): GET .../pulls with
// state=open reports whatever was last created for that head, and POST
// refuses nothing but records every creation so a test can assert a retry
// reused rather than duplicated.
type fakeGitHub struct {
	*httptest.Server
	mu      sync.Mutex
	byHead  map[string]ghPR
	creates int
	next    int
}

type ghPR struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"html_url"`
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{byHead: map[string]ghPR{}, next: 100}
	g.Server = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.Server.Close)
	return g
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "repos" || parts[3] != "pulls" {
		http.NotFound(w, r)
		return
	}
	owner, repo := parts[1], parts[2]

	g.mu.Lock()
	defer g.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		head := r.URL.Query().Get("head") // "owner:branch"
		hp := strings.SplitN(head, ":", 2)
		key := owner + "/" + repo + "#" + hp[len(hp)-1]
		var out []ghPR
		if pr, ok := g.byHead[key]; ok {
			out = append(out, pr)
		}
		json.NewEncoder(w).Encode(out)
	case http.MethodPost:
		var in struct{ Title, Head, Base, Body string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := owner + "/" + repo + "#" + in.Head
		g.next++
		g.creates++
		pr := ghPR{Title: in.Title, Body: in.Body, URL: "https://github.com/" + owner + "/" + repo + "/pull/" + strconv.Itoa(g.next)}
		g.byHead[key] = pr
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(pr)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func testToken(context.Context) (string, error) { return "test-token", nil }

// prFix builds a fixture whose repo's remote is github.com-shaped (redirected
// locally) and returns everything a PRLander test needs.
type prFix struct {
	f    *fixture
	repo forge.Repo
	m    *Mirror
	gh   *fakeGitHub
	pr   *PRLander
}

func fixPR(t *testing.T) *prFix {
	t.Helper()
	f := fix(t)
	repo := f.repo
	repo.Remote = githubRemote(t, f.remote)

	m, err := NewMirror(f.root, tn, f.store.Leaser("runner-a"))
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if err := m.Fetch(context.Background(), repo); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	gh := newFakeGitHub(t)
	lander := NewLander(m, f.store.Log())
	pr := NewPRLander(lander, testToken)
	pr.apiBase = gh.URL
	pr.httpClient = gh.Client()

	return &prFix{f: f, repo: repo, m: m, gh: gh, pr: pr}
}

func TestPROpenedWithFixesLineAndEvidence(t *testing.T) {
	x := fixPR(t)
	ctx := context.Background()
	leaser := x.f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := x.m.Checkout(ctx, lease, x.f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head := buildOn(t, ws, "feature")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr", DrivingIssue: "22",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	landing, err := x.pr.Land(ctx, cs)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !landing.Complete() {
		t.Fatalf("landing incomplete: %+v", landing)
	}

	// The commit that landed on main is byte-identical to the built head —
	// wrapping changed nothing about what a plain git.Lander would have
	// pushed. No new merge commit was minted.
	got := strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/main"))
	if forge.Commit(got) != head {
		t.Fatalf("remote main = %s, want %s (the built head, unchanged by the PR wrapper)", got, head)
	}

	if landing.Landed[0].PRURL == "" {
		t.Fatal("Placement.PRURL was not populated")
	}
	if x.gh.creates != 1 {
		t.Fatalf("want exactly one PR created, got %d", x.gh.creates)
	}
	body := x.gh.byHead["acme/app#box/run-1"].Body
	if !strings.Contains(body, "Fixes #22") {
		t.Fatalf("pr body = %q, want it to contain Fixes #22", body)
	}

	// The evidence log gained a pr-opened record in the changeset scope,
	// keyed by the landed commit, carrying the PR URL.
	found := false
	for r, err := range x.f.store.Log().Since(ctx, tn, "changeset/cs-1", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		if r.Kind == "pr-opened" && r.Subject == string(head) {
			if !strings.Contains(string(r.Body), landing.Landed[0].PRURL) {
				t.Fatalf("pr-opened body %s does not carry the PR URL %s", r.Body, landing.Landed[0].PRURL)
			}
			if !strings.Contains(string(r.Body), "22") {
				t.Fatalf("pr-opened body %s does not carry the issue number", r.Body)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no pr-opened evidence keyed by the landed commit")
	}
}

func TestPROmitsFixesLineWithoutDrivingIssue(t *testing.T) {
	x := fixPR(t)
	ctx := context.Background()
	leaser := x.f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := x.m.Checkout(ctx, lease, x.f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head := buildOn(t, ws, "feature")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "routine change",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	body := x.gh.byHead["acme/app#box/run-1"].Body
	if strings.Contains(body, "Fixes #") {
		t.Fatalf("pr body = %q, want no Fixes # line when DrivingIssue is empty", body)
	}
}

// TestPRRetryReusesOpenPR simulates a retried tick: the first attempt opens
// the PR but the landing itself fails because the base branch moved
// out-of-band (an ordinary reason a tick retries), so nothing is landed yet.
// The second attempt, rebuilt against the branch's new tip, must reuse the
// PR opened on the first attempt rather than open a duplicate.
func TestPRRetryReusesOpenPR(t *testing.T) {
	x := fixPR(t)
	ctx := context.Background()
	leaser := x.f.store.Leaser("runner-a")

	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ws, err := x.m.Checkout(ctx, lease, x.f.base)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	head1 := buildOn(t, ws, "feature")

	// Sabotage: main moves out-of-band after the build, so the first land
	// attempt's fenced push will be refused.
	rogue := filepath.Join(t.TempDir(), "rogue")
	mustGit(t, "", "clone", x.f.remote, rogue)
	mustGit(t, rogue, "commit", "--allow-empty", "-m", "out of band")
	mustGit(t, rogue, "push", "origin", "HEAD:refs/heads/main")
	rogueSha := forge.Commit(strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/main")))

	cs1 := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr", DrivingIssue: "22",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head1, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs1); err == nil {
		t.Fatal("first land should have been refused: base moved out from under it")
	}
	if x.gh.creates != 1 {
		t.Fatalf("want exactly one PR created after the first (failed) attempt, got %d", x.gh.creates)
	}

	// Retry: renew the lease, rebuild on the branch's new tip, land again.
	// The mirror must see the out-of-band commit before it can check it out.
	if err := x.m.Fetch(ctx, x.repo); err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	lease2, err := leaser.Renew(ctx, lease, time.Hour)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	ws2, err := x.m.Checkout(ctx, lease2, rogueSha)
	if err != nil {
		t.Fatalf("checkout retry: %v", err)
	}
	head2 := buildOn(t, ws2, "feature")

	cs2 := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr", DrivingIssue: "22",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: rogueSha, Head: head2, Lease: lease2}},
	}
	landing, err := x.pr.Land(ctx, cs2)
	if err != nil {
		t.Fatalf("retried land: %v", err)
	}
	if !landing.Complete() {
		t.Fatalf("retried landing incomplete: %+v", landing)
	}
	if x.gh.creates != 1 {
		t.Fatalf("retry must reuse the open PR, not open a duplicate: want 1 create, got %d", x.gh.creates)
	}
	if landing.Landed[0].PRURL == "" {
		t.Fatal("Placement.PRURL was not populated on the retried landing")
	}
	got := strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/main"))
	if forge.Commit(got) != head2 {
		t.Fatalf("remote main = %s, want %s", got, head2)
	}
}
