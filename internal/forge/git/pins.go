package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ReadPinned returns the bytes of one file at one commit in a remote, along
// with the git object id of the blob.
//
// The object id is the strongest content address available for free — it
// changes if any byte changes, it is computable without extracting anything,
// and it is the id the source repository would compute — which is what makes a
// governed kind declaration's digest mean the same thing as a pinned skill's.
//
// Fetching is conditional in exactly the way skill resolution is: a commit
// already mirrored costs no network. The mirror directory is shared with skill
// pins on purpose. What is being cached is "this remote at this commit", and
// splitting it by what the pin happens to be FOR would mean fetching the same
// repository twice because two different features asked for it.
func (m *Mirror) ReadPinned(ctx context.Context, remote, commit, path string) ([]byte, string, error) {
	if remote == "" || commit == "" || path == "" {
		return nil, "", fmt.Errorf("git: a pinned file needs a remote, a commit and a path (got %q %q %q)", remote, commit, path)
	}
	dir, err := m.ensureSkillCommit(ctx, remote, commit)
	if err != nil {
		return nil, "", err
	}
	spec := commit + ":" + path
	oid, err := run(ctx, dir, "rev-parse", spec)
	if err != nil {
		return nil, "", fmt.Errorf("git: %s not found at %s in %s: %w", path, commit, remote, err)
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", strings.TrimSpace(oid))
	cmd.Dir = dir
	body, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("git: reading %s at %s in %s: %w", path, commit, remote, err)
	}
	return body, strings.TrimSpace(oid), nil
}
