package git

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu      sync.Mutex
	byHead  map[string]ghPR
	creates int
	next    int
	checks  []ghCheckRun
}

type ghPR struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"html_url"`
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

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{byHead: map[string]ghPR{}, next: 100}
	g.Server = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.Server.Close)
	return g
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "repos" {
		http.NotFound(w, r)
		return
	}
	owner, repo := parts[1], parts[2]

	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case parts[3] == "pulls" && r.Method == http.MethodGet:
		head := r.URL.Query().Get("head") // "owner:branch"
		hp := strings.SplitN(head, ":", 2)
		key := owner + "/" + repo + "#" + hp[len(hp)-1]
		var out []ghPR
		if pr, ok := g.byHead[key]; ok {
			out = append(out, pr)
		}
		json.NewEncoder(w).Encode(out)
	case parts[3] == "pulls" && r.Method == http.MethodPost:
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
	case parts[3] == "check-runs" && r.Method == http.MethodPost:
		var in ghCheckRun
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.Owner, in.Repo = owner, repo
		g.checks = append(g.checks, in)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(in)
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
		Intent: "a much longer task paragraph the planner wrote for the builder, never meant to title a PR",
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
		Intent: "the planner's whole task paragraph, still never a title",
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
		Intent: "the planner's whole task paragraph, never a title, regardless of Summary's length",
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
