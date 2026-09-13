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
	"os"
	"strings"
	"time"

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
	// statusesOnly reports through the commit status API; see [PRLander.StatusesOnly].
	statusesOnly bool
}

// NewPRLander wraps inner to open a GitHub pull request at land time,
// immediately before the fenced push. token supplies the bearer credential
// per call — never a hardcoded secret.
// StatusesOnly makes the lander report verdicts as commit statuses rather
// than check runs. A check run can only be created by a GitHub App; an
// OAuth token — which is what `land_token: gh` is — gets "You must
// authenticate via a GitHub App" (403), and the first landing through the
// reviewer onto main parked on exactly that after both verdicts had passed.
// A commit status is the form an OAuth token can post: the same context
// names (box/check, box/review, box/land), the same conclusion, the
// summary as the description GitHub bounds at 140 characters. The evidence
// ids ride only the check-run form; the chain is the record either way.
// The daemon sets this for the gh token and leaves it off for an App.
func (p *PRLander) StatusesOnly() *PRLander {
	p.statusesOnly = true
	return p
}

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
	prURLs := make(map[string]string, len(cs.Members))
	for _, mem := range cs.Members {
		repo, ok := p.inner.m.Repo(mem.Repo)
		if !ok {
			return forge.Landing{}, fmt.Errorf("git: pr: repo %q not established by Fetch", mem.Repo)
		}
		prURL, err := p.EnsureRequest(ctx, cs, mem)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: open pr for %s: %w", mem.Repo, err)
		}
		prURLs[mem.Repo] = prURL

		owner, name, err := parseGitHubRemote(repo.Remote)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: %w", err)
		}
		tok, err := p.token(ctx)
		if err != nil {
			return forge.Landing{}, fmt.Errorf("git: pr: token: %w", err)
		}

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
			// box/check stays the roll-up over every gate, the reviewer
			// included. box/review carries that one gate's own verdict
			// besides, because issue #62 wants the judgement addressable on
			// its own check, not only folded into the roll-up's text. No
			// verdict names the reviewer in the optimistic lane above, and
			// also whenever the gate list this run used didn't include one
			// — absent stays absent rather than a box/review invented for a
			// change no reviewer read.
			if rv, ok := reviewVerdict(cs.Verdicts); ok {
				if err := p.postCheck(ctx, tok, owner, name, string(mem.Head), "box/review",
					checkConclusion(rv.Decision), fmt.Sprintf("review: %s", rv.Decision),
					checkSummary([]forge.Verdict{rv}), verdictText([]forge.Verdict{rv})); err != nil {
					return forge.Landing{}, fmt.Errorf("git: pr: review check for %s: %w", mem.Repo, err)
				}
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
		body := fmt.Sprintf(`{"pr_url":%q,"issue":%q}`, prURL, cs.DrivingIssue)
		if cs.DrivingIssue == "" {
			// An empty DrivingIssue is itself a finding, not a default: the
			// next reader must not have to diff this record against another
			// to notice the gap. cs.OriginKey may itself be empty — %q shows
			// "" and that emptiness is the finding too.
			body = fmt.Sprintf(`{"pr_url":%q,"issue":%q,"note":%q}`, prURL, cs.DrivingIssue,
				fmt.Sprintf("no driving issue: origin key %q", cs.OriginKey))
		}
		p.inner.record(ctx, cs, "pr-opened", string(pl.Now), body)
	}
	if err == nil && cs.DrivingIssue != "" && cs.CompletesPlan {
		p.closeDrivingIssue(ctx, cs, &landing, prURLs)
	}
	return landing, err
}

// closeDrivingIssue closes cs.DrivingIssue on GitHub once cs.CompletesPlan —
// the loop's own assertion that this landing settled the last unsettled unit
// of the issue's plan, never derived here — and only after inner.Land has
// already pushed the commit: the first member's Repo names the issue, the
// same convention [PRLander.prBody]'s "Fixes #<n>" relies on.
//
// Every failure here is reported to stderr and swallowed rather than
// propagated: the commit is already on the branch, so a landing must never
// fail over a comment or a close that GitHub refused. landing.Landed's
// IssueClosed is left false in every such case — set true only once GitHub
// has actually answered — so a caller (internal/loop) never records a close
// that did not happen.
func (p *PRLander) closeDrivingIssue(ctx context.Context, cs forge.ChangeSet, landing *forge.Landing, prURLs map[string]string) {
	mem := cs.Members[0]
	placementIdx := -1
	var sha string
	for i, pl := range landing.Landed {
		if pl.Repo == mem.Repo {
			placementIdx, sha = i, string(pl.Now)
			break
		}
	}
	if placementIdx == -1 {
		// The member naming the issue never reached its remote — a partial
		// landing stopped before it, which CompletesPlan cannot be true for.
		return
	}
	repo, ok := p.inner.m.Repo(mem.Repo)
	if !ok {
		fmt.Fprintf(os.Stderr, "git: close issue #%s: repo %q not established\n", cs.DrivingIssue, mem.Repo)
		return
	}
	owner, name, err := parseGitHubRemote(repo.Remote)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: close issue #%s: %v\n", cs.DrivingIssue, err)
		return
	}
	tok, err := p.token(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: close issue #%s: token: %v\n", cs.DrivingIssue, err)
		return
	}

	issueEndpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%s", p.apiBase, owner, name, cs.DrivingIssue)
	var issue struct {
		State string `json:"state"`
	}
	if err := p.call(ctx, http.MethodGet, issueEndpoint, tok, nil, &issue); err != nil {
		fmt.Fprintf(os.Stderr, "git: close issue #%s: get: %v\n", cs.DrivingIssue, err)
		return
	}

	commentsEndpoint := issueEndpoint + "/comments"
	var comments []struct {
		Body string `json:"body"`
	}
	if err := p.call(ctx, http.MethodGet, commentsEndpoint, tok, nil, &comments); err != nil {
		fmt.Fprintf(os.Stderr, "git: close issue #%s: list comments: %v\n", cs.DrivingIssue, err)
		return
	}
	alreadyCommented := false
	for _, c := range comments {
		if strings.Contains(c.Body, sha) {
			alreadyCommented = true
			break
		}
	}
	if !alreadyCommented {
		body := fmt.Sprintf("Landed as %s via %s.", sha, prURLs[mem.Repo])
		if ev := verdictText(cs.Verdicts); ev != "" {
			body += "\n\n" + ev
		}
		in := struct {
			Body string `json:"body"`
		}{Body: body}
		if err := p.call(ctx, http.MethodPost, commentsEndpoint, tok, in, nil); err != nil {
			fmt.Fprintf(os.Stderr, "git: close issue #%s: comment: %v\n", cs.DrivingIssue, err)
			return
		}
	}

	if issue.State == "open" {
		in := struct {
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
		}{State: "closed", StateReason: "completed"}
		if err := p.call(ctx, http.MethodPatch, issueEndpoint, tok, in, nil); err != nil {
			fmt.Fprintf(os.Stderr, "git: close issue #%s: patch: %v\n", cs.DrivingIssue, err)
			return
		}
	}
	landing.Landed[placementIdx].IssueClosed = true
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

// runBranchAtHead reports whether the run branch's tip on the remote already
// equals head, so [PRLander.EnsureRequest] can skip a push that would only
// re-push content already there. Any error (branch not created yet, remote
// unreachable) reads as "not at head" — the safe direction, since it only
// costs one push more than strictly necessary rather than skipping a push a
// moved head needed.
func (p *PRLander) runBranchAtHead(ctx context.Context, repo forge.Repo, branch string, head forge.Commit) bool {
	out, err := run(ctx, p.inner.m.mirrorDir(repo.Name), "ls-remote", repo.Remote, "refs/heads/"+branch)
	if err != nil {
		return false
	}
	fields := strings.Fields(out)
	return len(fields) > 0 && forge.Commit(fields[0]) == head
}

// workflowsPrefix is the path GitHub treats as workflow definitions: moving
// it requires the OAuth `workflow` scope, which GitHub itself enforces at
// the push, not before — round 11's #68 landing parked exactly there, after
// both verdicts had already passed. assertWorkflowScope asks the same
// question earlier, before any push happens, so the unit parks with the
// scope named and the fix in hand instead of on the remote's rejection.
//
// Phase 7's GitHub App replaces this assertion at this same seam with the
// App's installation-permission check (docs/PLAN.md Phase 7): the
// assertion stays: only its question changes, from "does this OAuth token
// carry workflow" to "does this installation grant workflows: write".
const workflowsPrefix = ".github/workflows/"

// assertWorkflowScope refuses before any push when mem's changeset touches
// workflowsPrefix and the token backing this landing cannot be shown to
// carry the `workflow` scope. It is a no-op for every other change: probing
// the token costs a real API call, and GitHub only ever rejects a push for
// this reason when the diff touches workflow files.
func (p *PRLander) assertWorkflowScope(ctx context.Context, cs forge.ChangeSet, mem forge.Member) error {
	ws := p.inner.m.workDir(cs.Run, mem.Repo)
	out, err := run(ctx, ws, "diff", "--name-only", string(mem.Base), string(mem.Head))
	if err != nil {
		return fmt.Errorf("diff %s..%s: %w", mem.Base, mem.Head, err)
	}
	touches := false
	for _, f := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(f), workflowsPrefix) {
			touches = true
			break
		}
	}
	if !touches {
		return nil
	}
	tok, err := p.token(ctx)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	scopes, present, err := p.tokenScopes(ctx, tok)
	if err != nil {
		return fmt.Errorf("check token scopes: %w", err)
	}
	if !present {
		return fmt.Errorf("git: this change touches %s but the token does not report OAuth scopes (a fine-grained PAT or GitHub App installation token) — the workflow scope cannot be asserted", workflowsPrefix)
	}
	for _, s := range scopes {
		if s == "workflow" {
			return nil
		}
	}
	return fmt.Errorf("git: this change touches %s but the token lacks the workflow scope (has: %s) — fix: gh auth refresh -s workflow", workflowsPrefix, strings.Join(scopes, ", "))
}

// tokenScopes asks GitHub what OAuth scopes tok carries. Any authenticated
// GitHub REST response carries the answer on the X-OAuth-Scopes header, so
// this asks the cheapest such endpoint rather than add a new one; present
// reports whether the header was sent at all, since a fine-grained PAT or a
// GitHub App installation token authenticates fine but never sends it, and
// that absence must read as neither "has every scope" nor "has none" — the
// caller decides what to do with unknown.
func (p *PRLander) tokenScopes(ctx context.Context, tok string) (scopes []string, present bool, err error) {
	endpoint := p.apiBase + "/rate_limit"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("github: GET %s: %s", endpoint, resp.Status)
	}
	values := resp.Header.Values("X-OAuth-Scopes")
	if values == nil {
		return nil, false, nil
	}
	raw := strings.TrimSpace(values[0])
	if raw == "" {
		return []string{}, true, nil
	}
	for _, s := range strings.Split(raw, ",") {
		scopes = append(scopes, strings.TrimSpace(s))
	}
	return scopes, true, nil
}

// EnsureRequest makes sure a pull request exists covering mem's head onto
// its branch — the push-and-open steps [PRLander.Land] performs before it
// lands, extracted so a gate that only wants a request to review can call
// this on every tick for as long as the review takes, without landing
// anything or posting a check.
//
// Idempotent both ways: ensurePR already reuses an open PR for the run
// branch rather than opening a second (forge.Gate's own idempotence
// requirement — internal/forge/gate.go), and the run branch itself is
// pushed only when its remote tip is not already mem.Head, so a rebuild's
// new head reaches the branch an approver is reading and an unmoved head
// costs no second push.
func (p *PRLander) EnsureRequest(ctx context.Context, cs forge.ChangeSet, mem forge.Member) (string, error) {
	branch := "box/" + cs.Run
	repo, ok := p.inner.m.Repo(mem.Repo)
	if !ok {
		return "", fmt.Errorf("repo %q not established by Fetch", mem.Repo)
	}
	if err := p.assertWorkflowScope(ctx, cs, mem); err != nil {
		return "", fmt.Errorf("git: ensure request: %w", err)
	}
	if !p.runBranchAtHead(ctx, repo, branch, mem.Head) {
		if _, err := p.pushRunBranch(ctx, cs, mem, branch); err != nil {
			return "", fmt.Errorf("git: ensure request: %w", err)
		}
	}
	owner, name, err := parseGitHubRemote(repo.Remote)
	if err != nil {
		return "", fmt.Errorf("git: ensure request: %w", err)
	}
	tok, err := p.token(ctx)
	if err != nil {
		return "", fmt.Errorf("git: ensure request: token: %w", err)
	}
	title, err := p.prTitle(ctx, cs)
	if err != nil {
		return "", fmt.Errorf("git: ensure request: title: %w", err)
	}
	prURL, err := p.ensurePR(ctx, tok, owner, name, branch, mem.Branch, title, p.prBody(cs))
	if err != nil {
		return "", fmt.Errorf("git: ensure request: %w", err)
	}
	return prURL, nil
}

// Review is one reviewer's decision on the pull request an [EnsureRequest]
// opened, read back from GitHub's own review state. Neutral on purpose: this
// package must not import internal/approval (a cross-section dependency the
// daemon is allowed to make and this package is not — see gate.go:30-32's
// read-only List contract), so the mapping from a Review to an
// approval.Approval, and the filter to only APPROVED ones, is the caller's
// job.
type Review struct {
	Login       string
	CommitID    string
	State       string
	ID          int64
	SubmittedAt time.Time
}

// Reviews reads back every review on the open pull request for mem's run
// branch — every state, not only APPROVED, so a caller can tell a dismissed
// or superseded review from one that never happened; silence must never
// read as approval, and a caller cannot honor that with a list that has
// already thrown states away. Empty, not an error, when there is no open
// pull request for the run branch yet.
func (p *PRLander) Reviews(ctx context.Context, cs forge.ChangeSet, mem forge.Member) ([]Review, error) {
	branch := "box/" + cs.Run
	repo, ok := p.inner.m.Repo(mem.Repo)
	if !ok {
		return nil, fmt.Errorf("repo %q not established by Fetch", mem.Repo)
	}
	owner, name, err := parseGitHubRemote(repo.Remote)
	if err != nil {
		return nil, fmt.Errorf("git: reviews: %w", err)
	}
	tok, err := p.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("git: reviews: token: %w", err)
	}
	pr, err := p.findOpenPR(ctx, tok, owner, name, branch)
	if err != nil {
		return nil, fmt.Errorf("git: reviews: %w", err)
	}
	if pr.Number == 0 {
		return nil, nil
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", p.apiBase, owner, name, pr.Number)
	var out []struct {
		ID   int64 `json:"id"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CommitID    string    `json:"commit_id"`
		State       string    `json:"state"`
		SubmittedAt time.Time `json:"submitted_at"`
	}
	if err := p.call(ctx, http.MethodGet, endpoint, tok, nil, &out); err != nil {
		return nil, fmt.Errorf("git: reviews: list: %w", err)
	}
	reviews := make([]Review, 0, len(out))
	for _, r := range out {
		reviews = append(reviews, Review{
			Login: r.User.Login, CommitID: r.CommitID, State: r.State,
			ID: r.ID, SubmittedAt: r.SubmittedAt,
		})
	}
	return reviews, nil
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
	if existing.URL != "" {
		return existing.URL, nil
	}
	return p.createPR(ctx, token, owner, repo, head, base, title, body)
}

// openPR is what findOpenPR reports for a head branch: the URL callers have
// used since #22, and the PR number [PRLander.Reviews] needs to address the
// reviews endpoint, which is keyed by number, not by head branch.
type openPR struct {
	URL    string
	Number int
}

func (p *PRLander) findOpenPR(ctx context.Context, token, owner, repo, head string) (openPR, error) {
	q := url.Values{}
	q.Set("head", owner+":"+head)
	q.Set("state", "open")
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", p.apiBase, owner, repo, q.Encode())
	var out []struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := p.call(ctx, http.MethodGet, endpoint, token, nil, &out); err != nil {
		return openPR{}, fmt.Errorf("list open prs: %w", err)
	}
	if len(out) == 0 {
		return openPR{}, nil
	}
	return openPR{URL: out[0].HTMLURL, Number: out[0].Number}, nil
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

// ReviewGateName is the [forge.Verdict.Gate] value the review gate posts —
// the literal string "review". internal/agent/acp defines the reviewer
// itself (its Name() returns this same string, there called ReviewerName),
// and internal/forge/git must not import internal/agent/acp, so the string
// is the seam instead — the same treatment forge.EvidenceKindRecord gets,
// and for the same reason (internal/forge/gate.go:51-58).
const ReviewGateName = "review"

// reviewVerdict finds the verdict the review gate posted among vs, if any.
// Not every gate list a run uses includes a reviewer, and the optimistic
// lane calls this with no verdicts at all, so absence is reported rather
// than papered over with a zero value that would read as a real verdict.
func reviewVerdict(vs []forge.Verdict) (forge.Verdict, bool) {
	for _, v := range vs {
		if v.Gate == ReviewGateName {
			return v, true
		}
	}
	return forge.Verdict{}, false
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
	if p.statusesOnly {
		st := statusInput{State: conclusion, Context: name, Description: statusDescription(title, summary)}
		endpoint := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", p.apiBase, owner, repo, sha)
		return p.call(ctx, http.MethodPost, endpoint, token, st, nil)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/check-runs", p.apiBase, owner, repo)
	return p.call(ctx, http.MethodPost, endpoint, token, in, nil)
}

// statusInput is the commit status API's create body. State is one of
// success, failure, error, pending — the check-run conclusions this lander
// posts are the first two.
type statusInput struct {
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description"`
}

// maxStatusDescription is GitHub's bound on a status description.
const maxStatusDescription = 140

// statusDescription folds a check run's title and summary into the one
// line a status carries, cut to GitHub's bound with the cut marked.
func statusDescription(title, summary string) string {
	d := title
	if summary != "" {
		d += " — " + summary
	}
	r := []rune(d)
	if len(r) <= maxStatusDescription {
		return d
	}
	return string(r[:maxStatusDescription-1]) + "…"
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
