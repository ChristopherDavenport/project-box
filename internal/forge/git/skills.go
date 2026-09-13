package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// SkillPluginName is the name of the generated plugin that carries a bundle's
// skills. One plugin rather than one per skill: the harness loads plugins, and
// a directory per skill would multiply manifests without separating anything —
// they share a lifetime, a provenance story, and a single MCP posture.
const SkillPluginName = "box-skills"

// ResolveSkills pins each skill ref to the exact tree it names.
//
// A skill ref is a repo, a commit, and a path — never a branch. The digest is
// the git object id of the tree at that path in that commit, which is the
// strongest content address available for free: it changes if any byte of the
// skill changes, it is computable without extracting anything, and it is the
// same id the source repository would compute. Because [agent.Composer] hashes
// skill digests into the agentref, two runs whose skills differ can no longer
// share a ref, and a run's recorded bundle names the exact skill text that was
// on the machine.
//
// Fetching is conditional: a commit already in the mirror is not re-fetched,
// so a pinned skill costs no network after the first run that needs it. That
// is what makes per-run resolution affordable, and per-run resolution is what
// makes editing box.yaml enough to change what the builders know.
func (m *Mirror) ResolveSkills(ctx context.Context, refs []agent.SkillRef) ([]agent.SkillRef, error) {
	out := make([]agent.SkillRef, 0, len(refs))
	for _, r := range refs {
		if r.Name == "" {
			return nil, fmt.Errorf("git: skill ref with no name (%s)", r.Source.Repo)
		}
		if r.Source.Kind != agent.SourceGit {
			return nil, fmt.Errorf("git: skill %q is %q, not a git ref — this resolver pins commits", r.Name, r.Source.Kind)
		}
		if r.Source.Repo == "" || r.Source.Commit == "" {
			return nil, fmt.Errorf("git: skill %q needs both a repo and a commit — a branch names different content on different days", r.Name)
		}
		dir, err := m.ensureSkillCommit(ctx, r.Source.Repo, r.Source.Commit)
		if err != nil {
			return nil, err
		}
		path := r.Source.Path
		if path == "" {
			path = "."
		}
		obj, err := run(ctx, dir, "rev-parse", r.Source.Commit+":"+path)
		if err != nil {
			return nil, fmt.Errorf("git: skill %q: %s not found at %s in %s: %w",
				r.Name, path, r.Source.Commit, r.Source.Repo, err)
		}
		r.Source.Digest = strings.TrimSpace(obj)
		out = append(out, r)
	}
	return out, nil
}

// MaterializeSkills writes the bundle's skills under dir as one generated
// harness plugin, and reports whether anything was written.
//
// A plugin directory, because that is the only shape the harness loads from an
// arbitrary path; a sibling of the workspace, never inside it, for the reason
// context material is a sibling — the builder commits whatever it finds dirty
// in the tree, so a skill written into the workspace would land in the
// repository as part of the change. Extracted read-only: skills are reading
// material, and a skill an agent can edit mid-run is a skill that no longer
// matches the digest its agentref claims.
func (m *Mirror) MaterializeSkills(ctx context.Context, dir string, refs []agent.SkillRef) (bool, error) {
	if len(refs) == 0 {
		return false, nil
	}
	// Replace rather than extract over: tar cannot overwrite a tree the last
	// pass sealed, and what survived would be a skill this run was not
	// granted. The same reasoning MaterializeContext records, and the same
	// reclaim — it is named for its first caller, not for its only one.
	if err := RemoveContext(dir); err != nil {
		return false, err
	}
	manifest := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(manifest, 0o755); err != nil {
		return false, err
	}
	body := fmt.Sprintf(`{"name":%q,"description":%q,"version":"0"}`+"\n",
		SkillPluginName, "Skills pinned by box.yaml and resolved into this run's bundle.")
	if err := os.WriteFile(filepath.Join(manifest, "plugin.json"), []byte(body), 0o644); err != nil {
		return false, err
	}
	for _, r := range refs {
		if r.Source.Digest == "" {
			return false, fmt.Errorf("git: skill %q is unresolved — ResolveSkills must run before materialization", r.Name)
		}
		mirror, err := m.ensureSkillCommit(ctx, r.Source.Repo, r.Source.Commit)
		if err != nil {
			return false, err
		}
		dest := filepath.Join(dir, "skills", r.Name)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return false, err
		}
		// Archive the TREE, not the commit at a path: extracting
		// <digest> puts the skill's own files at the root of dest, so the
		// directory the harness sees is named for the skill rather than
		// echoing wherever it happened to live in its source repository.
		if err := extract(ctx, mirror, r.Source.Digest, dest); err != nil {
			return false, fmt.Errorf("git: materialize skill %q: %w", r.Name, err)
		}
	}
	if err := makeReadOnly(dir); err != nil {
		return false, err
	}
	return true, nil
}

// ensureSkillCommit makes sure a skill repo's mirror holds a commit, cloning
// or fetching only when it does not, and returns the mirror directory.
//
// Each presence check takes only the mirror's read lock, released before
// Fetch is ever called: Fetch takes the write side itself, and a Go RWMutex
// is not reentrant, so holding the read lock across the call would deadlock
// a goroutine against itself. Two runs racing the check both call Fetch,
// which serializes them — a duplicated `remote update`, never a deadlock —
// and the second one finds the commit and returns.
func (m *Mirror) ensureSkillCommit(ctx context.Context, remote, commit string) (string, error) {
	name := skillMirrorName(remote)
	dir := m.mirrorDir(name)
	lk := m.fetchLock(name)
	if _, err := os.Stat(dir); err == nil {
		lk.RLock()
		_, err := run(ctx, dir, "cat-file", "-e", commit+"^{commit}")
		lk.RUnlock()
		if err == nil {
			return dir, nil
		}
	}
	if err := m.Fetch(ctx, forge.Repo{Name: name, Remote: remote}); err != nil {
		return "", fmt.Errorf("git: skill source %s: %w", remote, err)
	}
	lk.RLock()
	_, err := run(ctx, dir, "cat-file", "-e", commit+"^{commit}")
	lk.RUnlock()
	if err != nil {
		return "", fmt.Errorf("git: commit %s is not in %s even after fetching — check the pin", commit, remote)
	}
	return dir, nil
}

// skillMirrorName is a filesystem-safe, collision-free directory name for a
// skill source. Namespaced under skills/ so a skill repo can never take the
// mirror directory of a box repo that happens to share a name.
func skillMirrorName(remote string) string {
	sum := sha256.Sum256([]byte(remote))
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, lastSegment(remote))
	return filepath.Join("skills", slug+"-"+hex.EncodeToString(sum[:4]))
}

func lastSegment(remote string) string {
	s := strings.TrimSuffix(remote, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		s = "skills"
	}
	return s
}

// extract streams a git object into a directory as plain files.
func extract(ctx context.Context, mirror, object, dest string) error {
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", object)
	archive.Dir = mirror
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
		return fmt.Errorf("archive %s: %w", object, err)
	}
	return untar.Wait()
}
