package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// scopesHandler answers the one GET the probe makes — a repo's
// .github/workflows contents — with a fixed status and X-OAuth-Scopes
// header, and fails the test if the probe asks about anything else.
func scopesHandler(t *testing.T, status int, scopesHeader string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/app/contents/.github/workflows" {
			t.Fatalf("probe hit unexpected path %s", r.URL.Path)
		}
		if scopesHeader != "" {
			w.Header().Set("X-OAuth-Scopes", scopesHeader)
		}
		w.WriteHeader(status)
	}
}

// newGHLandDaemon builds a Daemon whose one repo, shop/app, is configured
// for pull_request landing, then swaps in a github.com remote and points
// landProbe at ts — the injectable seam landtokenscopes.go adds, the same
// shape ghIssueLister already has.
//
// New(cfgPath) itself is built against a local bare remote (writeLandCfg's
// default), so the assemble it runs internally never touches ts or the
// network: a non-GitHub remote is probe-cannot-answer before either. That
// same first assemble also clones the mirror, which is what lets the
// github.com remote substituted afterward take Fetch's offline
// remote-update path instead of a real clone.
func newGHLandDaemon(t *testing.T, ts *httptest.Server) *Daemon {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "pull_request", "gh")

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	d.landProbe.apiBase = ts.URL
	d.landProbe.httpClient = ts.Client()
	d.landProbe.token = func(context.Context) (string, error) { return "tok", nil }
	d.cfg.Boxes[0].Repos[0].Remote = "https://github.com/acme/app.git"
	return d
}

// TestAssembleRefusesADefiniteWorkflowScopeGap is case (a): a classic OAuth
// token whose scopes are known and lack "workflow", probed against a repo
// that already has a .github/workflows directory — the one gap round 11's
// #68 landing found out about only at the remote's rejection. assemble must
// refuse with the fix verbatim, and the evidence record proving why must
// already be on the chain.
func TestAssembleRefusesADefiniteWorkflowScopeGap(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(scopesHandler(t, http.StatusOK, "repo,read:org"))
	defer ts.Close()
	d := newGHLandDaemon(t, ts)

	if _, _, _, _, err := d.assemble(ctx, d.cfg); err == nil {
		t.Fatal("land token missing the workflow scope, on a repo with a workflows directory, was accepted")
	} else if !strings.Contains(err.Error(), "gh auth refresh -s workflow") {
		t.Fatalf("error = %v, want it to contain the one-line fix verbatim", err)
	}

	rec, err := d.store.Log().Head(ctx, tenant.ID("acme"), "control/config")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Kind != "land-token-scopes" || rec.Subject != "shop/app" {
		t.Fatalf("head record = %+v, want a land-token-scopes record for shop/app", rec)
	}
	var got landTokenScopes
	if err := json.Unmarshal(rec.Body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Scopes != "repo,read:org" || !got.HasWorkflowsDir {
		t.Fatalf("recorded scopes = %+v, want the answered header verbatim with the workflows directory noted", got)
	}
}

// TestAssembleAcceptsAScopeListCarryingWorkflow is case (b): the same
// repo, but the token's scopes include "workflow" — no gap, so assemble
// must succeed and still record what it saw.
func TestAssembleAcceptsAScopeListCarryingWorkflow(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(scopesHandler(t, http.StatusOK, "repo,workflow,read:org"))
	defer ts.Close()
	d := newGHLandDaemon(t, ts)

	loops, _, _, _, err := d.assemble(ctx, d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := loops["shop/app"]
	if !ok {
		t.Fatal("no loop assembled for shop/app")
	}
	if lp.PRLander == nil {
		t.Fatal("land: pull_request configured, but Loop.PRLander is nil")
	}

	rec, err := d.store.Log().Head(ctx, tenant.ID("acme"), "control/config")
	if err != nil {
		t.Fatal(err)
	}
	var got landTokenScopes
	if err := json.Unmarshal(rec.Body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Scopes != "repo,workflow,read:org" || !got.HasWorkflowsDir {
		t.Fatalf("recorded scopes = %+v, want the answered header verbatim with the workflows directory noted", got)
	}
}

// TestAssembleAcceptsAMissingWorkflowsDirectory is case (c): the token's
// scopes lack "workflow", but the repo has no .github/workflows directory
// (404) for a pull-request landing to ever need to touch — not a gap this
// probe can see, so assemble must succeed.
func TestAssembleAcceptsAMissingWorkflowsDirectory(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(scopesHandler(t, http.StatusNotFound, "repo,read:org"))
	defer ts.Close()
	d := newGHLandDaemon(t, ts)

	if _, _, _, _, err := d.assemble(ctx, d.cfg); err != nil {
		t.Fatal(err)
	}

	rec, err := d.store.Log().Head(ctx, tenant.ID("acme"), "control/config")
	if err != nil {
		t.Fatal(err)
	}
	var got landTokenScopes
	if err := json.Unmarshal(rec.Body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Answered || got.Scopes != "repo,read:org" || got.HasWorkflowsDir {
		t.Fatalf("recorded scopes = %+v, want the answered header verbatim with no workflows directory", got)
	}
}

// roundTripFunc lets a test assert that no HTTP request was made at all,
// rather than merely stubbing one out.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestAssembleNonGitHubRemoteProbesNothing is case (d): the guard that
// keeps every existing land: pull_request test (pr_test.go, approval_test.go,
// attempts_test.go) offline. A non-GitHub remote must be probe-cannot-answer
// before landTokenScopeProbe ever fetches a token or opens a connection —
// ghOwnerRepo failing is what proves it never got that far.
func TestAssembleNonGitHubRemoteProbesNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	writeLandCfg(t, cfgPath, db, root, bare(t, "app"), "pull_request", "gh")

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	d.landProbe.token = func(context.Context) (string, error) {
		t.Fatal("land token fetched for a non-GitHub remote — probe must short-circuit before touching credentials")
		return "", nil
	}
	d.landProbe.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("network request made for a non-GitHub remote")
		return nil, nil
	})}

	if _, _, _, _, err := d.assemble(ctx, d.cfg); err != nil {
		t.Fatal(err)
	}
}
