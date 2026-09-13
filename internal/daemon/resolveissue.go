package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
)

// escalationCloser closes a GitHub issue tied to a resolved escalation — the
// same GET-issue/list-comments/dedupe/POST-comment/PATCH-close sequence
// git.PRLander's own closeDrivingIssue performs when a plan's last unit
// lands, duplicated here rather than exported across the section boundary
// between the daemon and the forge/git packages — see ghOwnerRepo's own
// comment for the precedent this follows. apiBase and httpClient are
// overridable so a test can stand an httptest.Server in, the same shape
// ghIssueLister and landTokenScopeProbe already use.
type escalationCloser struct {
	token gitforge.TokenSource

	apiBase    string
	httpClient *http.Client
}

func newEscalationCloser(token gitforge.TokenSource) *escalationCloser {
	return &escalationCloser{token: token, apiBase: "https://api.github.com", httpClient: http.DefaultClient}
}

// close closes issueNumber on remote (a github.com remote) idempotently: the
// comment posts only once — skipped when an existing comment already
// contains dedupeKey — and the issue is PATCHed closed only when it is still
// open. It reports whether the sequence completed with GitHub answering
// every call, true whether the issue was already closed or freshly closed
// by this call — never whether a close was newly made — so a caller that
// only records a close it can prove happened treats "already closed" the
// same as "just closed".
func (c *escalationCloser) close(ctx context.Context, remote, issueNumber, dedupeKey, comment string) (bool, error) {
	owner, repo, err := ghOwnerRepo(remote)
	if err != nil {
		return false, err
	}
	tok, err := c.token(ctx)
	if err != nil {
		return false, fmt.Errorf("daemon: close issue #%s: token: %w", issueNumber, err)
	}

	issueEndpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%s", c.apiBase, owner, repo, issueNumber)
	var issue struct {
		State string `json:"state"`
	}
	if err := c.call(ctx, http.MethodGet, issueEndpoint, tok, nil, &issue); err != nil {
		return false, fmt.Errorf("daemon: close issue #%s: get: %w", issueNumber, err)
	}

	commentsEndpoint := issueEndpoint + "/comments"
	var comments []struct {
		Body string `json:"body"`
	}
	if err := c.call(ctx, http.MethodGet, commentsEndpoint, tok, nil, &comments); err != nil {
		return false, fmt.Errorf("daemon: close issue #%s: list comments: %w", issueNumber, err)
	}
	already := false
	for _, cm := range comments {
		if strings.Contains(cm.Body, dedupeKey) {
			already = true
			break
		}
	}
	if !already {
		in := struct {
			Body string `json:"body"`
		}{Body: comment}
		if err := c.call(ctx, http.MethodPost, commentsEndpoint, tok, in, nil); err != nil {
			return false, fmt.Errorf("daemon: close issue #%s: comment: %w", issueNumber, err)
		}
	}

	if issue.State == "open" {
		in := struct {
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
		}{State: "closed", StateReason: "completed"}
		if err := c.call(ctx, http.MethodPatch, issueEndpoint, tok, in, nil); err != nil {
			return false, fmt.Errorf("daemon: close issue #%s: patch: %w", issueNumber, err)
		}
	}
	return true, nil
}

func (c *escalationCloser) call(ctx context.Context, method, endpoint, token string, in, out any) error {
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
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// resolveEscalatedIssue is api.Server's EscalationResolved hook: a person
// gave up on an origination's planning, and if that origination's delivery
// key names a GitHub issue (the "issue-<n>" convention internal/loop's own
// drivingIssue parses) and its box lands by pull request, the same issue
// closes here — the driving issue never gets closed by a plan that will now
// never land. reason is plan.Standing.Reason as api.Server read it just
// before Resolve overwrote the chain's newest standing — the plan-failed
// body; note is the resolver's own words. Both go into the one comment
// posted, deduped on a repeated call by the ingress id itself.
//
// Every failure here is reported to stderr and swallowed: Plans.Resolve has
// already succeeded by the time this runs, so a GitHub failure must not
// undo it, and — honestly — no issue-closed record is written for a close
// GitHub never answered. A resolve of an origination whose key is not
// issue-shaped, or whose box does not land by pull request, closes nothing.
func (d *Daemon) resolveEscalatedIssue(ctx context.Context, id, actor, reason, note string) {
	ing, ok, err := d.plans.Origination(ctx, d.tenant, id)
	if err != nil || !ok {
		return
	}
	issueNumber, ok := strings.CutPrefix(ing.Key, "issue-")
	if !ok {
		return
	}
	d.mu.RLock()
	remote, landsByPR := d.remotes[ing.Route], d.landPR[ing.Route]
	d.mu.RUnlock()
	if !landsByPR || remote == "" {
		return
	}

	comment := fmt.Sprintf("%s\n\nResolved as %s by %s: %s", reason, id, actor, note)
	closed, err := d.escalationCloser.close(ctx, remote, issueNumber, id, comment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: close issue #%s for resolved %s: %v\n", issueNumber, id, err)
		return
	}
	if !closed {
		return
	}
	body, _ := json.Marshal(struct {
		Issue  string `json:"issue"`
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}{Issue: issueNumber, Reason: reason, Note: note})
	if _, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: ingress.Scope(id), Kind: "issue-closed", Subject: id,
		Actor: actor, OnBehalfOf: ing.Requester, Body: body,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: record issue-closed for resolved %s: %v\n", id, err)
	}
}
