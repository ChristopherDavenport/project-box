package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// MaterializeContext lays an agent's granted context under dir, and nothing
// else: one subdirectory per granted repo, at its pinned commit, narrowed to
// its granted paths, files read-only.
//
// The extraction is git-archive from the mirror, so what lands is plain
// files — no .git, no remotes, no history beyond the pinned tree. An agent
// cannot fetch its way past the pin, because there is nothing to fetch with.
// Least privilege holds by construction: repos outside the set are simply
// never written here, and the runtime points the agent at this directory and
// its workspace, nowhere else.
//
// A second materialization replaces the first rather than extracting over it.
// Two reasons, and the second is the one that matters: tar cannot overwrite a
// tree the previous pass made read-only — the 0555 directories deny the
// unlink — so a re-claimed run would fail here rather than build; and what
// survived would be material the agent can read and was not granted this
// time, which is exactly the property the set exists to state.
func (m *Mirror) MaterializeContext(ctx context.Context, dir string, set agent.ContextSet) error {
	if err := RemoveContext(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, cr := range set.Repos {
		if cr.Commit == "" {
			return fmt.Errorf("git: context repo %q is unpinned — a context whose content can change mid-run is an injection surface", cr.Repo)
		}
		if _, ok := m.Repo(cr.Repo); !ok {
			return fmt.Errorf("git: context repo %q not established by Fetch", cr.Repo)
		}
		dest := filepath.Join(dir, cr.Repo)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		args := []string{"archive", "--format=tar", cr.Commit}
		args = append(args, cr.Paths...)
		archive := exec.CommandContext(ctx, "git", args...)
		archive.Dir = m.mirrorDir(cr.Repo)
		untar := exec.CommandContext(ctx, "tar", "-x", "-C", dest)
		pipe, err := archive.StdoutPipe()
		if err != nil {
			return err
		}
		untar.Stdin = pipe
		if err := untar.Start(); err != nil {
			return err
		}
		if err := archive.Run(); err != nil {
			return fmt.Errorf("git: archive %s@%s: %w", cr.Repo, cr.Commit, err)
		}
		if err := untar.Wait(); err != nil {
			return fmt.Errorf("git: extract %s: %w", cr.Repo, err)
		}
		if !cr.Write {
			if err := makeReadOnly(dest); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveContext deletes a materialized context tree, restoring write
// permission on the way down.
//
// Plain [os.RemoveAll] cannot do it. Removing an entry needs write permission
// on its DIRECTORY, and [makeReadOnly] leaves every directory at 0555, so the
// reclaim fails on the first one — silently, from Prune's point of view,
// which is how a factory accumulates every context set it has ever
// materialized.
func RemoveContext(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Best-effort: a path that cannot be chmodded is reported by the removal
	// below, where the error means something.
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(path, 0o755)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

func makeReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(path, 0o555)
		}
		return os.Chmod(path, 0o444)
	})
}
