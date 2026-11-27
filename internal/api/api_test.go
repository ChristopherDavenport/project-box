package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/control"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/pump"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

const tn = tenant.ID("acme")

type world struct {
	srv  *Server
	pump *pump.Pump
	rem  string
}

// queueDump renders every row the daemon holds, one per line, with the NOTE.
//
// It exists because the assertions below count, and a count is the one thing
// that cannot say why it is wrong. Both of the tests that use it have failed
// in CI on a bare number — "want 2 done, got 1", and "settled: pending=0
// map[done:2 failed:1]" — which says an item failed instead of landing and
// nothing else. Not which item, not at which stage, and above all not why.
// Neither run was reproducible afterwards, so the arithmetic was the whole
// record of an event that has now happened twice.
//
// The reason was on the row the entire time. `QueueEntry.Note` is "why the
// item is in this state — a gate's reasons, a park cause", and its own doc
// says the queue has always written it and nothing has ever shown it. That
// sentence has been earned twice in this package already; a test is the last
// place it should still be true.
//
// It never fails the test itself. This runs on a path where the test is
// already failing, so a diagnostic that could panic or abort would replace
// the weak message with no message at all.
func queueDump(t *testing.T, c *Client) string {
	t.Helper()
	rows, err := c.Items(context.Background())
	if err != nil {
		return fmt.Sprintf("\n\t(the queue could not be read for diagnosis: %v)", err)
	}
	if len(rows) == 0 {
		return "\n\t(the queue is empty)"
	}
	var b strings.Builder
	for _, e := range rows {
		fmt.Fprintf(&b, "\n\t%s state=%s stage=%s", e.ID, e.State, orNone(e.Stage))
		if e.Head != "" {
			fmt.Fprintf(&b, " head=%s", short(e.Head))
		}
		if e.Built != "" {
			fmt.Fprintf(&b, " built=%s", short(e.Built))
		}
		if e.Runner != "" {
			fmt.Fprintf(&b, " runner=%s lease=%ds", e.Runner, e.LeaseExpiresSec)
		}
		// Last and unabbreviated: it is the only field here that is prose,
		// and the only one that has ever been the answer.
		fmt.Fprintf(&b, "\n\t    note: %s", orNone(e.Note))
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

type builder struct{}

func (builder) Build(_ context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	os.WriteFile(filepath.Join(ws.Dir, "done.txt"), []byte(task+"\n"), 0o644)
	if _, err := gitforge.RevParse(context.Background(), ws.Dir, "HEAD"); err != nil {
		return "", err
	}
	if err := gitforge.CommitAll(context.Background(), ws.Dir, forge.Identity{}, "build: "+task); err != nil {
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
		Kinds: kind.Seeded("shop"), Decomposers: plan.Default(),
		Plans: &plan.Store{Log: s.Log(), Actor: "d"},
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
				Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "daemon",
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
	hs := &http.Server{Handler: w.srv.Handler(), ConnContext: ConnContext}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })

	// No X-Box-Actor claim: the peer this socket asserts is the whole
	// identity, exactly what mustActor requires.
	c, err := Dial("unix:"+sock, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Delegate: open an ingress over the wire.
	res, err := c.OpenIngress(ctx, "issues", "issue-9", "ship the widget", "", "")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	// Redelivery is idempotent over the wire too.
	res2, _ := c.OpenIngress(ctx, "issues", "issue-9", "ship the widget", "", "")
	if res2.Opened || res2.Item != res.Item {
		t.Fatalf("redelivery: %+v", res2)
	}
	// An unconfigured route is the caller asking for authority it does not
	// have: refused.
	if _, err := c.OpenIngress(ctx, "backdoor", "x", "y", "", ""); err == nil {
		t.Fatal("an unconfigured route was accepted")
	}

	// The API wrote intent; the daemon does the work.
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the tick's dispatched run")
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
	// And the stop is a fact with a name on it, not a bit: the room shows
	// who pressed it and why without a trip through the activity feed. The
	// name is the peer SO_PEERCRED asserted for this process's own socket
	// connection, never a claim — there is none on this client.
	wantActor := accountName(uint32(os.Getuid()))
	st, err = c.Status(ctx)
	if err != nil || !st.Stopped || !st.Claims.Stopped || st.Claims.Reason != "investigating" || st.Claims.Actor != wantActor || st.Claims.At.IsZero() {
		t.Fatalf("status after stop should carry the record: %+v %v", st.Claims, err)
	}
	if _, err := c.OpenIngress(ctx, "issues", "issue-10", "more work", "", ""); err != nil {
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
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the resumed tick's dispatched run")
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

// TestSubmitCarriesASummary confirms an origination gets a one-line name
// distinct from its task paragraph when a submitter supplies one, and that
// omitting it still opens exactly as before — no regression for callers who
// never knew the field existed.
func TestSubmitCarriesASummary(t *testing.T) {
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

	res, err := c.OpenIngress(ctx, "issues", "issue-summary", "a whole paragraph of task text nobody wants as a room name", "fix the widget", "")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	if res.Ingress.Title != "fix the widget" {
		t.Fatalf("title: got %q, want the supplied summary", res.Ingress.Title)
	}

	// Omitting the summary is not a regression: no summary means no title,
	// the same as before this field existed.
	res2, err := c.OpenIngress(ctx, "issues", "issue-no-summary", "another paragraph", "", "")
	if err != nil || !res2.Opened {
		t.Fatalf("open without summary: %+v %v", res2, err)
	}
	if res2.Ingress.Title != "" {
		t.Fatalf("title: got %q, want empty when no summary was supplied", res2.Ingress.Title)
	}
}

// TestSubmitCarriesAPriority is issue #115 part 4: a priority named on
// submit lands on the ingress and survives intake onto the item itself,
// exactly like [ingress.Delivery.Priority] already proves one layer down.
func TestSubmitCarriesAPriority(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	c := dialUnix(t, w, "")

	res, err := c.OpenIngress(ctx, "issues", "issue-urgent", "ship it now", "", "urgent")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	if res.Ingress.Priority != queue.PriorityUrgent {
		t.Fatalf("ingress priority = %q, want urgent", res.Ingress.Priority)
	}

	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the tick's dispatched run")
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone {
		t.Fatalf("item over the wire: %+v %v", item, err)
	}
	if item.Priority != queue.PriorityUrgent {
		t.Fatalf("item priority = %q, want urgent", item.Priority)
	}
}

// TestSubmitRefusesAnUnrecognisedPriority: a typo'd priority is the
// caller's error, said plainly as a 400 naming the four levels — not the
// blanket 500 [ingress.Router.Open] would otherwise produce.
func TestSubmitRefusesAnUnrecognisedPriority(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	c := dialUnix(t, w, "")

	_, err := c.OpenIngress(ctx, "issues", "issue-bad-priority", "ship it", "", "urgentish")
	if err == nil {
		t.Fatal("an unrecognised priority was accepted")
	}
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want a 400", err)
	}
	if !strings.Contains(apiErr.Message, "low") || !strings.Contains(apiErr.Message, "urgent") {
		t.Fatalf("message = %q, want the four levels named", apiErr.Message)
	}
}

// TestQueueOrdersByPriorityThenAge is issue #115 part 4: the listing must
// stay true to its own doc — the order the factory would actually take
// these items — which [Room.Queue] achieves the same way the pump's own
// dispatch sort does: priority rank descending, then age.
func TestQueueOrdersByPriorityThenAge(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	c := dialUnix(t, w, "")

	if _, err := c.OpenIngress(ctx, "issues", "issue-older-normal", "older normal work", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "issues", "issue-younger-urgent", "younger urgent work", "", "urgent"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "issues", "issue-youngest-normal", "youngest normal work", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}

	entries, err := c.Items(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range entries {
		order = append(order, e.Task)
	}
	want := []string{"younger urgent work", "older normal work", "youngest normal work"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
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
	if _, err := c.OpenIngress(ctx, "issues", "issue-live", "streamed work", "", ""); err != nil {
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
		if _, err := c.OpenIngress(ctx, "issues", key, "make pending legible for "+key, "", ""); err != nil {
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
		t.Fatalf("want 2 pending, got %d:%s", len(pending), queueDump(t, c))
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
		// The frontier rides the same row. Nothing depends on these two, and
		// nobody has claimed them, so both are work the factory could be doing
		// right now — which is the fact the state "pending" cannot carry and
		// the reason an operator can tell a stalled factory from an empty one.
		if !e.Ready {
			t.Fatalf("unblocked unclaimed work must read as ready over the wire: %+v", e)
		}
	}

	// Drain until they settle — one branch lease admits one run at a time,
	// so two units take two passes (issue #18) — and the filter follows them.
	// Each pass is quiesced before the next: Drain only dispatches and
	// returns (issue #196), and without settling first, a second pass could
	// dispatch the other unit while the first was still mid-land, racing both
	// for the same branch lease instead of taking the two clean passes this
	// test is about.
	for range 3 {
		if _, err := w.pump.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		if !w.pump.Quiesce(ctx) {
			t.Fatal("quiesce did not settle the pass's dispatched run")
		}
	}
	if still, err := c.Items(ctx, "pending"); err != nil || len(still) != 0 {
		t.Fatalf("settled work still listed as pending: %d %v:%s", len(still), err, queueDump(t, c))
	}
	done, err := c.Items(ctx, "done")
	if err != nil || len(done) != 2 {
		t.Fatalf("want 2 done, got %d %v:%s", len(done), err, queueDump(t, c))
	}
	// Unfiltered is everything.
	all, err := c.Items(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered: %d %v", len(all), err)
	}
}

// TestTheCountAndTheListingAreOneFold is the other half of issue #20, and the
// half the counter itself was the complaint about: /v1/status's numbers are a
// tally of /v1/items rather than a second walk arriving at their own.
//
// The two used to be separate readings of one queue — the listing folding each
// chain, the count walking index/pending with its own copy of the
// not-yet-settled vocabulary. They agreed because two switch statements
// happened to match, which is agreement with nothing holding it, and the state
// vocabulary in this system has already drifted that way once.
func TestTheCountAndTheListingAreOneFold(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	ts := httptest.NewServer(w.srv.Handler())
	defer ts.Close()
	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"issue-20a", "issue-20b", "issue-20c"} {
		if _, err := c.OpenIngress(ctx, "issues", key, "make pending legible for "+key, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.pump.Intake(ctx); err != nil {
		t.Fatal(err)
	}

	// agrees holds the counter to the rows at every point the queue moves
	// through, not only at the start: a count that matches when everything is
	// pending and drifts once work settles is the bug this closes.
	agrees := func(when string) Status {
		t.Helper()
		st, err := c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		all, err := c.Items(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Backlog.Total != len(all) {
			t.Fatalf("%s: status says %d in the queue, the listing returns %d",
				when, st.Backlog.Total, len(all))
		}
		if st.Pending != st.Backlog.Pending {
			t.Fatalf("%s: the flat count %d and the fold's %d disagree", when, st.Pending, st.Backlog.Pending)
		}
		// State by state, against the listing narrowed to that state — the
		// endpoint a client would use to derive the number itself.
		sum, owed := 0, 0
		for state, n := range st.Backlog.ByState {
			rows, err := c.Items(ctx, string(state))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != n {
				t.Errorf("%s: status counts %d %s, GET /v1/items?state=%s returns %d",
					when, n, state, state, len(rows))
			}
			sum += n
			if queue.Claimable(state) {
				owed += n
			}
		}
		if sum != st.Backlog.Total {
			t.Fatalf("%s: the parts sum to %d, the total says %d", when, sum, st.Backlog.Total)
		}
		// One vocabulary: what the counter calls pending is what the drain
		// will take, read from the same function the pump selects with.
		if st.Pending != owed {
			t.Fatalf("%s: pending=%d, but %d rows are in states the drain would claim: %+v",
				when, st.Pending, owed, st.Backlog.ByState)
		}
		return st
	}

	st := agrees("after intake")
	if st.Pending != 3 || st.Backlog.ByState[queue.StatePending] != 3 {
		t.Fatalf("three submitted, status says %d: %+v%s", st.Pending, st.Backlog.ByState, queueDump(t, c))
	}

	// Mid-flight, where the two accounts had the most room to come apart: one
	// branch lease admits one run at a time (issue #18), so a drain leaves a
	// mixture rather than a clean sweep. Drain itself only dispatches and
	// returns (issue #196) rather than waiting for the run it just started, so
	// the pass is quiesced before agrees reads anything — agrees is not one
	// read, it is a status call compared against several Items calls, and
	// without settling first those calls would be watching a queue still
	// moving underneath them.
	if _, err := w.pump.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the pass's dispatched run")
	}
	agrees("mid-drain")

	// Quiesced between passes for the same reason as above: an un-settled
	// pass here would race the next pass's own dispatch for the same branch
	// lease instead of taking the clean, one-run-per-pass sweep to done.
	for range 4 {
		if _, err := w.pump.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		if !w.pump.Quiesce(ctx) {
			t.Fatal("quiesce did not settle the pass's dispatched run")
		}
	}
	st = agrees("settled")
	if st.Pending != 0 || st.Backlog.ByState[queue.StateDone] != 3 {
		t.Fatalf("settled: pending=%d %+v%s", st.Pending, st.Backlog.ByState, queueDump(t, c))
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
	if _, err := c.OpenIngress(ctx, "issues", "issue-inner", "watch me work", "", ""); err != nil {
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

// TestFlakesOverTheWire: GET /v1/flakes serves [control.Room.Flakes] —
// registered beside GET /v1/exceptions — grouped by subject with its count
// and the latest proof's detail, same as the direct-store fold tested in
// internal/control.
func TestFlakesOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	subject := "acme/repo/pkg/TestFlipFlop"
	body := []byte(`{"test":"TestFlipFlop","package":"pkg","repo":"acme/repo","refused_head":"h1","passed_head":"h2","refused_record":"r1","passed_record":"r2","work_item":"wi-1"}`)
	for range 2 {
		if _, err := w.srv.Store.Log().Append(ctx, evidence.Record{
			Tenant: tn, Scope: "index/flakes", Kind: "flaky",
			Subject: subject, Actor: "runner-1", Body: body,
		}); err != nil {
			t.Fatal(err)
		}
	}

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Flakes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 grouped flake, got %d: %+v", len(got), got)
	}
	if got[0].Subject != subject || got[0].Count != 2 {
		t.Fatalf("the wire does not group by subject or count occurrences: %+v", got[0])
	}
	if got[0].RefusedHead != "h1" || got[0].PassedHead != "h2" || got[0].WorkItem != "wi-1" {
		t.Fatalf("the latest proof's detail did not survive the wire: %+v", got[0])
	}
}

// TestSectionsOverTheWire: GET /v1/sections serves [control.Room.Sections]
// per repo, keyed from s.Sections — the declared names an operator wrote
// into box.yaml, wired at daemon assemble (issue #200) — so a section with
// no landings yet still appears, at zero, same as the direct-store fold
// tested in internal/control.
func TestSectionsOverTheWire(t *testing.T) {
	w := rig(t)
	w.srv.Sections = map[string][]string{"app": {"alpha", "beta"}}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	item, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t",
		Repo: "app", Sections: []string{"alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := q.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	item.LandedSections, item.LandedPaths = []string{"alpha"}, []string{"alpha/x.go"}
	if _, err := q.Done(ctx, item, lease, forge.Commit("sha-1")); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(ts.URL + "/v1/sections")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /v1/sections: status %d", res.StatusCode)
	}
	var out map[string]control.SectionCensus
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	census, ok := out["app"]
	if !ok {
		t.Fatalf("no census for repo app: %+v", out)
	}
	byName := map[string]control.SectionStat{}
	for _, s := range census.Sections {
		byName[s.Name] = s
	}
	if byName["alpha"].Landed != 1 {
		t.Fatalf("alpha: want 1 landing, got %+v", byName["alpha"])
	}
	beta, ok := byName["beta"]
	if !ok || beta.Landed != 0 {
		t.Fatalf("beta: declared with no landings yet, want zero not absent: %+v", byName)
	}
}

// TestLandingsOverTheWire is issue #183 part 2's happy path: GET
// /v1/landings lists two settled items newest first, each carrying its own
// [queue.LandingCost], and the per-landing fold — both on the listing
// itself and on /v1/status's spend block — sums only the one with a known
// cost and counts the other in Unknown rather than silently reading it as
// free.
func TestLandingsOverTheWire(t *testing.T) {
	w := rig(t)
	w.srv.Meter = &capacity.Meter{Budget: capacity.Budget{Window: time.Hour}}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)
	ctx := context.Background()

	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}

	item1, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "close issue one", Repo: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	item1, lease1, err := q.Claim(ctx, item1)
	if err != nil {
		t.Fatal(err)
	}
	item1.Attempt = 2
	item1.Cost = &queue.LandingCost{CostMicros: 3500, Tokens: 800, Runs: 3, Reviews: 1, Known: true}
	if _, err := q.Done(ctx, item1, lease1, forge.Commit("sha-1")); err != nil {
		t.Fatal(err)
	}

	item2, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-2", Tenant: tn, Box: "shop", Ingress: "ing-2", Task: "close issue two", Repo: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	item2, lease2, err := q.Claim(ctx, item2)
	if err != nil {
		t.Fatal(err)
	}
	item2.Cost = &queue.LandingCost{Runs: 1, Unmetered: 1, Known: false}
	if _, err := q.Done(ctx, item2, lease2, forge.Commit("sha-2")); err != nil {
		t.Fatal(err)
	}

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	page, err := c.Landings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Landings) != 2 {
		t.Fatalf("want 2 landings, got %d: %+v", len(page.Landings), page.Landings)
	}
	if page.Landings[0].ID != "wi-2" || page.Landings[1].ID != "wi-1" {
		t.Fatalf("want newest first (wi-2, wi-1), got %+v", page.Landings)
	}
	first := page.Landings[1]
	if first.Head != "sha-1" || first.Attempt != 2 {
		t.Fatalf("wi-1: head/attempt did not survive the wire: %+v", first)
	}
	if first.Cost == nil || !first.Cost.Known || first.Cost.CostMicros != 3500 ||
		first.Cost.Tokens != 800 || first.Cost.Reviews != 1 {
		t.Fatalf("wi-1: cost did not survive the wire: %+v", first.Cost)
	}
	second := page.Landings[0]
	if second.Cost == nil || second.Cost.Known {
		t.Fatalf("wi-2: cost should read unknown: %+v", second.Cost)
	}

	for _, pl := range []struct {
		name string
		got  *control.PerLanding
	}{{"since_boot", &page.SinceBoot}, {"window", page.Window}} {
		if pl.got == nil {
			t.Fatalf("%s: want a per-landing fold, got none", pl.name)
		}
		if pl.got.Count != 2 || pl.got.Unknown != 1 || pl.got.CostMicros != 3500 {
			t.Fatalf("%s: fold = %+v, want count 2 unknown 1 cost_micros 3500", pl.name, pl.got)
		}
	}

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Spend == nil || st.Spend.PerLanding == nil {
		t.Fatalf("status: want spend.per_landing, got %+v", st.Spend)
	}
	if got := st.Spend.PerLanding; got.Count != 2 || got.Unknown != 1 || got.CostMicros != 3500 {
		t.Fatalf("status: per_landing = %+v, want count 2 unknown 1 cost_micros 3500", got)
	}
	// The embedding must not rename anything. Decoding through [Status] and
	// [SpendReport] again would only prove the two sides of this same test
	// agree with themselves — a tag added to the embedded field (nesting
	// every [capacity.Report] key under spend.report) round-trips through
	// that struct just as cleanly as today's flat shape does. The property
	// this guards is the wire's, so it has to be read off the wire: fetch
	// /v1/status raw and assert the keys [capacity.Report] has always
	// reported are still top-level members of the spend object, beside
	// per_landing rather than nesting it away from them.
	res, err := http.Get(ts.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var raw struct {
		Spend map[string]json.RawMessage `json:"spend"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"tokens", "cost_micros", "max_per_window", "window_sec",
		"refusing", "running_builds", "per_landing",
	} {
		if _, ok := raw.Spend[key]; !ok {
			t.Fatalf("status: spend.%s missing from the wire; keys are %v", key, slices.Sorted(maps.Keys(raw.Spend)))
		}
	}
}

// TestLandingsOmitsPerLandingWindowWithNoWindowConfigured: a meter with no
// Window set has no window to project against, and the fold must be absent
// on both endpoints — never a {0,0,0} that would misread as "an empty
// window" rather than "no window at all" (docs/PLAN.md's unknown-never-
// reads-as-zero rule, applied to a fold rather than a money figure).
func TestLandingsOmitsPerLandingWindowWithNoWindowConfigured(t *testing.T) {
	w := rig(t)
	w.srv.Meter = &capacity.Meter{}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)
	ctx := context.Background()

	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	item, err := q.Submit(ctx, queue.WorkItem{ID: "wi-1", Tenant: tn, Box: "shop", Ingress: "ing-1", Task: "t", Repo: "app"})
	if err != nil {
		t.Fatal(err)
	}
	item, lease, err := q.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	item.Cost = &queue.LandingCost{CostMicros: 100, Runs: 1, Known: true}
	if _, err := q.Done(ctx, item, lease, forge.Commit("sha-1")); err != nil {
		t.Fatal(err)
	}

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	page, err := c.Landings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if page.Window != nil {
		t.Fatalf("no window configured: want window fold absent, got %+v", page.Window)
	}
	if page.SinceBoot.Count != 1 {
		t.Fatalf("since_boot should still fold with no window configured: %+v", page.SinceBoot)
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Spend == nil {
		t.Fatal("status: want spend present with a meter wired")
	}
	if st.Spend.PerLanding != nil {
		t.Fatalf("no window configured: want spend.per_landing absent, got %+v", st.Spend.PerLanding)
	}
}

// statusOnly dials a bare Server over a unix socket — every test below only
// cares about /v1/status, not a working queue.
func statusOnly(t *testing.T, srv *Server) *Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: srv.Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestStatusReportsAKnownBuild: a Build seam that reports a known commit
// surfaces on /v1/status verbatim, dirty bit included.
func TestStatusReportsAKnownBuild(t *testing.T) {
	w := rig(t)
	w.srv.Build = func() version.Build { return version.Build{Commit: "deadbeefcafe", Dirty: true, Known: true} }
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BuildCommit != "deadbeefcafe" || !st.BuildDirty {
		t.Fatalf("status = %+v, want commit deadbeefcafe dirty true", st)
	}
}

// TestStatusOmitsAnUnknownBuild: unknown never reads as zero. With no Build
// seam wired, the handler falls back to version.ThisBuild directly — which a
// `go test` binary (never stamped with vcs.revision the way `go build` is)
// reports as unknown — and the fields must be absent, not an empty commit
// beside a false dirty bit that would read as "clean".
func TestStatusOmitsAnUnknownBuild(t *testing.T) {
	w := rig(t)
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.BuildCommit != "" || st.BuildDirty {
		t.Fatalf("status = %+v, want build fields absent for an unknown build", st)
	}
}

// TestStatusReportsTheFollowBranchSeam: when the daemon wires FollowBranch,
// /v1/status carries the branch and the commit it resolved to.
func TestStatusReportsTheFollowBranchSeam(t *testing.T) {
	w := rig(t)
	w.srv.FollowBranch = func(context.Context) (string, string, error) {
		return "main", "cafefeed0000", nil
	}
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.FollowBranch != "main" || st.FollowHead != "cafefeed0000" {
		t.Fatalf("status = %+v, want follow_branch main / follow_head cafefeed0000", st)
	}
}

// TestStatusOmitsFollowBranchWhenUnwired: no self_deploy configured means no
// FollowBranch seam, which must read as the fields simply being absent —
// the nil-means-unwired shape every other optional Server dependency uses.
func TestStatusOmitsFollowBranchWhenUnwired(t *testing.T) {
	w := rig(t)
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.FollowBranch != "" || st.FollowHead != "" {
		t.Fatalf("status = %+v, want follow branch fields absent", st)
	}
}

// TestStatusFailsWhenFollowBranchCannotResolve: self_deploy configured but
// the branch not resolvable (never fetched, say) is a real problem, not a
// quiet omission that would look identical to self_deploy never having been
// turned on — it must fail the request the same way a Backlog or
// ExceptionHealth failure already does.
func TestStatusFailsWhenFollowBranchCannotResolve(t *testing.T) {
	w := rig(t)
	w.srv.FollowBranch = func(context.Context) (string, string, error) {
		return "", "", fmt.Errorf("resolving main: unknown revision")
	}
	if _, err := statusOnly(t, w.srv).Status(context.Background()); err == nil {
		t.Fatal("status should have failed when the followed branch could not resolve")
	}
}

// TestStatusReportsTheLastPassSeam: when the daemon wires LastPass and a
// pass has run, /v1/status carries when it ran, what it decided, and why
// (issue #152).
func TestStatusReportsTheLastPassSeam(t *testing.T) {
	w := rig(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	w.srv.LastPass = func() (time.Time, string, string, bool) {
		return at, "current", "already running what the branch names", true
	}
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastPassAt != at.Format(time.RFC3339) {
		t.Fatalf("last_pass_at = %q, want %q", st.LastPassAt, at.Format(time.RFC3339))
	}
	if st.LastPassDecision != "current" || st.LastPassReason != "already running what the branch names" {
		t.Fatalf("status = %+v, want decision/reason to survive the wire", st)
	}
}

// TestStatusOmitsLastPassWhenUnwired: no self_deploy configured means no
// LastPass seam, which must read as the fields simply being absent — the
// nil-means-unwired shape every other optional Server dependency uses.
func TestStatusOmitsLastPassWhenUnwired(t *testing.T) {
	w := rig(t)
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastPassAt != "" || st.LastPassDecision != "" || st.LastPassReason != "" {
		t.Fatalf("status = %+v, want last-pass fields absent", st)
	}
}

// TestStatusOmitsLastPassBeforeTheFirstPass: self_deploy configured (LastPass
// wired) but no pass has run yet this process — a restart — must also read
// as absent, never a zero time or empty decision presented as real.
func TestStatusOmitsLastPassBeforeTheFirstPass(t *testing.T) {
	w := rig(t)
	w.srv.LastPass = func() (time.Time, string, string, bool) {
		return time.Time{}, "", "", false
	}
	st, err := statusOnly(t, w.srv).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastPassAt != "" || st.LastPassDecision != "" || st.LastPassReason != "" {
		t.Fatalf("status = %+v, want last-pass fields absent before the first pass", st)
	}
}
