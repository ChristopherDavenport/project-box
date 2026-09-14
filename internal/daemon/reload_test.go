package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// dialSocket serves the daemon's API over a real unix socket with the peer
// credential wiring production uses (ConnContext), so a test exercising a
// mutating endpoint gets a resolvable peer the same way boxd does — a TCP
// httptest.Server, used elsewhere here for read-only calls, has none.
func dialSocket(t *testing.T, d *Daemon) *api.Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := api.Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: d.API.Handler(), ConnContext: api.ConnContext}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := api.Dial("unix:"+sock, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// selfAccountName is who SO_PEERCRED names this test process's own uid as —
// the identity a socket-connected client above asserts with no claim.
func selfAccountName(t *testing.T) string {
	t.Helper()
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil || u.Username == "" {
		return "uid:" + strconv.Itoa(os.Getuid())
	}
	return u.Username
}

func bare(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name+".git")
	mustGit(t, "", "init", "--bare", dir)
	mustGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), name+"-seed")
	mustGit(t, "", "clone", dir, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte(name+"\n"), 0o644)
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "seed")
	mustGit(t, seed, "push", "origin", "HEAD:refs/heads/main")
	return dir
}

const buildCmd = `echo done > out.txt && git add -A && git -c user.name=b -c user.email=b@i commit -m work`

func writeCfg(t *testing.T, path, db, root string, remotes map[string]string) {
	t.Helper()
	var repos strings.Builder
	for name, remote := range remotes {
		fmt.Fprintf(&repos, `
      - name: %s
        remote: %s
        build: '%s'
        check: 'test -f out.txt'`, name, remote, buildCmd)
	}
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
every: 1s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    repos:%s
`, db, root, repos.String())
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReloadAddsARepoWithoutRestart is the confirmation by name: a running
// daemon picks up a new repository and its route from the edited file — no
// restart, work flowing to the new target on the next tick.
func TestReloadAddsARepoWithoutRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	apiRem := bare(t, "api")

	writeCfg(t, cfgPath, db, root, map[string]string{"api": apiRem})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	c := dialSocket(t, d)

	// The original config works.
	if _, err := c.OpenIngress(ctx, "shop-api", "api-1", "first change", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// The not-yet-configured route is refused.
	if _, err := c.OpenIngress(ctx, "shop-web", "web-1", "too early", "", ""); err == nil {
		t.Fatal("unconfigured route accepted before reload")
	}

	// Edit the file: add a second repo. Reload over the API.
	webRem := bare(t, "web")
	writeCfg(t, cfgPath, db, root, map[string]string{"api": apiRem, "web": webRem})
	if summary, err := c.Reload(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	} else if !strings.Contains(summary, "executors=2") {
		t.Fatalf("reload summary: %q", summary)
	}

	// The new route exists NOW, on the same process.
	res, err := c.OpenIngress(ctx, "shop-web", "web-1", "change the web", "", "")
	if err != nil {
		t.Fatalf("new route after reload: %v", err)
	}
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !d.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the tick's dispatched run")
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone {
		t.Fatalf("work on the reloaded repo: %+v %v", item, err)
	}
	if out := mustGit(t, webRem, "log", "--format=%s", "refs/heads/main"); !strings.Contains(out, "work") {
		t.Fatalf("reloaded repo's remote never moved:\n%s", out)
	}
	// The reload is on the record: who, and what shape. The actor is the
	// socket's own peer — this client carried no X-Box-Actor claim.
	wantActor := selfAccountName(t)
	saw := false
	for r, err := range d.store.Log().Since(ctx, tenant.ID("acme"), "control/config", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "config-reloaded" && r.Actor == wantActor && strings.Contains(string(r.Body), "executors=2") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("the reload left no evidence")
	}
}

// TestReloadRefusesIdentityChangesAndBadConfigs: identity fields and broken
// files are refused with the OLD configuration left running.
func TestReloadRefusesIdentityChangesAndBadConfigs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	db, root := filepath.Join(dir, "box.db"), filepath.Join(dir, "forge")
	apiRem := bare(t, "api")
	writeCfg(t, cfgPath, db, root, map[string]string{"api": apiRem})
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	// Identity change: a different tenant in the same file.
	bad := strings.Replace(mustRead(t, cfgPath), "tenant: acme", "tenant: globex", 1)
	os.WriteFile(cfgPath, []byte(bad), 0o644)
	if _, err := d.Reload(ctx, "chris"); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("tenant change should refuse: %v", err)
	}
	// Identity change: a different root, tenant restored, in the same file.
	writeCfg(t, cfgPath, db, filepath.Join(dir, "forge2"), map[string]string{"api": apiRem})
	if _, err := d.Reload(ctx, "chris"); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("root change should refuse: %v", err)
	}
	// Broken YAML.
	os.WriteFile(cfgPath, []byte("boxes: ["), 0o644)
	if _, err := d.Reload(ctx, "chris"); err == nil {
		t.Fatal("broken config should refuse")
	}
	// The old configuration is still running: original route still works.
	writeCfg(t, cfgPath, db, root, map[string]string{"api": apiRem}) // restore for cleanliness
	ts := httptest.NewServer(d.API.Handler())
	t.Cleanup(ts.Close)
	c, _ := api.Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "", "chris")
	if _, err := c.OpenIngress(ctx, "shop-api", "still-alive", "prove it", "", ""); err != nil {
		t.Fatalf("old config not running after refused reloads: %v", err)
	}
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
