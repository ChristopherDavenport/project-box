package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
)

// landTokenScopeProbe asks GitHub what scopes the land token carries, for a
// repo assemble is wiring for pull_request landing — the same shape
// ghIssueLister already uses: token/apiBase/httpClient defaulted to
// ghAuthToken/api.github.com/http.DefaultClient, overridden in tests with an
// httptest.Server.
type landTokenScopeProbe struct {
	token gitforge.TokenSource

	apiBase    string
	httpClient *http.Client
}

func newLandTokenScopeProbe(token gitforge.TokenSource) *landTokenScopeProbe {
	return &landTokenScopeProbe{token: token, apiBase: "https://api.github.com", httpClient: http.DefaultClient}
}

// landTokenScopes is one probe's answer, and — json-marshaled — the body of
// the evidence record assemble writes for it. Answered is false for every
// outcome the probe cannot read as a fact: a non-GitHub remote, a token
// fetch or network failure, an absent X-OAuth-Scopes header (fine-grained
// and GitHub App tokens do not send one), or a status other than 200/404.
// Reason names which. HasWorkflowsDir only means something when Answered —
// it is the status of the same response Scopes came from, not a second
// request.
type landTokenScopes struct {
	Answered        bool   `json:"answered"`
	Scopes          string `json:"scopes,omitempty"`
	HasWorkflowsDir bool   `json:"has_workflows_dir"`
	Reason          string `json:"reason,omitempty"`
}

// missingWorkflowScope reports the one definite gap this probe exists to
// catch before the remote does: an answered, classic-OAuth scope list
// lacking "workflow", for a repository that already has a
// .github/workflows directory a pull-request landing's push would have to
// create or update a file under. Every other outcome — including an
// answered scope list for a repo with no workflows directory yet — is not
// a gap this probe can see, so it does not refuse.
func (s landTokenScopes) missingWorkflowScope() bool {
	if !s.Answered || !s.HasWorkflowsDir {
		return false
	}
	for _, sc := range strings.Split(s.Scopes, ",") {
		if strings.TrimSpace(sc) == "workflow" {
			return false
		}
	}
	return true
}

// missingIssuesScope reports the one definite gap closing a driving issue
// exists to catch before GitHub's own 403 does: an answered, classic-OAuth
// scope list carrying neither "repo" nor "public_repo". Classic OAuth has no
// standalone "issues" scope — issue write rides "repo" (and "public_repo"
// for a public repository) — so those are the only two names that satisfy
// it. Unlike missingWorkflowScope this does not gate on HasWorkflowsDir:
// issue closing is unconditional on pull_request landing, not on a repo
// shape. Every indefinite outcome is, as with missingWorkflowScope, not a
// gap this probe can see, so it does not refuse.
func (s landTokenScopes) missingIssuesScope() bool {
	if !s.Answered {
		return false
	}
	for _, sc := range strings.Split(s.Scopes, ",") {
		switch strings.TrimSpace(sc) {
		case "repo", "public_repo":
			return false
		}
	}
	return true
}

// probe parses remote to an owner/repo FIRST, with the daemon's own
// ghOwnerRepo — a remote that does not name a github.com repository is
// probe-cannot-answer without ever calling token (which shells out to `gh
// auth token`) or touching the network. That ordering is what keeps
// assemble offline for every non-GitHub remote already exercised by this
// package's other tests.
func (p *landTokenScopeProbe) probe(ctx context.Context, remote string) landTokenScopes {
	owner, repo, err := ghOwnerRepo(remote)
	if err != nil {
		return landTokenScopes{Reason: fmt.Sprintf("remote %q is not a github.com remote — land-token scopes cannot be probed", remote)}
	}
	token, err := p.token(ctx)
	if err != nil {
		return landTokenScopes{Reason: fmt.Sprintf("fetching the land token: %v", err)}
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/.github/workflows", p.apiBase, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return landTokenScopes{Reason: fmt.Sprintf("building the probe request: %v", err)}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return landTokenScopes{Reason: fmt.Sprintf("GET %s: %v", endpoint, err)}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	scopes := resp.Header.Get("X-OAuth-Scopes")
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		if scopes == "" {
			return landTokenScopes{Reason: fmt.Sprintf("GET %s: %s: no X-OAuth-Scopes header — not a classic OAuth token", endpoint, resp.Status)}
		}
		return landTokenScopes{Answered: true, Scopes: scopes, HasWorkflowsDir: resp.StatusCode == http.StatusOK}
	default:
		return landTokenScopes{Reason: fmt.Sprintf("GET %s: unexpected status %s", endpoint, resp.Status)}
	}
}
