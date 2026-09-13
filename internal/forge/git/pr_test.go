package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
	return githubRemoteNamed(t, real, "acme/app")
}

// githubRemoteNamed is [githubRemote] generalized to redirect more than one
// owner/repo, for tests that need a second, distinct backing remote: it
// appends to whatever GIT_CONFIG_COUNT this test has already set rather than
// resetting it, so multiple calls within one test compose.
func githubRemoteNamed(t *testing.T, real, ownerRepo string) string {
	t.Helper()
	fake := "https://github.com/" + ownerRepo + ".git"
	idx := 0
	if n := os.Getenv("GIT_CONFIG_COUNT"); n != "" {
		idx, _ = strconv.Atoi(n)
	}
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(idx+1))
	t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", idx), "url."+real+".insteadOf")
	t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", idx), fake)
	return fake
}

// fakeGitHub stands in for the GitHub REST API's pull request endpoints.
// Pull requests are keyed by (owner, repo, head branch): GET .../pulls with
// state=open reports whatever was last created for that head, and POST
// refuses nothing but records every creation so a test can assert a retry
// reused rather than duplicated.
type fakeGitHub struct {
	*httptest.Server
	mu       sync.Mutex
	byHead   map[string]ghPR
	creates  int
	next     int
	checks   []ghCheckRun
	statuses []ghStatus
	// appOnly makes /check-runs answer as api.github.com does to an OAuth
	// token: 403, "You must authenticate via a GitHub App."
	appOnly         bool
	reviewsByNumber map[int][]ghReview
	// scopesHeader, when non-nil, is the value /rate_limit answers with on
	// its X-OAuth-Scopes response header. A nil pointer sends no such
	// header at all — how a fine-grained PAT or a GitHub App installation
	// token actually authenticates, never mentioning scopes either way.
	scopesHeader *string
	// requests logs every request's "METHOD /path" this server has seen, so
	// a test can assert the scope probe was never sent for a change that
	// never touched .github/workflows/.
	requests []string
	// issues backs the issues API's get/comment/close endpoints, keyed
	// "owner/repo#number". Missing means an issue nobody has touched yet:
	// open, no comments.
	issues map[string]*ghIssueState
	// issueGet500 makes GET .../issues/{n} answer 500, for the "an issues
	// API failure must not fail the landing" test.
	issueGet500 bool
	// postPRStatus, when nonzero, makes POST .../pulls answer this status
	// instead of creating anything — a request GitHub never acted on, for
	// tests of a validation failure (422) or a plain server error (500)
	// that leaves no PR behind.
	postPRStatus int
	// postPRLoseAckOnce makes the FIRST POST .../pulls create and record the
	// PR exactly as it would in production, but answer 500 instead of 201 —
	// the ack lost, not the write. It resets itself after firing once, so a
	// retried create behaves normally.
	postPRLoseAckOnce bool
}

// ghIssueState is one issue's mutable state, as [PRLander.closeDrivingIssue]
// would observe and change it on the real GitHub REST API.
type ghIssueState struct {
	State    string
	Comments []string
	Patches  int
}

// issue returns key's state, creating it open with no comments the first
// time it is asked about — callers already hold g.mu.
func (g *fakeGitHub) issue(key string) *ghIssueState {
	if g.issues == nil {
		g.issues = map[string]*ghIssueState{}
	}
	st, ok := g.issues[key]
	if !ok {
		st = &ghIssueState{State: "open"}
		g.issues[key] = st
	}
	return st
}

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"html_url"`
}

// ghReview is one review [fakeGitHub] reports back from the pulls/{n}/reviews
// endpoint — enough of GitHub's own review shape for PRLander.Reviews to
// decode: who reviewed, what they reviewed (commit_id), and what they said
// (state).
type ghReview struct {
	ID   int64 `json:"id"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	CommitID    string    `json:"commit_id"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// setReviews seeds the reviews a test wants pulls/{number}/reviews to
// report, replacing whatever was set before for that number.
func (g *fakeGitHub) setReviews(number int, reviews []ghReview) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reviewsByNumber == nil {
		g.reviewsByNumber = map[int][]ghReview{}
	}
	g.reviewsByNumber[number] = reviews
}

// ghCheckRun is what a test reads back after PRLander posts to
// /check-runs: enough of the GitHub Checks API's create-check-run body to
// assert on name, sha, conclusion, and the output an auditor would read.
type ghCheckRun struct {
	Owner      string `json:"-"`
	Repo       string `json:"-"`
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	} `json:"output"`
}

// ghStatus is what a test reads back after PRLander posts to
// /statuses/{sha}: the commit status API's create body plus the sha it was
// posted on.
type ghStatus struct {
	SHA         string `json:"-"`
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description"`
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{byHead: map[string]ghPR{}, next: 100}
	g.Server = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.Server.Close)
	return g
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r.Method+" "+r.URL.Path)
	g.mu.Unlock()

	if r.URL.Path == "/rate_limit" && r.Method == http.MethodGet {
		g.mu.Lock()
		header := g.scopesHeader
		g.mu.Unlock()
		if header != nil {
			w.Header().Set("X-OAuth-Scopes", *header)
		}
		json.NewEncoder(w).Encode(map[string]string{})
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "repos" {
		http.NotFound(w, r)
		return
	}
	owner, repo := parts[1], parts[2]

	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case len(parts) == 4 && parts[3] == "pulls" && r.Method == http.MethodGet:
		head := r.URL.Query().Get("head") // "owner:branch"
		hp := strings.SplitN(head, ":", 2)
		key := owner + "/" + repo + "#" + hp[len(hp)-1]
		var out []ghPR
		if pr, ok := g.byHead[key]; ok {
			out = append(out, pr)
		}
		json.NewEncoder(w).Encode(out)
	case len(parts) == 4 && parts[3] == "pulls" && r.Method == http.MethodPost:
		if g.postPRStatus != 0 {
			http.Error(w, "boom", g.postPRStatus)
			return
		}
		var in struct{ Title, Head, Base, Body string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := owner + "/" + repo + "#" + in.Head
		g.next++
		g.creates++
		pr := ghPR{Number: g.next, Title: in.Title, Body: in.Body, URL: "https://github.com/" + owner + "/" + repo + "/pull/" + strconv.Itoa(g.next)}
		g.byHead[key] = pr
		if g.postPRLoseAckOnce {
			g.postPRLoseAckOnce = false
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(pr)
	case len(parts) == 4 && parts[3] == "check-runs" && r.Method == http.MethodPost:
		var in ghCheckRun
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.Owner, in.Repo = owner, repo
		g.checks = append(g.checks, in)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(in)
	case len(parts) == 5 && parts[3] == "statuses" && r.Method == http.MethodPost:
		var in ghStatus
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.SHA = parts[4]
		g.statuses = append(g.statuses, in)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(in)
	case len(parts) == 4 && parts[3] == "check-runs" && r.Method == http.MethodPost && g.appOnly:
		// What api.github.com says to an OAuth token.
		http.Error(w, `{"message":"You must authenticate via a GitHub App."}`, http.StatusForbidden)
	case len(parts) == 6 && parts[3] == "pulls" && parts[5] == "reviews" && r.Method == http.MethodGet:
		number, _ := strconv.Atoi(parts[4])
		out := g.reviewsByNumber[number]
		if out == nil {
			out = []ghReview{}
		}
		json.NewEncoder(w).Encode(out)
	case len(parts) == 5 && parts[3] == "issues" && r.Method == http.MethodGet:
		if g.issueGet500 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		st := g.issue(owner + "/" + repo + "#" + parts[4])
		json.NewEncoder(w).Encode(map[string]string{"state": st.State})
	case len(parts) == 5 && parts[3] == "issues" && r.Method == http.MethodPatch:
		var in struct {
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st := g.issue(owner + "/" + repo + "#" + parts[4])
		st.State = in.State
		st.Patches++
		json.NewEncoder(w).Encode(map[string]string{"state": st.State})
	case len(parts) == 6 && parts[3] == "issues" && parts[5] == "comments" && r.Method == http.MethodGet:
		st := g.issue(owner + "/" + repo + "#" + parts[4])
		out := make([]map[string]string, 0, len(st.Comments))
		for _, c := range st.Comments {
			out = append(out, map[string]string{"body": c})
		}
		json.NewEncoder(w).Encode(out)
	case len(parts) == 6 && parts[3] == "issues" && parts[5] == "comments" && r.Method == http.MethodPost:
		var in struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st := g.issue(owner + "/" + repo + "#" + parts[4])
		st.Comments = append(st.Comments, in.Body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{})
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

// TestPROpenedNotesOriginKeyWithoutDrivingIssue is issue #142: a landing
// whose DrivingIssue is empty used to write its pr-opened record silently
// (`{"pr_url":...,"issue":""}`), so the gap was only visible by comparing it
// against a record that did carry an issue. The record must now say why.
func TestPROpenedNotesOriginKeyWithoutDrivingIssue(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "campaign sweep", OriginKey: "campaign-7",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	found := false
	for r, err := range x.f.store.Log().Since(ctx, tn, "changeset/cs-1", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		if r.Kind == "pr-opened" && r.Subject == string(head) {
			if !strings.Contains(string(r.Body), `"note":"no driving issue: origin key \"campaign-7\""`) {
				t.Fatalf("pr-opened body %s does not name the origin key", r.Body)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no pr-opened evidence keyed by the landed commit")
	}
}

// TestPROpenedCarriesNoNoteWithDrivingIssue guards the byte-compatible shape:
// a landing with a DrivingIssue must not gain the note field — existing
// readers unmarshal a subset of this hand-formatted body.
func TestPROpenedCarriesNoNoteWithDrivingIssue(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		DrivingIssue: "22", OriginKey: "issue-22",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	found := false
	for r, err := range x.f.store.Log().Since(ctx, tn, "changeset/cs-1", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		if r.Kind == "pr-opened" && r.Subject == string(head) {
			if strings.Contains(string(r.Body), "note") {
				t.Fatalf("pr-opened body %s carries a note despite a driving issue", r.Body)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no pr-opened evidence keyed by the landed commit")
	}
}

// TestPRTitledFromMemberSummary is issue #59: PRLander used to title every
// PR with the ChangeSet's whole Intent, which is the planner's unbounded
// task paragraph and is what produced GitHub's 422 "title is too long". When
// the first member carries a Summary, the PR must be titled from that, never
// from Intent.
func TestPRTitledFromMemberSummary(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1",
		Intent:  "a much longer task paragraph the planner wrote for the builder, never meant to title a PR",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease, Summary: "fix the thing"}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	title := x.gh.byHead["acme/app#box/run-1"].Title
	if title != "fix the thing" {
		t.Fatalf("title = %q, want %q", title, "fix the thing")
	}
	if strings.Contains(title, cs.Intent) {
		t.Fatalf("title = %q, must not contain the task text", title)
	}
}

// TestPRTitledFromCommitSubjectWhenSummaryEmpty covers the fallback: a
// member with no Summary (e.g. a work item predating issue #51's field, or
// one where Summary was never populated) is titled from the landed commit's
// own subject line instead — still never from Intent.
func TestPRTitledFromCommitSubjectWhenSummaryEmpty(t *testing.T) {
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
	head := buildOn(t, ws, "some commit subject")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1",
		Intent:  "the planner's whole task paragraph, still never a title",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	title := x.gh.byHead["acme/app#box/run-1"].Title
	if title != "some commit subject" {
		t.Fatalf("title = %q, want %q", title, "some commit subject")
	}
	if strings.Contains(title, cs.Intent) {
		t.Fatalf("title = %q, must not contain the task text", title)
	}
}

// TestPRTitleTruncatedAtWordBoundary is the 422 issue #59 fixes directly: a
// Summary over GitHub's 256-character title limit must be cut to fit, at a
// word boundary rather than mid-word, and Intent (the untruncated task text)
// must never appear in the title.
func TestPRTitleTruncatedAtWordBoundary(t *testing.T) {
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

	// 28 ten-char words joined by single spaces: 307 characters, well past
	// 256, with a space every 11 characters so the exact cut point is easy
	// to hand-verify. Char 256 lands inside word 23 (0-indexed), so the cut
	// must back up to the space ending word 22, at byte 252.
	word := "0123456789"
	words := make([]string, 28)
	for i := range words {
		words[i] = word
	}
	summary := strings.Join(words, " ")
	if len(summary) <= 256 {
		t.Fatalf("test setup: summary too short: %d bytes", len(summary))
	}
	want := summary[:252]

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1",
		Intent:  "the planner's whole task paragraph, never a title, regardless of Summary's length",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease, Summary: summary}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	title := x.gh.byHead["acme/app#box/run-1"].Title
	if title != want {
		t.Fatalf("title = %q (%d bytes), want %q (%d bytes)", title, len(title), want, len(want))
	}
	if len(title) > 256 {
		t.Fatalf("title exceeds GitHub's 256-character limit: %d bytes", len(title))
	}
	if summary[len(title)] != ' ' {
		t.Fatalf("title %q does not end at a word boundary: next original character is %q", title, string(summary[len(title)]))
	}
	if strings.Contains(title, cs.Intent) {
		t.Fatalf("title = %q, must not contain the task text", title)
	}
}

// TestPRTwoMembersTitledFromFirstSummaryBodyMentionsSecond is the multi-repo
// case: production loop.go never builds a ChangeSet with more than one
// member (confirmed by grep — this repo runs one repo per Loop), so this
// ChangeSet is built by hand, the same way changeset() in git_test.go builds
// a single-member one. Every PR a multi-member ChangeSet opens is titled
// from the first member's Summary, and the body says what else the
// changeset touches.
func TestPRTwoMembersTitledFromFirstSummaryBodyMentionsSecond(t *testing.T) {
	x := fixPR(t)
	ctx := context.Background()
	leaser := x.f.store.Leaser("runner-a")

	lease1, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire app: %v", err)
	}
	ws1, err := x.m.Checkout(ctx, lease1, x.f.base)
	if err != nil {
		t.Fatalf("checkout app: %v", err)
	}
	head1 := buildOn(t, ws1, "feature-1")

	// A second, distinct backing remote — built the same way fix(t) builds
	// the first — fetched into the same mirror under its own repo name.
	remote2 := filepath.Join(t.TempDir(), "remote2.git")
	seed2 := t.TempDir()
	mustGit(t, "", "init", "--bare", remote2)
	mustGit(t, remote2, "symbolic-ref", "HEAD", "refs/heads/main")
	mustGit(t, "", "clone", remote2, filepath.Join(seed2, "clone"))
	clone2 := filepath.Join(seed2, "clone")
	os.WriteFile(filepath.Join(clone2, "README.md"), []byte("seed2\n"), 0o644)
	mustGit(t, clone2, "add", ".")
	mustGit(t, clone2, "commit", "-m", "seed2")
	mustGit(t, clone2, "push", "origin", "HEAD:refs/heads/main")
	base2 := forge.Commit(strings.TrimSpace(mustGit(t, clone2, "rev-parse", "HEAD")))

	repo2 := forge.Repo{Name: "app2", Remote: githubRemoteNamed(t, remote2, "acme/app2"), Role: forge.RoleService, DefaultBranch: "main"}
	if err := x.m.Fetch(ctx, repo2); err != nil {
		t.Fatalf("fetch app2: %v", err)
	}
	lease2, err := leaser.Acquire(ctx, forge.LeaseRequest{Tenant: tn, Repo: "app2", Ref: "refs/heads/main", Run: "run-1", TTL: time.Hour})
	if err != nil {
		t.Fatalf("acquire app2: %v", err)
	}
	ws2, err := x.m.Checkout(ctx, lease2, base2)
	if err != nil {
		t.Fatalf("checkout app2: %v", err)
	}
	head2 := buildOn(t, ws2, "feature-2")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1",
		Intent: "a task paragraph spanning two repos, never a title",
		Members: []forge.Member{
			{Repo: "app", Branch: "main", Base: x.f.base, Head: head1, Lease: lease1, Summary: "close two repos at once"},
			{Repo: "app2", Branch: "main", Base: base2, Head: head2, Lease: lease2, Summary: "should never surface as a title"},
		},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	title1 := x.gh.byHead["acme/app#box/run-1"].Title
	title2 := x.gh.byHead["acme/app2#box/run-1"].Title
	if title1 != "close two repos at once" {
		t.Fatalf("app title = %q, want the first member's summary", title1)
	}
	if title2 != "close two repos at once" {
		t.Fatalf("app2 title = %q, want it titled from the first member's summary too", title2)
	}

	body := x.gh.byHead["acme/app#box/run-1"].Body
	if !strings.Contains(body, "app2") {
		t.Fatalf("pr body = %q, want it to mention the other member's repo (app2)", body)
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

// TestPRPostsCheckRunWithVerdicts is issue #58: a gate's resolved verdict,
// carried on ChangeSet.Verdicts, must reach GitHub as a box/check run named
// on the built head — not just fence-pushed silently — and the run's output
// must carry the evidence record id so an auditor can walk from the check
// back to the chain.
func TestPRPostsCheckRunWithVerdicts(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green",
				Evidence: []forge.EvidenceRef{{Kind: forge.EvidenceKindRecord, URI: "rec-abc123"}}},
		},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	if len(x.gh.checks) != 1 {
		t.Fatalf("want exactly one check-run posted, got %d: %+v", len(x.gh.checks), x.gh.checks)
	}
	got := x.gh.checks[0]
	if got.Name != "box/check" {
		t.Fatalf("check name = %q, want box/check", got.Name)
	}
	if got.HeadSHA != string(head) {
		t.Fatalf("check head_sha = %q, want %q", got.HeadSHA, head)
	}
	if got.Conclusion != "success" {
		t.Fatalf("check conclusion = %q, want success", got.Conclusion)
	}
	if !strings.Contains(got.Output.Text, "rec-abc123") {
		t.Fatalf("check output text = %q, want it to carry the evidence record id", got.Output.Text)
	}
}

// TestPRPostsReviewCheckAlongsideCheck is issue #62: the reviewer's verdict
// must reach GitHub as its own box/review check run, addressable apart from
// box/check's roll-up, with its conclusion and text taken from the review
// verdict alone rather than the resolved decision over every verdict.
func TestPRPostsReviewCheckAlongsideCheck(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green"},
			{Gate: "review", Decision: forge.DecisionPass, Reason: "looks right",
				Evidence: []forge.EvidenceRef{{Kind: forge.EvidenceKindRecord, URI: "rec-review1"}}},
		},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	if len(x.gh.checks) != 2 {
		t.Fatalf("want two check-runs posted, got %d: %+v", len(x.gh.checks), x.gh.checks)
	}
	var check, review *ghCheckRun
	for i := range x.gh.checks {
		switch x.gh.checks[i].Name {
		case "box/check":
			check = &x.gh.checks[i]
		case "box/review":
			review = &x.gh.checks[i]
		}
	}
	if check == nil {
		t.Fatalf("want a box/check run, got %+v", x.gh.checks)
	}
	if review == nil {
		t.Fatalf("want a box/review run, got %+v", x.gh.checks)
	}
	if review.HeadSHA != string(head) {
		t.Fatalf("box/review head_sha = %q, want %q", review.HeadSHA, head)
	}
	if review.Conclusion != "success" {
		t.Fatalf("box/review conclusion = %q, want success", review.Conclusion)
	}
	if !strings.Contains(review.Output.Summary, "looks right") {
		t.Fatalf("box/review summary = %q, want it to carry the review verdict's own reason", review.Output.Summary)
	}
	if strings.Contains(review.Output.Summary, "build-and-test") {
		t.Fatalf("box/review summary = %q, want only the review verdict, not the other gate", review.Output.Summary)
	}
	if !strings.Contains(review.Output.Text, "rec-review1") {
		t.Fatalf("box/review text = %q, want it to carry the review verdict's evidence id", review.Output.Text)
	}
}

// TestPRPostsFailingReviewCheck confirms a failing review verdict posts a
// failing box/review, independent of what the other gates in the same
// ChangeSet decided.
func TestPRPostsFailingReviewCheck(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "review", Decision: forge.DecisionFail, Reason: "missing test coverage"},
		},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}

	var review *ghCheckRun
	for i := range x.gh.checks {
		if x.gh.checks[i].Name == "box/review" {
			review = &x.gh.checks[i]
		}
	}
	if review == nil {
		t.Fatalf("want a box/review run, got %+v", x.gh.checks)
	}
	if review.Conclusion != "failure" {
		t.Fatalf("box/review conclusion = %q, want failure", review.Conclusion)
	}
	if !strings.Contains(review.Output.Summary, "missing test coverage") {
		t.Fatalf("box/review summary = %q, want it to carry the failing reason", review.Output.Summary)
	}
}

// TestPRPostsNoCheckRunWithoutVerdicts is the optimistic lane's edge case
// named in issue #58: a ChangeSet that reaches Land with no verdict yet must
// never have one fabricated for it.
func TestPRPostsNoCheckRunWithoutVerdicts(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	if len(x.gh.checks) != 0 {
		t.Fatalf("want no check-run posted for an empty verdict set, got %d: %+v", len(x.gh.checks), x.gh.checks)
	}
}

// TestPRPostsFailingLandCheckOnFencingRejection is issue #58's other half: a
// force-with-lease rejection (git.go's "push refused" error) has to surface
// on GitHub as a failing box/land check, or a reader sees a PR whose last
// check was a green box/check and no sign the actual land ever ran.
func TestPRPostsFailingLandCheckOnFencingRejection(t *testing.T) {
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

	// Sabotage: main moves out-of-band after the build, so the fenced push
	// PRLander delegates to will be refused.
	rogue := filepath.Join(t.TempDir(), "rogue")
	mustGit(t, "", "clone", x.f.remote, rogue)
	mustGit(t, rogue, "commit", "--allow-empty", "-m", "out of band")
	mustGit(t, rogue, "push", "origin", "HEAD:refs/heads/main")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green"},
		},
	}
	_, err = x.pr.Land(ctx, cs)
	if err == nil {
		t.Fatal("land should have been refused: base moved out from under it")
	}

	var landChecks []ghCheckRun
	for _, c := range x.gh.checks {
		if c.Name == "box/land" {
			landChecks = append(landChecks, c)
		}
	}
	if len(landChecks) != 1 {
		t.Fatalf("want exactly one box/land check posted, got %d: %+v", len(landChecks), x.gh.checks)
	}
	got := landChecks[0]
	if got.HeadSHA != string(head) {
		t.Fatalf("box/land head_sha = %q, want %q", got.HeadSHA, head)
	}
	if got.Conclusion != "failure" {
		t.Fatalf("box/land conclusion = %q, want failure", got.Conclusion)
	}
	if !strings.Contains(got.Output.Summary, "push refused") {
		t.Fatalf("box/land summary = %q, want it to carry the fencing rejection's reason", got.Output.Summary)
	}
}

// TestEnsureRequestIdempotentSecondCallOpensNoSecondPR is issue #66's gate
// requirement: an approval gate calls EnsureRequest on every tick for as
// long as a review takes, and that must never open a second pull request
// for the same run — forge.Gate's own idempotence rule.
func TestEnsureRequestIdempotentSecondCallOpensNoSecondPR(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "ask for approval",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}

	url1, err := x.pr.EnsureRequest(ctx, cs, cs.Members[0])
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	url2, err := x.pr.EnsureRequest(ctx, cs, cs.Members[0])
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if x.gh.creates != 1 {
		t.Fatalf("want exactly one PR created across two ensure calls, got %d", x.gh.creates)
	}
	if url1 != url2 {
		t.Fatalf("ensure returned different URLs for the same run: %q vs %q", url1, url2)
	}
}

// TestEnsureRequestRePushesOnMovedHead: a rebuild moves the member's head,
// and the run branch an approver is reading must move with it — the same
// open pull request, a new tip.
func TestEnsureRequestRePushesOnMovedHead(t *testing.T) {
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
	cs1 := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "ask for approval",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head1, Lease: lease}},
	}
	if _, err := x.pr.EnsureRequest(ctx, cs1, cs1.Members[0]); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if got := strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/box/run-1")); got != string(head1) {
		t.Fatalf("run branch = %s, want first head %s", got, head1)
	}

	head2 := buildOn(t, ws, "feature, revised")
	cs2 := cs1
	cs2.Members = []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head2, Lease: lease}}
	if _, err := x.pr.EnsureRequest(ctx, cs2, cs2.Members[0]); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if x.gh.creates != 1 {
		t.Fatalf("a moved head must reuse the open PR, not open a second: want 1 create, got %d", x.gh.creates)
	}
	if got := strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/box/run-1")); got != string(head2) {
		t.Fatalf("run branch = %s, want the rebuild's new head %s", got, head2)
	}
}

// TestReviewsDecodeAndOnlyApprovedCount: Reviews reports every review GitHub
// has on record — APPROVED, COMMENTED, DISMISSED alike, each with its
// commit_id — because silence must never read as approval and a caller
// cannot honor that with a list that already threw states away. Filtering
// to only APPROVED, which is the daemon's job, is exercised here as a plain
// predicate over what Reviews returned.
func TestReviewsDecodeAndOnlyApprovedCount(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "ask for approval",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.EnsureRequest(ctx, cs, cs.Members[0]); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	number := x.gh.byHead["acme/app#box/run-1"].Number
	if number == 0 {
		t.Fatal("fake github never recorded a PR number for the run branch")
	}
	submitted := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	x.gh.setReviews(number, []ghReview{
		{ID: 1, User: struct {
			Login string `json:"login"`
		}{Login: "alice"}, CommitID: string(head), State: "APPROVED", SubmittedAt: submitted},
		{ID: 2, User: struct {
			Login string `json:"login"`
		}{Login: "bob"}, CommitID: string(head), State: "DISMISSED", SubmittedAt: submitted},
		{ID: 3, User: struct {
			Login string `json:"login"`
		}{Login: "carol"}, CommitID: string(head), State: "COMMENTED", SubmittedAt: submitted},
	})

	reviews, err := x.pr.Reviews(ctx, cs, cs.Members[0])
	if err != nil {
		t.Fatalf("reviews: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("reviews = %d, want all 3 states reported, not just approvals: %+v", len(reviews), reviews)
	}
	for _, r := range reviews {
		if r.CommitID != string(head) {
			t.Fatalf("review %+v: commit_id = %q, want %q", r, r.CommitID, head)
		}
		if !r.SubmittedAt.Equal(submitted) {
			t.Fatalf("review %+v: submitted_at = %v, want %v", r, r.SubmittedAt, submitted)
		}
	}
	var approved int
	for _, r := range reviews {
		if r.State == "APPROVED" {
			approved++
		}
	}
	if approved != 1 {
		t.Fatalf("approved reviews = %d, want 1 — a dismissed or commenting review is not an approval", approved)
	}
}

// TestAnOAuthTokenReportsVerdictsAsStatuses. The first landing through the
// reviewer onto main built, passed both gates, opened its pull request and
// parked at the check run: a check run is a GitHub App's to create, and
// `land_token: gh` is an OAuth token. A lander told it holds an OAuth token
// posts the same verdicts as commit statuses — the form the token can post
// — under the same context names, and never touches /check-runs.
func TestAnOAuthTokenReportsVerdictsAsStatuses(t *testing.T) {
	x := fixPR(t)
	x.gh.appOnly = true
	x.pr.StatusesOnly()
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green"},
			{Gate: "review", Decision: forge.DecisionPass, Reason: "looks right"},
		},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	if len(x.gh.checks) != 0 {
		t.Fatalf("an OAuth lander posted check runs: %+v", x.gh.checks)
	}
	if len(x.gh.statuses) != 2 {
		t.Fatalf("want two statuses posted, got %d: %+v", len(x.gh.statuses), x.gh.statuses)
	}
	by := map[string]ghStatus{}
	for _, st := range x.gh.statuses {
		by[st.Context] = st
	}
	for _, name := range []string{"box/check", "box/review"} {
		st, ok := by[name]
		if !ok {
			t.Fatalf("no %s status: %+v", name, x.gh.statuses)
		}
		if st.SHA != string(head) || st.State != "success" {
			t.Fatalf("%s = %+v, want success on %s", name, st, head)
		}
		if len([]rune(st.Description)) > maxStatusDescription {
			t.Fatalf("%s description is over GitHub's bound: %q", name, st.Description)
		}
	}
	if !strings.Contains(by["box/review"].Description, "looks right") {
		t.Fatalf("box/review description = %q, want the review verdict's reason", by["box/review"].Description)
	}
}

// TestAStatusDescriptionIsCutAtGitHubsBound: a long summary is cut with the
// cut marked, never refused by the API after the pull request exists.
func TestAStatusDescriptionIsCutAtGitHubsBound(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := statusDescription("gate: pass", long)
	if n := len([]rune(got)); n != maxStatusDescription {
		t.Fatalf("len = %d, want %d", n, maxStatusDescription)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("the cut is not marked: %q", got[len(got)-10:])
	}
}

// buildOnWorkflow makes one commit in the workspace that adds a file under
// .github/workflows/, the path [PRLander.assertWorkflowScope] watches for.
func buildOnWorkflow(t *testing.T, ws forge.Workspace, msg string) forge.Commit {
	t.Helper()
	dir := filepath.Join(ws.Dir, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir workflows dir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "ci.yml"), []byte(msg+"\n"), 0o644)
	mustGit(t, ws.Dir, "add", ".")
	mustGit(t, ws.Dir, "commit", "-m", msg)
	return forge.Commit(strings.TrimSpace(mustGit(t, ws.Dir, "rev-parse", "HEAD")))
}

// remoteHasBranch reports whether remote carries branch — used to assert a
// refused scope assertion never let the run branch push reach the remote.
func remoteHasBranch(t *testing.T, remote, branch string) bool {
	t.Helper()
	out := mustGit(t, "", "ls-remote", remote, "refs/heads/"+branch)
	return strings.TrimSpace(out) != ""
}

// TestPRRefusesWorkflowChangeWithoutWorkflowScope is issue #78's forge half:
// a change touching .github/workflows/ pushed with a token whose scopes
// lack `workflow` is refused before any push happens, rather than parking
// later on GitHub's own rejection of the push.
func TestPRRefusesWorkflowChangeWithoutWorkflowScope(t *testing.T) {
	x := fixPR(t)
	scopes := "repo"
	x.gh.scopesHeader = &scopes
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
	head := buildOnWorkflow(t, ws, "add ci workflow")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land a workflow change",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green"},
		},
	}
	_, err = x.pr.Land(ctx, cs)
	if err == nil {
		t.Fatal("land should have been refused: token lacks the workflow scope")
	}
	if !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("error = %q, want it to name the missing workflow scope", err.Error())
	}
	if !strings.Contains(err.Error(), "gh auth refresh -s workflow") {
		t.Fatalf("error = %q, want it to contain the fix command verbatim", err.Error())
	}
	if remoteHasBranch(t, x.f.remote, "box/run-1") {
		t.Fatal("run branch was pushed despite the missing scope — the assertion must run before any push")
	}
	if len(x.gh.checks) != 0 || len(x.gh.statuses) != 0 {
		t.Fatalf("a scope refusal is not a verdict and must post nothing: checks=%+v statuses=%+v", x.gh.checks, x.gh.statuses)
	}
	if x.gh.creates != 0 {
		t.Fatalf("want no pull request created, got %d", x.gh.creates)
	}
}

// TestPRLandsWorkflowChangeWithWorkflowScope: the same change lands
// normally end to end once the token's scopes include `workflow`.
func TestPRLandsWorkflowChangeWithWorkflowScope(t *testing.T) {
	x := fixPR(t)
	scopes := "repo, workflow"
	x.gh.scopesHeader = &scopes
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
	head := buildOnWorkflow(t, ws, "add ci workflow")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land a workflow change",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	landing, err := x.pr.Land(ctx, cs)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !landing.Complete() {
		t.Fatalf("landing incomplete: %+v", landing)
	}
	got := strings.TrimSpace(mustGit(t, x.f.remote, "rev-parse", "refs/heads/main"))
	if forge.Commit(got) != head {
		t.Fatalf("remote main = %s, want %s", got, head)
	}
	if x.gh.creates != 1 {
		t.Fatalf("want exactly one PR created, got %d", x.gh.creates)
	}
}

// TestPRSkipsScopeProbeWhenNoWorkflowFileChanges: probing the token's
// scopes is not free, and GitHub only ever rejects a push over this scope
// for a change that touches .github/workflows/ — pr_test.go's other
// fixtures never touch that path, and this pins that the probe request is
// never sent for them.
func TestPRSkipsScopeProbeWhenNoWorkflowFileChanges(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	if _, err := x.pr.Land(ctx, cs); err != nil {
		t.Fatalf("land: %v", err)
	}
	for _, req := range x.gh.requests {
		if strings.HasPrefix(req, "GET /rate_limit") {
			t.Fatalf("scope probe sent for a change that never touched .github/workflows/: %v", x.gh.requests)
		}
	}
}

// TestPRRefusesWorkflowChangeWhenTokenReportsNoScopes: a fine-grained PAT
// or GitHub App installation token authenticates fine but never sends
// X-OAuth-Scopes at all — that absence must read as neither "has every
// scope" nor "has none," so the refusal names the distinct reason instead
// of claiming the scope is missing.
func TestPRRefusesWorkflowChangeWhenTokenReportsNoScopes(t *testing.T) {
	x := fixPR(t)
	x.gh.scopesHeader = nil
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
	head := buildOnWorkflow(t, ws, "add ci workflow")

	cs := forge.ChangeSet{
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land a workflow change",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	_, err = x.pr.Land(ctx, cs)
	if err == nil {
		t.Fatal("land should have been refused: token does not report scopes")
	}
	if strings.Contains(err.Error(), "lacks the workflow scope") {
		t.Fatalf("error = %q, want the distinct cannot-assert message, not the missing-scope one", err.Error())
	}
	if !strings.Contains(err.Error(), "does not report") {
		t.Fatalf("error = %q, want it to say the token does not report scopes", err.Error())
	}
	if remoteHasBranch(t, x.f.remote, "box/run-1") {
		t.Fatal("run branch was pushed despite being unable to assert the scope")
	}
}

// landForIssueClose builds and lands one changeset naming issue "9", so
// each of the issue-closing tests below can vary only cs.CompletesPlan (or
// the fake GitHub server's behavior) and read the result back off the
// returned Landing.
func landForIssueClose(t *testing.T, x *prFix, completesPlan bool, verdicts []forge.Verdict) (forge.Landing, forge.Commit) {
	t.Helper()
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		DrivingIssue: "9", CompletesPlan: completesPlan,
		Members:  []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		Verdicts: verdicts,
	}
	landing, err := x.pr.Land(ctx, cs)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	return landing, head
}

// TestPRClosesIssueOnlyWhenCompletesPlanAndDrivingIssueSet is issue #108:
// closing the driving issue is the loop's own assertion, carried onto
// cs.CompletesPlan, and PRLander must never act on it unless both that AND
// DrivingIssue are set.
func TestPRClosesIssueOnlyWhenCompletesPlanAndDrivingIssueSet(t *testing.T) {
	t.Run("completes plan with driving issue closes and comments", func(t *testing.T) {
		x := fixPR(t)
		landing, head := landForIssueClose(t, x, true, []forge.Verdict{
			{Gate: "build-and-test", Decision: forge.DecisionPass, Reason: "all green",
				Evidence: []forge.EvidenceRef{{Kind: forge.EvidenceKindRecord, URI: "rec-xyz"}}},
		})
		if !landing.Landed[0].IssueClosed {
			t.Fatal("Landed[0].IssueClosed = false, want true")
		}
		st := x.gh.issue("acme/app#9")
		if st.State != "closed" {
			t.Fatalf("issue state = %q, want closed", st.State)
		}
		if st.Patches != 1 {
			t.Fatalf("issue patched %d times, want 1", st.Patches)
		}
		if len(st.Comments) != 1 {
			t.Fatalf("issue has %d comments, want 1", len(st.Comments))
		}
		if !strings.Contains(st.Comments[0], string(head)) {
			t.Fatalf("comment %q does not name the landed sha %s", st.Comments[0], head)
		}
		if !strings.Contains(st.Comments[0], landing.Landed[0].PRURL) {
			t.Fatalf("comment %q does not name the pull request URL %s", st.Comments[0], landing.Landed[0].PRURL)
		}
		if !strings.Contains(st.Comments[0], "rec-xyz") {
			t.Fatalf("comment %q does not name the verdict's evidence id", st.Comments[0])
		}
	})

	t.Run("completes plan without driving issue does nothing", func(t *testing.T) {
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
			ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "routine change", CompletesPlan: true,
			Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
		}
		landing, err := x.pr.Land(ctx, cs)
		if err != nil {
			t.Fatalf("land: %v", err)
		}
		if landing.Landed[0].IssueClosed {
			t.Fatal("Landed[0].IssueClosed = true, want false with no DrivingIssue")
		}
		for _, req := range x.gh.requests {
			if strings.Contains(req, "/issues/") {
				t.Fatalf("no issues API call expected without a driving issue, got %v", x.gh.requests)
			}
		}
	})

	t.Run("driving issue without completing the plan does nothing", func(t *testing.T) {
		x := fixPR(t)
		landing, _ := landForIssueClose(t, x, false, nil)
		if landing.Landed[0].IssueClosed {
			t.Fatal("Landed[0].IssueClosed = true, want false: this landing did not complete the plan")
		}
		for _, req := range x.gh.requests {
			if strings.Contains(req, "/issues/") {
				t.Fatalf("no issues API call expected when CompletesPlan is false, got %v", x.gh.requests)
			}
		}
	})
}

// TestPRClosesIssueIdempotentlyAlreadyClosed is issue #108's idempotence
// half: an issue a person (or an earlier retried tick) already closed stays
// closed, but the trail comment still lands exactly once.
func TestPRClosesIssueIdempotentlyAlreadyClosed(t *testing.T) {
	x := fixPR(t)
	x.gh.issue("acme/app#9").State = "closed"
	landing, _ := landForIssueClose(t, x, true, nil)
	if !landing.Landed[0].IssueClosed {
		t.Fatal("Landed[0].IssueClosed = false, want true: GitHub answered and confirmed the issue is closed")
	}
	st := x.gh.issue("acme/app#9")
	if st.Patches != 0 {
		t.Fatalf("issue patched %d times, want 0: it was already closed", st.Patches)
	}
	if len(st.Comments) != 1 {
		t.Fatalf("issue has %d comments, want 1", len(st.Comments))
	}
}

// TestPRClosesIssueCommentNotDuplicatedOnRetry is issue #108's other
// idempotence half: a retried tick landing the SAME commit again must not
// post a second comment naming a sha the issue already carries.
func TestPRClosesIssueCommentNotDuplicatedOnRetry(t *testing.T) {
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		DrivingIssue: "9", CompletesPlan: true,
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	// Seed the issue with a comment already naming this exact sha, as if an
	// earlier retried tick had already landed and closed it.
	x.gh.issue("acme/app#9").Comments = append(x.gh.issue("acme/app#9").Comments, fmt.Sprintf("Landed as %s via https://example.invalid/pull/1.", head))
	landing, err := x.pr.Land(ctx, cs)
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if !landing.Landed[0].IssueClosed {
		t.Fatal("Landed[0].IssueClosed = false, want true")
	}
	st := x.gh.issue("acme/app#9")
	if len(st.Comments) != 1 {
		t.Fatalf("issue has %d comments, want exactly the one seeded, not a duplicate: %v", len(st.Comments), st.Comments)
	}
}

// TestPRIssuesAPIFailureDoesNotFailLandingOrCloseIssue is issue #108's
// failure-isolation requirement: the commit is already on the branch by the
// time the issues API is ever asked about, so a 500 there must not fail the
// landing, and must leave IssueClosed false — an unanswered close records
// nothing.
func TestPRIssuesAPIFailureDoesNotFailLandingOrCloseIssue(t *testing.T) {
	x := fixPR(t)
	x.gh.issueGet500 = true
	landing, _ := landForIssueClose(t, x, true, nil)
	if !landing.Complete() {
		t.Fatalf("landing incomplete despite the issues API failing: %+v", landing)
	}
	if landing.Landed[0].IssueClosed {
		t.Fatal("Landed[0].IssueClosed = true, want false: the issues API never answered")
	}
}

// landThroughPR builds a one-member ChangeSet on a fresh feature commit and
// lands it through x.pr, for tests that only care about the error the PR
// stage itself returns, not about closing a driving issue.
func landThroughPR(t *testing.T, x *prFix) (forge.Landing, error) {
	t.Helper()
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}
	return x.pr.Land(ctx, cs)
}

// TestPRLandSurfacesTypedTransientErrorOn500 is issue #161: a 500 answering
// the PR-create call must reach PRLander.Land as the typed *HTTPError — not
// a plain fmt.Errorf nobody downstream can classify — and Transient must
// call it retriable: the forge's own bad minute, not a fact about the
// change.
func TestPRLandSurfacesTypedTransientErrorOn500(t *testing.T) {
	x := fixPR(t)
	x.gh.postPRStatus = http.StatusInternalServerError

	_, err := landThroughPR(t, x)
	if err == nil {
		t.Fatal("want error from a 500 on PR create")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("errors.As did not find *HTTPError in: %v", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", httpErr.StatusCode)
	}
	if !Transient(err) {
		t.Fatalf("Transient(%v) = false, want true for a 500", err)
	}
}

// TestPRLandNonTransientOn422 is the other half of #161: a 422 (validation,
// e.g. the workflow-scope check from #78) is a fact about the request, not
// the forge's trouble, and must never be retried.
func TestPRLandNonTransientOn422(t *testing.T) {
	x := fixPR(t)
	x.gh.postPRStatus = http.StatusUnprocessableEntity

	_, err := landThroughPR(t, x)
	if err == nil {
		t.Fatal("want error from a 422 on PR create")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("errors.As did not find *HTTPError in: %v", err)
	}
	if httpErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("StatusCode = %d, want 422", httpErr.StatusCode)
	}
	if Transient(err) {
		t.Fatalf("Transient(%v) = true, want false for a 422", err)
	}
}

// resetConnectionServer answers every request by hijacking the connection
// and closing it with SO_LINGER 0, which forces a TCP RST rather than a
// clean FIN — the deterministic way to make an HTTP client observe a
// connection reset rather than a timeout or a clean EOF.
func resetConnectionServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetLinger(0)
		}
		conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPRLandTransientOnConnectionReset is #161's transport-level case: a
// connection reset never reaches GitHub's answer at all, so there is no
// *HTTPError to type-assert on, but Transient must still call it retriable
// — it is unambiguously the forge's own trouble reaching GitHub, not a fact
// about the change.
func TestPRLandTransientOnConnectionReset(t *testing.T) {
	x := fixPR(t)
	reset := resetConnectionServer(t)
	x.pr.apiBase = reset.URL
	x.pr.httpClient = reset.Client()

	_, err := landThroughPR(t, x)
	if err == nil {
		t.Fatal("want error from a reset connection")
	}
	if !Transient(err) {
		t.Fatalf("Transient(%v) = false, want true for a connection reset", err)
	}
}

// TestTransientFalseOnContextCanceled is #161's explicit exclusion: context
// cancellation must never read as the forge's own trouble, including when
// it surfaces wrapped in a *url.Error the way httpClient.Do reports it.
func TestTransientFalseOnContextCanceled(t *testing.T) {
	x := fixPR(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out any
	err := x.pr.call(ctx, http.MethodGet, x.gh.URL+"/rate_limit", "test-token", nil, &out)
	if err == nil {
		t.Fatal("want error from a call made with an already-canceled context")
	}
	if Transient(err) {
		t.Fatalf("Transient(%v) = true, want false for context.Canceled", err)
	}
}

// TestPRLandRetriesAfterLostCreateAck is the idempotency case #161 names
// directly: the first POST /pulls creates the PR on GitHub's side but the
// forge only sees a 500 (the ack lost, not the write). A retried Land must
// find the already-created PR through the list-open-PRs-by-head lookup in
// ensurePR rather than create a second one.
func TestPRLandRetriesAfterLostCreateAck(t *testing.T) {
	x := fixPR(t)
	x.gh.postPRLoseAckOnce = true
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
		ID: "cs-1", Tenant: tn, Run: "run-1", Intent: "land through a pr",
		Members: []forge.Member{{Repo: "app", Branch: "main", Base: x.f.base, Head: head, Lease: lease}},
	}

	if _, err := x.pr.Land(ctx, cs); err == nil {
		t.Fatal("want error from the first land: the create ack was lost")
	} else if !Transient(err) {
		t.Fatalf("Transient(%v) = false, want true: a lost ack is the forge's own trouble", err)
	}

	// Same ChangeSet, same still-held lease: a retried tick resumes the
	// same run rather than acquiring a new one.
	landing, err := x.pr.Land(ctx, cs)
	if err != nil {
		t.Fatalf("retried land: %v", err)
	}
	if !landing.Complete() {
		t.Fatalf("retried landing incomplete: %+v", landing)
	}

	posts := 0
	for _, r := range x.gh.requests {
		if r == "POST /repos/acme/app/pulls" {
			posts++
		}
	}
	if posts != 1 {
		t.Fatalf("POST .../pulls seen %d times across both attempts, want exactly 1", posts)
	}
}
