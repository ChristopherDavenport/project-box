package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// TokenSource supplies a bearer token for the GitHub REST API, fetched fresh
// per call rather than held. It is a seam, not a policy: today it is wired to
// a plain token; a forge.Minter-backed implementation
// (internal/forge/credential.go) can satisfy the same signature later
// without callers of [PRLander] changing. That path stays reserved and
// unimplemented until Phase 7 (docs/PLAN.md Phase 7) — this file never
// touches forge.Minter.
type TokenSource func(ctx context.Context) (string, error)

// PRLander wraps a *[Lander] to open, or reuse, a GitHub pull request as
// part of landing — without changing the commit that lands.
//
// GitHub's merge/squash/rebase-merge endpoints always mint a NEW commit sha,
// and every downstream record keyed by the sha the builder produced —
// evidence subjects, checks.Write, the promotion ladder, `box provenance
// <sha>` — would silently stop matching the content that landed. So this
// never calls those endpoints. Land pushes the built head to a disposable
// per-run branch, opens (or reuses) a pull request from that branch onto the
// member's branch, and then delegates the actual landing to the wrapped
// Lander completely unchanged: the same force-with-lease fenced push of the
// exact built commit, same compensation on failure. Once that push lands the
// identical sha on the base branch, GitHub's own backend notices the PR's
// head commit is now reachable from base and marks it merged on its own —
// that is the mechanism, not a second API call.
type PRLander struct {
	inner *Lander
	token TokenSource

	// apiBase and httpClient are overridable so tests can stand an
	// httptest.Server in for the real GitHub REST API; production callers get
	// the zero value's defaults from NewPRLander.
	apiBase    string
	httpClient *http.Client
}

// NewPRLander wraps inner to open a GitHub pull request at land time,
// immediately before the fenced push. token supplies the bearer credential
// per call — never a hardcoded secret.
func NewPRLander(inner *Lander, token TokenSource) *PRLander {
	return &PRLander{inner: inner, token: token, apiBase: "https://api.github.com", httpClient: http.DefaultClient}
}

var _ forge.Lander = (*PRLander)(nil)

// Land opens or reuses one pull request per member — head the disposable
// run branch `box/<run>`, base the member's branch, title the ChangeSet's
// Intent, and a body naming DrivingIssue with "Fixes #<n>" when the change
// originated from one — then delegates to the wrapped Lander for the actual
// landing. A retried tick reuses the still-open PR for the same head branch
// rather than opening a duplicate.
func (p *PRLander) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if len(cs.Members) == 0 {
		return forge.Landing{}, errors.New("git: empty ChangeSet")
	}
	branch := "box/" + cs.Run
	body := ""
	if cs.DrivingIssue != "" {
		body = "Fixes #" + cs.DrivingIssue
	}

	prURLs := make(map[string]string, len(cs.Members))
	for _, mem := range cs.Members {
		repo, err := p.pushRunBranch(ctx, cs, mem, branch)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: %w", err)
		}
		owner, name, err := parseGitHubRemote(repo.Remote)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: %w", err)
		}
		tok, err := p.token(ctx)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: token: %w", err)
		}
		prURL, err := p.ensurePR(ctx, tok, owner, name, branch, mem.Branch, cs.Intent, body)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: open pr for %s: %w", mem.Repo, err)
		}
		prURLs[mem.Repo] = prURL
	}

	landing, err := p.inner.Land(ctx, cs)
	for i := range landing.Landed {
		pl := &landing.Landed[i]
		prURL, ok := prURLs[pl.Repo]
		if !ok {
			continue
		}
		pl.PRURL = prURL
		p.inner.record(ctx, cs, "pr-opened", string(pl.Now),
			fmt.Sprintf(`{"pr_url":%q,"issue":%q}`, prURL, cs.DrivingIssue))
	}
	return landing, err
}

// pushRunBranch collects the member's head from its workspace, exactly the
// way [Lander.push] does, and pushes it to the disposable run branch. Run
// branches are per-run and disposable (docs/ARCHITECTURE.md "Enforcement on
// GitHub"), so — unlike landing onto mem.Branch — this is a plain push, not
// fenced with force-with-lease: nobody else contends for a branch scoped to
// this run, and a retried tick simply overwrites its own prior attempt.
func (p *PRLander) pushRunBranch(ctx context.Context, cs forge.ChangeSet, mem forge.Member, branch string) (forge.Repo, error) {
	repo, ok := p.inner.m.Repo(mem.Repo)
	if !ok {
		return forge.Repo{}, fmt.Errorf("repo %q not established by Fetch", mem.Repo)
	}
	mirror := p.inner.m.mirrorDir(mem.Repo)
	ws := p.inner.m.workDir(cs.Run, mem.Repo)
	if _, err := run(ctx, mirror, "fetch", ws, string(mem.Head)); err != nil {
		return forge.Repo{}, fmt.Errorf("collect %s from workspace: %w", mem.Head, err)
	}
	if _, err := run(ctx, mirror, "push", "--force", repo.Remote, string(mem.Head)+":refs/heads/"+branch); err != nil {
		return forge.Repo{}, fmt.Errorf("push run branch %s: %w", branch, err)
	}
	return repo, nil
}

// parseGitHubRemote derives owner/repo from a github.com remote URL, in
// either form git itself produces. It refuses rather than guesses: a remote
// that is not github.com cannot be turned into a REST API call by
// pattern-matching harder.
func parseGitHubRemote(remote string) (owner, repo string, err error) {
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
		return "", "", fmt.Errorf("git: remote %q is not a github.com remote", remote)
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("git: remote %q does not name an owner/repo", remote)
	}
	return parts[0], parts[1], nil
}

// ensurePR looks up an open pull request for head first and reuses it,
// since a retried tick must be idempotent; only opens a new one when none
// exists.
func (p *PRLander) ensurePR(ctx context.Context, token, owner, repo, head, base, title, body string) (string, error) {
	existing, err := p.findOpenPR(ctx, token, owner, repo, head)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}
	return p.createPR(ctx, token, owner, repo, head, base, title, body)
}

func (p *PRLander) findOpenPR(ctx context.Context, token, owner, repo, head string) (string, error) {
	q := url.Values{}
	q.Set("head", owner+":"+head)
	q.Set("state", "open")
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", p.apiBase, owner, repo, q.Encode())
	var out []struct {
		HTMLURL string `json:"html_url"`
	}
	if err := p.call(ctx, http.MethodGet, endpoint, token, nil, &out); err != nil {
		return "", fmt.Errorf("list open prs: %w", err)
	}
	if len(out) == 0 {
		return "", nil
	}
	return out[0].HTMLURL, nil
}

func (p *PRLander) createPR(ctx context.Context, token, owner, repo, head, base, title, body string) (string, error) {
	in := struct {
		Title string `json:"title"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Body  string `json:"body,omitempty"`
	}{Title: title, Head: head, Base: base, Body: body}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls", p.apiBase, owner, repo)
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := p.call(ctx, http.MethodPost, endpoint, token, in, &out); err != nil {
		return "", fmt.Errorf("create pr: %w", err)
	}
	return out.HTMLURL, nil
}

// call issues one GitHub REST API request. in nil omits the body; out nil
// discards the response body beyond checking its status.
func (p *PRLander) call(ctx context.Context, method, endpoint, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github: %s %s: %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
