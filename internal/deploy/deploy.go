// Package deploy makes deployment a ChangeSet, not a verb.
//
// A deploy is the loop run against a repository whose role is env: the
// Builder renders the service's manifests into the env repo's working tree
// and commits. Gates judge it, the lander pushes it with the same fencing and
// expected-old-value checks as any change, Argo CD reconciles the result into
// the cluster — and rollback is git revert, because it was never anything
// else. The control plane holds no cluster credentials at any point in this
// package, which is the blast-radius property the whole plane is built on.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/render"
)

// Builder writes a service's rendered manifests into an env repo workspace.
// It satisfies loop.Builder: to the loop, a deploy is indistinguishable from
// any other change, which is the point.
type Builder struct {
	Service  render.Service
	Target   render.Target
	Template string
	// Identity authors and commits the deploy. The zero value is
	// [forge.DefaultIdentity].
	Identity forge.Identity
}

// Build renders and commits. The commit message carries the declaration's
// identity so the env repo's history reads as a deploy log.
func (b Builder) Build(ctx context.Context, ws forge.Workspace, task string) (forge.Commit, error) {
	tpl := b.Template
	if tpl == "" {
		tpl = render.DefaultTemplate
	}
	manifests, err := render.Manifests(b.Service, b.Target, tpl)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(ws.Dir, "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, b.Service.Name+".yaml"), manifests, 0o644); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("deploy %s %s:%s to %s\n\n%s", b.Service.Name, b.Service.Image, b.Service.Tag, b.Target.Env, task)
	if _, err := gitIn(ctx, b.Identity, ws.Dir, "add", "."); err != nil {
		return "", err
	}
	if _, err := gitIn(ctx, b.Identity, ws.Dir, "commit", "-m", msg); err != nil {
		return "", err
	}
	out, err := gitIn(ctx, b.Identity, ws.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return forge.Commit(strings.TrimSpace(out)), nil
}

func gitIn(ctx context.Context, id forge.Identity, dir string, args ...string) (string, error) {
	if err := id.Validate(); err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Env = append(cmd.Env, id.Env()...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}
