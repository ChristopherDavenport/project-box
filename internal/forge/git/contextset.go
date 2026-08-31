package git

import (
	"context"
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
func (m *Mirror) MaterializeContext(ctx context.Context, dir string, set agent.ContextSet) error {
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
