package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
)

// ghIssueLister is the [ingress.Lister] internal/ingress.Poller reads a
// route's forge through, for the one forge this daemon speaks. apiBase and
// httpClient are overridable so a test can stand an httptest.Server in —
// the same shape internal/forge/git/pr.go's PRLander already uses for its
// own REST calls.
type ghIssueLister struct {
	token gitforge.TokenSource

	apiBase    string
	httpClient *http.Client
}

// newGHIssueLister wires token as both the poller's and (when land:
// pull_request is on) the lander's credential source — ghAuthToken today in
// both cases, so this is the seam, not a second credential path.
func newGHIssueLister(token gitforge.TokenSource) *ghIssueLister {
	return &ghIssueLister{token: token, apiBase: "https://api.github.com", httpClient: http.DefaultClient}
}

var _ ingress.Lister = (*ghIssueLister)(nil)

// List reports target's open issues, narrowed server-side to label when one
// is given. GitHub's issues endpoint also returns pull requests, so any item
// carrying a "pull_request" object is skipped here — a route left to poll
// without that filter would open work against the box's own PRs.
func (g *ghIssueLister) List(ctx context.Context, target, label string) ([]ingress.Issue, error) {
	owner, repo, err := ghOwnerRepo(target)
	if err != nil {
		return nil, err
	}
	token, err := g.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: %w", err)
	}
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "100")
	if label != "" {
		q.Set("labels", label)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues?%s", g.apiBase, owner, repo, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github: GET %s: %s: %s", endpoint, resp.Status, strings.TrimSpace(string(data)))
	}
	var out []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("github: GET %s: %w", endpoint, err)
	}
	issues := make([]ingress.Issue, 0, len(out))
	for _, o := range out {
		if len(o.PullRequest) > 0 {
			continue
		}
		labels := make([]string, 0, len(o.Labels))
		for _, l := range o.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, ingress.Issue{Number: o.Number, Title: o.Title, Body: o.Body, Labels: labels})
	}
	return issues, nil
}

// ghOwnerRepo derives owner/repo from a github.com remote URL. A twin of
// internal/forge/git/pr.go's parseGitHubRemote, duplicated rather than
// exported across the section boundary between this package and the forge
// packages — the daemon and the loop's forge layer are different sections,
// and a shared unexported helper would cross it.
func ghOwnerRepo(remote string) (owner, repo string, err error) {
	trimmed := strings.TrimSuffix(remote, ".git")
	var rest string
	switch {
	case strings.HasPrefix(trimmed, "git@github.com:"):
		rest = strings.TrimPrefix(trimmed, "git@github.com:")
	case strings.HasPrefix(trimmed, "https://github.com/"):
		rest = strings.TrimPrefix(trimmed, "https://github.com/")
	case strings.HasPrefix(trimmed, "http://github.com/"):
		rest = strings.TrimPrefix(trimmed, "http://github.com/")
	default:
		return "", "", fmt.Errorf("daemon: remote %q is not a github.com remote", remote)
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("daemon: remote %q does not name an owner/repo", remote)
	}
	return parts[0], parts[1], nil
}
