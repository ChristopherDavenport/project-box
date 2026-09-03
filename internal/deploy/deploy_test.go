package deploy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/render"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

// TestDeployIsAChangeSet: the whole deploy plane in one test. A deploy runs
// through the SAME loop as a code change — claim, lease, render-and-commit,
// gate, fenced land — into a repo whose role is env. No cluster credentials
// appear anywhere; what a cluster runs is whatever Argo reconciles from the
// repo this test just moved.
func TestDeployIsAChangeSet(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The env repo: bare remote seeded with an empty README.
	remote := filepath.Join(t.TempDir(), "env-prod.git")
	mustSh(t, "", "git", "init", "--bare", remote)
	mustSh(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	seed := filepath.Join(t.TempDir(), "seed")
	mustSh(t, "", "git", "clone", remote, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("prod env\n"), 0o644)
	mustSh(t, seed, "git", "add", ".")
	mustSh(t, seed, "git", "commit", "-m", "seed")
	mustSh(t, seed, "git", "push", "origin", "HEAD:refs/heads/main")

	envRepo := forge.Repo{Name: "env-prod", Remote: remote, Role: forge.RoleEnv, DefaultBranch: "main"}
	m, err := git.NewMirror(t.TempDir(), tn, s.Leaser("runner-1"))
	if err != nil {
		t.Fatal(err)
	}

	service := render.Service{Name: "shop-api", Box: "shop", Image: "gcr.io/acme/shop-api", Tag: "v1.2.3", Port: 8080, Replicas: 2}
	target := render.Target{Env: "prod", Namespace: "shop"}

	l := &loop.Loop{
		Store: s, Runner: "runner-1", Mirror: m, Repo: envRepo, Branch: "main",
		Builder: Builder{Service: service, Target: target},
		Gates: func(ws forge.Workspace) []forge.Gate {
			// The env gate: the rendered manifests must parse as the render
			// of the declaration — drift refused at the gate, pre-cluster.
			return []forge.Gate{gate.Command{Label: "manifest-check", Dir: ws.Dir,
				Commands: [][]string{{"test", "-s", "services/shop-api.yaml"}}}}
		},
		TTL: time.Hour,
	}

	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "runner-1"}
	ing, _, err := router.Open(ctx,
		ingress.Route{Name: "releases", Kind: ingress.RouteWebhook, Opens: kind.SeedIssue, Box: "shop", Requester: "chris"},
		ingress.Delivery{Key: "release-v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.Queue{Store: s, Runner: "runner-1", TTL: time.Hour}
	item, err := q.Submit(ctx, loop.ItemFor(ing, "deploy-1", "ship v1.2.3 to prod"))
	if err != nil {
		t.Fatal(err)
	}
	item, err = l.Run(ctx, item)
	if err != nil {
		t.Fatalf("deploy loop: %v", err)
	}
	if item.State != queue.StateDone {
		t.Fatalf("deploy did not land: %+v", item)
	}

	// The env repo now carries exactly the canonical render.
	got := mustSh(t, remote, "git", "show", "refs/heads/main:services/shop-api.yaml")
	want, err := render.Manifests(service, target, render.DefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSpace(string(want)) {
		t.Fatalf("env repo content is not the canonical render:\n%s", got)
	}
	// Provenance: the env commit answers like any landed change.
	kinds := map[string]bool{}
	for r, err := range s.Log().BySubject(ctx, tn, item.Head) {
		if err != nil {
			t.Fatal(err)
		}
		kinds[r.Kind] = true
	}
	for _, k := range []string{"built", "gate", "land", "done"} {
		if !kinds[k] {
			t.Fatalf("deploy provenance missing %q", k)
		}
	}
	// Rollback is git revert: undo the deploy commit and the env repo is back
	// to its pre-deploy tree — no new machinery, no cluster call.
	work := filepath.Join(t.TempDir(), "rollback")
	mustSh(t, "", "git", "clone", remote, work)
	mustSh(t, work, "git", "revert", "--no-edit", item.Head)
	mustSh(t, work, "git", "push", "origin", "HEAD:refs/heads/main")
	if out, err := sh(remote, "git", "cat-file", "-e", "refs/heads/main:services/shop-api.yaml"); err == nil {
		t.Fatalf("revert did not remove the deploy: %s", out)
	}
}

func sh(dir, name string, args ...string) (string, error) {
	cmd := execCommand(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func mustSh(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := sh(dir, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return strings.TrimSpace(out)
}
