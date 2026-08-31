package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Sweep invalidates bound records whose code has moved: for each record bound
// to (repo, path, commit), if the path's content differs between the bound
// commit and the repo's current tip, the record is flagged stale — by the
// sweep, not by anyone remembering. This is the property that separates a
// knowledge base from a pile of increasingly wrong assertions.
//
// tips maps repo remote to its current tip commit, as the estate census
// already records; the sweep judges, the census observes.
func (b *Base) Sweep(ctx context.Context, scope string, tips map[string]string) (flagged []Record, err error) {
	records, superseded, stale, err := b.fold(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Repo == "" || superseded[r.ID] || stale[r.ID] {
			continue
		}
		tip, known := tips[r.Repo]
		if !known || tip == r.Commit {
			continue
		}
		changed, err := pathChanged(ctx, r.Repo, r.Commit, tip, r.Path)
		if err != nil {
			return flagged, err
		}
		if !changed {
			continue
		}
		why := fmt.Sprintf("bound to %s@%s; content differs at tip %s", bindingSubject(r.Repo, r.Path), short(r.Commit), short(tip))
		if err := b.Flag(ctx, scope, r.ID, why, "knowledge-sweep"); err != nil {
			return flagged, err
		}
		r.Stale = true
		flagged = append(flagged, r)
	}
	return flagged, nil
}

// pathChanged fetches both commits shallowly into a throwaway repo and diffs
// the path (or the whole tree when the binding has no path).
func pathChanged(ctx context.Context, remote, was, now, path string) (bool, error) {
	dir, err := os.MkdirTemp("", "sweep-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	if _, err := gitOut(ctx, "", "init", "--bare", "-q", dir); err != nil {
		return false, err
	}
	if _, err := gitOut(ctx, dir, "fetch", "-q", remote, was, now); err != nil {
		return false, err
	}
	args := []string{"diff", "--quiet", was, now}
	if path != "" {
		args = append(args, "--", path)
	}
	_, err = gitOut(ctx, dir, args...)
	if err == nil {
		return false, nil // exit 0: no difference
	}
	if strings.Contains(err.Error(), "exit status 1") {
		return true, nil // exit 1: the path differs
	}
	return false, err
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
