// Command deployloop runs the real deploy loop against a live env repo: the
// same internal/loop, internal/deploy, and internal/gate code the tests
// exercise, pointed at a remote a GitOps reconciler watches. It is the
// driver for the Phase 4 reconcile-verification leg (hack/reconcile-e2e.sh):
// everything up to the landed env commit is the control plane's code, and
// everything after it is Argo CD's — the boundary the architecture draws.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

func main() {
	remote := flag.String("remote", "", "env repo remote URL (with credentials)")
	image := flag.String("image", "nginx", "service image")
	tag := flag.String("tag", "1.27-alpine", "service tag")
	name := flag.String("service", "shop-api", "service name")
	deliveryKey := flag.String("delivery", "release-e2e-1", "delivery idempotency key")
	flag.Parse()
	if *remote == "" {
		log.Fatal("-remote is required")
	}
	ctx := context.Background()
	tn := tenant.ID("acme")

	work, err := os.MkdirTemp("", "deployloop-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(work)
	s, err := sqlite.Open(filepath.Join(work, "box.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	envRepo := forge.Repo{Name: "env-prod", Remote: *remote, Role: forge.RoleEnv, DefaultBranch: "main"}
	mirror, err := gitforge.NewMirror(filepath.Join(work, "forge"), tn, s.Leaser("e2e-runner"))
	if err != nil {
		log.Fatal(err)
	}

	router := &ingress.StoreRouter{Store: s, Tenant: tn, Runner: "e2e-runner"}
	ing, opened, err := router.Open(ctx,
		ingress.Route{Name: "releases", Kind: ingress.RouteWebhook, Opens: ingress.KindIssue, Box: "shop", Requester: "chris"},
		ingress.Delivery{Key: *deliveryKey})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ingress %s (opened=%v)\n", ing.ID, opened)

	l := &loop.Loop{
		Store: s, Runner: "e2e-runner", Mirror: mirror, Repo: envRepo, Branch: "main",
		Builder: deployBuilder(*name, *image, *tag),
		Gates: func(ws forge.Workspace) []forge.Gate {
			return []forge.Gate{gate.Command{Label: "manifest-check", Dir: ws.Dir,
				Commands: [][]string{{"test", "-s", "services/" + *name + ".yaml"}}}}
		},
		TTL: 10 * time.Minute,
	}
	q := &queue.Queue{Store: s, Runner: "e2e-runner", TTL: 10 * time.Minute}
	item, err := q.Submit(ctx, loop.ItemFor(ing, "deploy-"+*deliveryKey, fmt.Sprintf("ship %s:%s to prod", *image, *tag)))
	if err != nil {
		log.Fatal(err)
	}
	item, err = l.Run(ctx, item)
	if err != nil {
		log.Fatalf("loop: %v (item %+v)", err, item)
	}
	if item.State != queue.StateDone {
		log.Fatalf("item finished %s: %s", item.State, item.Note)
	}
	fmt.Printf("landed %s\n", item.Head)

	// The audit query, run against the live store before it is thrown away.
	fmt.Println("provenance:")
	for r, err := range s.Log().BySubject(ctx, tn, item.Head) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %-6s %s/%s seq %d\n", r.Kind, r.Tenant, r.Scope, r.Seq)
	}
}

func deployBuilder(name, image, tag string) loop.Builder {
	return builderFunc{name: name, image: image, tag: tag}
}

type builderFunc struct{ name, image, tag string }

func (b builderFunc) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	return deployBuild(ctx, ws, task, b.name, b.image, b.tag)
}
