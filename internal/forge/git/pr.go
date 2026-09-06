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
// run branch `box/<run>`, base the member's branch, title the first member's
// Summary (falling back to the landed commit's subject line, and truncated
// to fit GitHub's 256-character limit — see [truncateTitle]), and a body
// naming DrivingIssue with "Fixes #<n>" when the change originated from one,
// plus the task's own first paragraph — then delegates to the wrapped Lander
// for the actual landing. A retried tick reuses the still-open PR for the
// same head branch rather than opening a duplicate.
func (p *PRLander) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if len(cs.Members) == 0 {
		return forge.Landing{}, errors.New("git: empty ChangeSet")
	}
	branch := "box/" + cs.Run
	title, err := p.prTitle(ctx, cs)
	if err != nil {
		return forge.Landing{}, fmt.Errorf("git: pr: title: %w", err)
	}
	body := p.prBody(cs)

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
		prURL, err := p.ensurePR(ctx, tok, owner, name, branch, mem.Branch, title, body)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: open pr for %s: %w", mem.Repo, err)
		}
		prURLs[mem.Repo] = prURL

		// The commit and PR now exist on GitHub, which the Checks API
		// requires. cs.Verdicts is empty in the optimistic lane, which lands
		// before its gates run — post nothing there rather than fabricate a
		// decision nobody made.
		if len(cs.Verdicts) > 0 {
			decision := forge.Resolve(cs.Verdicts)
			if err := p.postCheck(ctx, tok, owner, name, string(mem.Head), "box/check",
				checkConclusion(decision), fmt.Sprintf("gate: %s", decision),
				checkSummary(cs.Verdicts), verdictText(cs.Verdicts)); err != nil {
				return forge.Landing{}, fmt.Errorf("git: pr: check for %s: %w", mem.Repo, err)
			}
		}
	}

	landing, err := p.inner.Land(ctx, cs)
	if err != nil {
		// A force-with-lease rejection (git.go's "push refused" error) would
		// otherwise show up nowhere the PR's reader would see it, and the
		// last box/check would still read green. Best effort: a failure to
		// report the failure must never mask the failure itself, so its own
		// error is swallowed and the wrapped land error returns unchanged.
		p.postLandFailure(ctx, cs, landing, err)
	}
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

// prTitle computes the single title used for every per-member PR a ChangeSet
// opens (see [PRLander.Land]) — titling every PR from cs.Intent, the
// planner's whole task paragraph, is what produces GitHub's 422 "title is
// too long" this exists to avoid. It prefers the first member's Summary —
// the one-line room summary carried onto [forge.Member] — and falls back to
// the landed commit's own subject line when that member had none. cs.Intent
// itself is never truncated; only the copy used as a title is.
func (p *PRLander) prTitle(ctx context.Context, cs forge.ChangeSet) (string, error) {
	mem := cs.Members[0]
	if mem.Summary != "" {
		return truncateTitle(mem.Summary, 256), nil
	}
	ws := p.inner.m.workDir(cs.Run, mem.Repo)
	out, err := run(ctx, ws, "log", "-1", "--format=%s", string(mem.Head))
	if err != nil {
		return "", fmt.Errorf("commit subject for %s: %w", mem.Head, err)
	}
	return truncateTitle(strings.TrimSpace(out), 256), nil
}

// truncateTitle cuts s to at most max characters, preferring to cut at the
// last space at or before the limit so a title never ends mid-word; when no
// space falls within the limit it hard-cuts at max rather than leave the
// title over length.
func truncateTitle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, ' '); i >= 0 {
		return cut[:i]
	}
	return cut
}

// prBody composes the PR body: "Fixes #<n>" when the change originated from
// a GitHub issue (unaffected by this fix — a PR title and body are labels on
// the PR, never on the commit, and approvals bind to the commit per
// docs/PLAN.md §6, not the pull request), followed by the task's first
// paragraph (cs.Intent split on the first blank line), and — for a
// ChangeSet with more than one member, which production loop.go never
// builds today — a list of the other members' repos so the PR still says
// what else the changeset touches.
func (p *PRLander) prBody(cs forge.ChangeSet) string {
	var b strings.Builder
	if cs.DrivingIssue != "" {
		b.WriteString("Fixes #" + cs.DrivingIssue + "\n\n")
	}
	para := cs.Intent
	if i := strings.Index(cs.Intent, "\n\n"); i >= 0 {
		para = cs.Intent[:i]
	}
	b.WriteString(strings.TrimSpace(para))
	if len(cs.Members) > 1 {
		b.WriteString("\n\nAlso touches:\n")
		for _, m := range cs.Members[1:] {
			b.WriteString("- " + m.Repo + "\n")
		}
	}
	return b.String()
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

// postLandFailure reports the member that stopped the landing as a failing
// box/land check on its own head sha, naming landErr as the reason. It looks
// up that member itself rather than take one as a parameter because the
// caller only learns which member failed from landing.Failed.
func (p *PRLander) postLandFailure(ctx context.Context, cs forge.ChangeSet, landing forge.Landing, landErr error) {
	var mem forge.Member
	found := false
	for _, m := range cs.Members {
		if m.Repo == landing.Failed {
			mem = m
			found = true
			break
		}
	}
	if !found {
		return
	}
	repo, ok := p.inner.m.Repo(mem.Repo)
	if !ok {
		return
	}
	owner, name, err := parseGitHubRemote(repo.Remote)
	if err != nil {
		return
	}
	tok, err := p.token(ctx)
	if err != nil {
		return
	}
	_ = p.postCheck(ctx, tok, owner, name, string(mem.Head), "box/land",
		"failure", "land failed", landErr.Error(), "")
}

// checkConclusion maps a resolved gate decision onto the GitHub Checks API's
// vocabulary. cs.Verdicts only reaches this file once loop.go has already
// resolved it to DecisionPass, so this branch is exercised by direct callers
// of PRLander (tests, and any future caller) rather than the loop today.
func checkConclusion(d forge.Decision) string {
	if d == forge.DecisionPass {
		return "success"
	}
	return "failure"
}

// checkSummary mirrors internal/loop's reasons(vs []forge.Verdict) string —
// same semicolon-joined, first-line-only formatting — rather than reinventing
// the joining logic. It differs in one way: that function only names
// failing gates, because it is only ever called on a ChangeSet that has
// already failed. A box/check summary has to describe a *passing* verdict
// set just as clearly, so this reports every verdict, not only the failures.
func checkSummary(vs []forge.Verdict) string {
	out := ""
	for _, v := range vs {
		if out != "" {
			out += "; "
		}
		line := v.Reason
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		out += fmt.Sprintf("%s: %s (%s)", v.Gate, v.Decision, line)
	}
	if out == "" {
		out = "no gate examined the change"
	}
	return out
}

// verdictText names the evidence-chain record backing each verdict — not
// every [forge.EvidenceRef] a gate attached (a gate.Command's own transcript
// reference is a local filesystem path, meaningless to a reader on GitHub) —
// so an auditor can walk from the check straight back to the chain that
// produced it.
func verdictText(vs []forge.Verdict) string {
	var ids []string
	for _, v := range vs {
		for _, e := range v.Evidence {
			if e.Kind == forge.EvidenceKindRecord && e.URI != "" {
				ids = append(ids, e.URI)
			}
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "evidence: " + strings.Join(ids, ", ")
}

type checkRunInput struct {
	Name       string         `json:"name"`
	HeadSHA    string         `json:"head_sha"`
	Status     string         `json:"status"`
	Conclusion string         `json:"conclusion"`
	Output     checkRunOutput `json:"output"`
}

type checkRunOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text,omitempty"`
}

// postCheck reports one GitHub check run — box/check for the gate verdict,
// box/land for a fencing rejection — on the commit sha the run is about,
// reusing [PRLander.call] so it is exercised against the same
// httptest.Server as PR creation.
func (p *PRLander) postCheck(ctx context.Context, token, owner, repo, sha, name, conclusion, title, summary, text string) error {
	in := checkRunInput{
		Name: name, HeadSHA: sha, Status: "completed", Conclusion: conclusion,
		Output: checkRunOutput{Title: title, Summary: summary, Text: text},
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/check-runs", p.apiBase, owner, repo)
	return p.call(ctx, http.MethodPost, endpoint, token, in, nil)
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
