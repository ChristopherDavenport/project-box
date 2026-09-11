package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGHIssueListerSkipsPullRequestsAndReturnsLabeledIssues: GitHub's issues
// endpoint also returns pull requests, and a route left to poll without that
// filter would open work against the box's own PRs.
func TestGHIssueListerSkipsPullRequestsAndReturnsLabeledIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/app/issues" {
			t.Fatalf("path = %q, want /repos/acme/app/issues", r.URL.Path)
		}
		if got := r.URL.Query().Get("labels"); got != "box" {
			t.Fatalf("labels query = %q, want box", got)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Fatalf("state query = %q, want open", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("authorization = %q, want Bearer tok", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "a real issue", "body": "the body", "labels": []map[string]string{{"name": "box"}}},
			{"number": 2, "title": "a pull request", "body": "", "pull_request": map[string]string{"url": "https://api.github.com/x"}},
		})
	}))
	defer ts.Close()

	g := newGHIssueLister(func(ctx context.Context) (string, error) { return "tok", nil })
	g.apiBase = ts.URL
	g.httpClient = ts.Client()

	issues, err := g.List(context.Background(), "https://github.com/acme/app.git", "box")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1 (the pull request must be skipped)", len(issues))
	}
	if issues[0].Number != 1 || issues[0].Title != "a real issue" || issues[0].Body != "the body" {
		t.Fatalf("issue = %+v", issues[0])
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "box" {
		t.Fatalf("labels = %v, want [box]", issues[0].Labels)
	}
}

// TestGHOwnerRepoParsesEveryRemoteForm mirrors internal/forge/git's own
// parseGitHubRemote coverage for its twin here.
func TestGHOwnerRepoParsesEveryRemoteForm(t *testing.T) {
	for _, tc := range []struct{ remote, owner, repo string }{
		{"https://github.com/acme/app.git", "acme", "app"},
		{"https://github.com/acme/app", "acme", "app"},
		{"git@github.com:acme/app.git", "acme", "app"},
	} {
		owner, repo, err := ghOwnerRepo(tc.remote)
		if err != nil {
			t.Fatalf("%s: %v", tc.remote, err)
		}
		if owner != tc.owner || repo != tc.repo {
			t.Fatalf("%s: owner/repo = %s/%s, want %s/%s", tc.remote, owner, repo, tc.owner, tc.repo)
		}
	}
	if _, _, err := ghOwnerRepo("https://gitlab.com/acme/app.git"); err == nil {
		t.Fatal("a non-github remote was accepted")
	}
}
