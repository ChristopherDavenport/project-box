package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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
	ln, err := Listen("unix:" + sock)
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

// TestDaemonIsSocketOnly: the cloud posture inverted and strengthened — the
// daemon cannot be given a network listener at all; that is the gateway's
// job.
func TestDaemonIsSocketOnly(t *testing.T) {
	if _, err := Listen("tcp:127.0.0.1:0"); err == nil {
		t.Fatal("the daemon accepted a network listener")
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

// TestUIServingWithSPAFallback: real files serve as themselves; router deep
// links fall back to index.html; /v1 stays the API.
func TestUIServingWithSPAFallback(t *testing.T) {
	w := rig(t)
	w.srv.UI = fstest.MapFS{
		"index.html": {Data: []byte("<box-app></box-app>")},
		"app.js":     {Data: []byte("// bundle")},
	}
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	get := func(p string) (int, string) {
		res, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get("/app.js"); code != 200 || body != "// bundle" {
		t.Fatalf("asset: %d %q", code, body)
	}
	if code, body := get("/items/wi-42"); code != 200 || !strings.Contains(body, "box-app") {
		t.Fatalf("deep link should fall back to the shell: %d %q", code, body)
	}
	if code, _ := get("/v1/status"); code != 200 {
		t.Fatalf("the API must stay the API: %d", code)
	}
}

// TestQueueOverTheWire: the listing verb the surface never had (issue #20).
// `GET /v1/items/{id}` needs an id you could only know from a submit response
// you kept; this is the endpoint that answers "what is the factory sitting
// on" without having kept anything.
func TestQueueOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
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
	for _, key := range []string{"issue-20", "issue-21"} {
		// Distinct tasks: the rig's builder writes the task into one file, so
		// two identical tasks would leave the second tree unchanged.
		if _, err := c.OpenIngress(ctx, "issues", key, "make pending legible for "+key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}

	pending, err := c.Items(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d: %+v", len(pending), pending)
	}
	// Where a unit came from, which the id hashes past recovery.
	for _, e := range pending {
		if e.Route != "issues" {
			t.Fatalf("row does not name its route: %+v", e)
		}
		if e.Key != "issue-20" && e.Key != "issue-21" {
			t.Fatalf("row does not name its delivery key: %+v", e)
		}
		if e.Task == "" || e.Opened.IsZero() {
			t.Fatalf("row is missing task or clock: %+v", e)
		}
	}

	// Drain until they settle — one branch lease admits one run at a time,
	// so two units take two passes (issue #18) — and the filter follows them.
	for range 3 {
		if _, err := w.pump.Drain(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if still, err := c.Items(ctx, "pending"); err != nil || len(still) != 0 {
		t.Fatalf("settled work still listed as pending: %d %v", len(still), err)
	}
	done, err := c.Items(ctx, "done")
	if err != nil || len(done) != 2 {
		t.Fatalf("want 2 done, got %d: %v", len(done), err)
	}
	// Unfiltered is everything.
	all, err := c.Items(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered: %d %v", len(all), err)
	}
}

// TestRunInternalsReachTheStream: a claimed item used to be a black box. The
// index scopes say a unit exists and later that it landed; everything between
// — the agent starting, the commit, each gate verdict, an escalation and its
// reason — lives on the item's own chain, which nothing was tailing. Watching
// a run meant reloading a page.
func TestRunInternalsReachTheStream(t *testing.T) {
	w := rig(t)
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, _ := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if _, err := c.OpenIngress(ctx, "issues", "issue-inner", "watch me work"); err != nil {
		t.Fatal(err)
	}
	// Intake first, so the item exists before the subscriber connects and is
	// picked up as in-flight rather than only via index/pending.
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && !strings.HasPrefix(sc.Text(), "event: hello") {
	}

	go w.pump.Drain(ctx)

	// The run's own records, not merely the index's summary of it.
	want := map[string]bool{"\"kind\":\"claimed\"": false, "\"kind\":\"built\"": false, "\"kind\":\"gate\"": false}
	deadline := time.Now().Add(15 * time.Second)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "workitem/") {
			for k := range want {
				if strings.Contains(line, k) {
					want[k] = true
				}
			}
		}
		all := true
		for _, seen := range want {
			all = all && seen
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			break
		}
	}
	t.Fatalf("the subscriber never saw the run's internals: %v", want)
}

// TestExceptionQueueOverTheWire: the room's Availability control, on the
// surface a UX reads. `exceptions=2` was the whole story, and a count cannot
// distinguish a queue being worked from one nobody has opened in a week — so
// the age, the severity, and the objective the age is judged against all
// ride the same JSON.
func TestExceptionQueueOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	w.srv.Objective = time.Hour
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	for _, p := range []struct {
		id  string
		sev queue.Severity
	}{{"wi-routine", queue.SeverityMedium}, {"wi-bad", queue.SeverityCritical}} {
		item, err := q.Submit(ctx, queue.WorkItem{
			ID: p.id, Tenant: tn, Box: "shop", Ingress: "ing-1", Task: p.id,
		})
		if err != nil {
			t.Fatal(err)
		}
		item, lease, err := q.Claim(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Park(ctx, item, lease, p.sev, "parked for "+p.id); err != nil {
			t.Fatal(err)
		}
	}

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Exceptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 exceptions, got %d: %+v", len(got), got)
	}
	// Loudest first, and each row carries its own clock.
	if got[0].ID != "wi-bad" || got[0].Severity != queue.SeverityCritical {
		t.Fatalf("the wire does not order or label exceptions: %+v", got)
	}
	if got[0].Parked.IsZero() || got[0].Overdue {
		t.Fatalf("a fresh exception arrived with no clock or already late: %+v", got[0])
	}
	// The item is still there whole — the age is additional, not a swap.
	if got[0].Note != "parked for wi-bad" || got[0].Ingress != "ing-1" {
		t.Fatalf("the work item did not survive the richer row: %+v", got[0])
	}

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Exceptions != 2 || st.ExceptionQueue.Parked != 2 {
		t.Fatalf("the count and the metric disagree: %d vs %+v", st.Exceptions, st.ExceptionQueue)
	}
	if st.ExceptionQueue.ObjectiveSec != 3600 {
		t.Fatalf("status reports the metric without the objective it is judged against: %+v", st.ExceptionQueue)
	}
	if !st.ExceptionQueue.Met || st.ExceptionQueue.Oldest == "" {
		t.Fatalf("exception health: %+v", st.ExceptionQueue)
	}
}
