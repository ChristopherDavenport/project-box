package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/pump"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

type world struct {
	srv  *Server
	pump *pump.Pump
	rem  string
}

type builder struct{}

func (builder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	os.WriteFile(filepath.Join(ws.Dir, "done.txt"), []byte(task+"\n"), 0o644)
	if _, err := gitforge.RevParse(context.Background(), ws.Dir, "HEAD"); err != nil {
		return "", err
	}
	if err := gitforge.CommitAll(context.Background(), ws.Dir, task); err != nil {
		return "", err
	}
	out, err := gitforge.RevParse(context.Background(), ws.Dir, "HEAD")
	return forge.Commit(out), err
}

func rig(t *testing.T) *world {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	remote := filepath.Join(t.TempDir(), "app.git")
	for _, c := range [][]string{
		{"init", "--bare", remote}, {"-C", remote, "symbolic-ref", "HEAD", "refs/heads/main"},
	} {
		if out, err := gitOut(t, "", c...); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	seed := filepath.Join(t.TempDir(), "seed")
	gitOut(t, "", "clone", remote, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("x\n"), 0o644)
	gitOut(t, seed, "add", ".")
	gitOut(t, seed, "commit", "-m", "seed")
	gitOut(t, seed, "push", "origin", "HEAD:refs/heads/main")

	repo := forge.Repo{Name: "app", Remote: remote, Role: forge.RoleService, DefaultBranch: "main"}
	m, err := gitforge.NewMirror(t.TempDir(), tn, s.Leaser("d"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Fetch(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: s, Runner: "d", TTL: time.Hour}
	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "d"}
	p := &pump.Pump{
		Store: s, Tenant: tn, Runner: "d", Queue: q,
		Meter: &capacity.Meter{Store: s, Tenant: tn, Actor: "d", Budget: capacity.Budget{MaxConcurrent: 2}},
		Loops: map[string]*loop.Loop{"shop": {
			Store: s, Runner: "d", Mirror: m, Repo: repo, Branch: "main",
			Builder: builder{}, Lane: forge.LaneGated,
			Gates: func(ws forge.Workspace) []forge.Gate {
				return []forge.Gate{gate.Command{Label: "ok", Dir: ws.Dir, Commands: [][]string{{"true"}}}}
			},
			TTL: time.Hour,
		}},
	}
	return &world{
		srv: &Server{
			Store: s, Tenant: tn, Queue: q, Router: router,
			Routes: map[string]ingress.Route{"issues": {
				Name: "issues", Kind: ingress.RouteIssue, Opens: ingress.KindIssue, Box: "shop", Requester: "daemon",
			}},
			Box: "shop", Runner: "d", Started: time.Now(),
		},
		pump: p, rem: remote,
	}
}

// TestDelegateOverTheWire: the whole client story — open work through the
// API, the daemon's tick does it, the client reads the outcome and the
// provenance. Exactly what a UX builds against, and exactly what a remote
// orchestrator calls in a cloud deployment.
func TestDelegateOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:"+sock, "")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })

	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	// Delegate: open an ingress over the wire.
	res, err := c.OpenIngress(ctx, "issues", "issue-9", "ship the widget")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	// Redelivery is idempotent over the wire too.
	res2, _ := c.OpenIngress(ctx, "issues", "issue-9", "ship the widget")
	if res2.Opened || res2.Item != res.Item {
		t.Fatalf("redelivery: %+v", res2)
	}
	// An unconfigured route is the caller asking for authority it does not
	// have: refused.
	if _, err := c.OpenIngress(ctx, "backdoor", "x", "y"); err == nil {
		t.Fatal("an unconfigured route was accepted")
	}

	// The API wrote intent; the daemon does the work.
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone {
		t.Fatalf("item over the wire: %+v %v", item, err)
	}
	// Provenance over the wire answers like the store does.
	prov, err := c.Provenance(ctx, item.Head)
	if err != nil || len(prov) == 0 {
		t.Fatalf("provenance: %d %v", len(prov), err)
	}
	// Status reflects reality.
	st, err := c.Status(ctx)
	if err != nil || st.Tenant != tn || st.Stopped {
		t.Fatalf("status: %+v %v", st, err)
	}
	// The stop button binds network callers and daemon alike.
	if err := c.Stop(ctx, "investigating"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "issues", "issue-10", "more work"); err != nil {
		t.Fatal(err) // intent is still writable...
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err) // ...but nothing claims
	}
	late, _ := c.Item(ctx, "wi-"+res2.Ingress.ID)
	_ = late
	item10 := ""
	for _, it := range mustActivity(t, c, ctx) {
		if it.Scope == "index/pending" && strings.Contains(string(it.Body), "more work") {
			item10 = it.Sub
		}
	}
	if item10 == "" {
		t.Fatal("second item not on the activity feed")
	}
	got, _ := c.Item(ctx, item10)
	if got.State != queue.StatePending {
		t.Fatalf("stopped daemon ran work: %+v", got)
	}
	if err := c.Resume(ctx, "clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Item(ctx, item10)
	if got.State != queue.StateDone {
		t.Fatalf("resume did not finish: %+v", got)
	}
	// A stop without a reason is refused: the reason is the record.
	if err := c.Stop(ctx, "  "); err == nil {
		t.Fatal("a reasonless stop was accepted")
	}
}

func mustActivity(t *testing.T, c *Client, ctx context.Context) []Activity {
	t.Helper()
	a, err := c.Activity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestTCPRequiresToken: the cloud posture — no token, no TCP listener; wrong
// token, 401; right token, service.
func TestTCPRequiresToken(t *testing.T) {
	if _, err := Listen("tcp:127.0.0.1:0", ""); err == nil {
		t.Fatal("an open TCP control plane was allowed")
	}
	w := rig(t)
	w.srv.Token = "s3cret"
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/status")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tokenless request: %v %v", resp.StatusCode, err)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("right token: %d", resp.StatusCode)
	}
}

// TestEventsStream: a subscriber sees new activity arrive, not a replay.
func TestEventsStream(t *testing.T) {
	w := rig(t)
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	// hello frame first
	for sc.Scan() && !strings.HasPrefix(sc.Text(), "event: hello") {
	}

	c, _ := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if _, err := c.OpenIngress(ctx, "issues", "issue-live", "streamed work"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "index/ingress") {
			return // the new ingress reached the subscriber
		}
		if time.Now().After(deadline) {
			break
		}
	}
	t.Fatal("the subscriber never saw the new ingress")
}

func gitOut(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := execCommand(dir, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
