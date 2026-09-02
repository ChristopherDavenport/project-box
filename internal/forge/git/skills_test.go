package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// skillSource builds a repository that carries two skills at different paths
// and returns its path plus the commit each version was made at.
func skillSource(t *testing.T) (remote string, first, second string) {
	t.Helper()
	remote = t.TempDir()
	must := func(args ...string) string {
		t.Helper()
		out, err := run(context.Background(), remote, args...)
		if err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
		return strings.TrimSpace(out)
	}
	must("init", "-q", "-b", "main")
	must("config", "user.email", "skills@example.com")
	must("config", "user.name", "Skills")

	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(remote, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("backend/go/skills/go/SKILL.md", "---\nname: go\n---\nfirst\n")
	write("backend/go/skills/go/references/errors.md", "wrap with %w\n")
	write("frontend/lit/skills/lit/SKILL.md", "---\nname: lit\n---\nlit\n")
	must("add", "-A")
	must("commit", "-qm", "skills")
	first = must("rev-parse", "HEAD")

	write("backend/go/skills/go/SKILL.md", "---\nname: go\n---\nsecond\n")
	must("add", "-A")
	must("commit", "-qm", "revise go")
	second = must("rev-parse", "HEAD")
	return remote, first, second
}

func skillMirror(t *testing.T) *Mirror {
	t.Helper()
	m, err := NewMirror(t.TempDir(), tenant.ID("acme"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func gitSkill(name, remote, commit, path string) agent.SkillRef {
	return agent.SkillRef{Name: name, Source: agent.Source{
		Kind: agent.SourceGit, Repo: remote, Commit: commit, Path: path,
	}}
}

// TestSkillDigestIsTheTreeNotTheCommit is what makes a skill ref useful for
// change management: the digest tracks the skill's own content, so revising a
// NEIGHBOURING skill in the same repository does not restate this one, and
// revising this one cannot go unnoticed.
func TestSkillDigestIsTheTreeNotTheCommit(t *testing.T) {
	ctx := context.Background()
	remote, first, second := skillSource(t)
	m := skillMirror(t)

	at := func(commit, name, path string) string {
		t.Helper()
		got, err := m.ResolveSkills(ctx, []agent.SkillRef{gitSkill(name, remote, commit, path)})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got[0].Source.Digest == "" {
			t.Fatal("resolved skill carries no digest")
		}
		return got[0].Source.Digest
	}

	goFirst := at(first, "go", "backend/go/skills/go")
	goSecond := at(second, "go", "backend/go/skills/go")
	if goFirst == goSecond {
		t.Error("the skill's text changed and its digest did not — the agentref would call two agents the same")
	}
	litFirst := at(first, "lit", "frontend/lit/skills/lit")
	litSecond := at(second, "lit", "frontend/lit/skills/lit")
	if litFirst != litSecond {
		t.Error("an untouched skill changed digest because a sibling moved — every pin would churn on every commit")
	}

	// A resolved skill is governed material: it came from a commit, so
	// Bundle.Ungoverned must not flag it even though the box.yaml line that
	// named it is only configuration.
	resolved, err := m.ResolveSkills(ctx, []agent.SkillRef{gitSkill("go", remote, first, "backend/go/skills/go")})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved[0].Source.Governed() {
		t.Error("a skill pinned to a commit does not read as governed")
	}
}

// TestUnpinnedSkillIsRefused. A branch names different content on different
// days, which is precisely what a ref is for preventing; the resolver must
// not quietly accept one.
func TestUnpinnedSkillIsRefused(t *testing.T) {
	ctx := context.Background()
	remote, first, _ := skillSource(t)
	m := skillMirror(t)

	for _, tc := range []struct {
		name string
		ref  agent.SkillRef
		want string
	}{
		{"no commit", gitSkill("go", remote, "", "backend/go/skills/go"), "commit"},
		{"no repo", gitSkill("go", "", first, "backend/go/skills/go"), "repo"},
		{"no name", gitSkill("", remote, first, "backend/go/skills/go"), "name"},
		{"not git", agent.SkillRef{Name: "go", Source: agent.Source{Kind: agent.SourceInline}}, "git ref"},
		{"missing path", gitSkill("go", remote, first, "nowhere/at/all"), "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.ResolveSkills(ctx, []agent.SkillRef{tc.ref})
			if err == nil {
				t.Fatal("accepted a ref that cannot identify an exact tree")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not say what is wrong (%q, wanted %q)", err, tc.want)
			}
		})
	}
}

// TestSkillsMaterializeAsALoadablePlugin: the harness loads plugins, not
// loose directories, so the generated tree has to be one — manifest included
// — and each skill has to sit under its OWN name rather than echoing the
// path it happened to occupy upstream.
func TestSkillsMaterializeAsALoadablePlugin(t *testing.T) {
	ctx := context.Background()
	remote, first, _ := skillSource(t)
	m := skillMirror(t)

	refs, err := m.ResolveSkills(ctx, []agent.SkillRef{
		gitSkill("golang", remote, first, "backend/go/skills/go"),
		gitSkill("lit", remote, first, "frontend/lit/skills/lit"),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// t.TempDir's own cleanup is os.RemoveAll, which cannot delete a sealed
	// tree — the same wall the mirror's prune hit.
	dir := filepath.Join(t.TempDir(), "run.skills")
	t.Cleanup(func() { RemoveContext(dir) })
	wrote, err := m.MaterializeSkills(ctx, dir, refs)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if !wrote {
		t.Fatal("materialize reported nothing written for two skills")
	}

	manifest, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("no plugin manifest — the harness would not load this: %v", err)
	}
	if !strings.Contains(string(manifest), SkillPluginName) {
		t.Errorf("manifest does not name the plugin: %s", manifest)
	}
	body, err := os.ReadFile(filepath.Join(dir, "skills", "golang", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not under its configured name: %v", err)
	}
	if !strings.Contains(string(body), "first") {
		t.Errorf("materialized the wrong revision: %s", body)
	}
	// Sub-directories come along: a skill is a directory, not a file, and
	// references/ is where the bulk of one usually lives.
	if _, err := os.Stat(filepath.Join(dir, "skills", "golang", "references", "errors.md")); err != nil {
		t.Errorf("skill sub-directory was dropped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "lit", "SKILL.md")); err != nil {
		t.Errorf("second skill missing: %v", err)
	}
}

// TestMaterializedSkillsAreReadOnlyAndReplaceable. Read-only because a skill
// an agent can edit no longer matches the digest its agentref claims;
// replaceable because the next run has to be able to delete a tree the last
// run sealed — the naked RemoveAll cannot, and that failure looks like a
// broken filesystem rather than a mode.
func TestMaterializedSkillsAreReadOnlyAndReplaceable(t *testing.T) {
	ctx := context.Background()
	remote, first, second := skillSource(t)
	m := skillMirror(t)
	// t.TempDir's own cleanup is os.RemoveAll, which cannot delete a sealed
	// tree — the same wall the mirror's prune hit.
	dir := filepath.Join(t.TempDir(), "run.skills")
	t.Cleanup(func() { RemoveContext(dir) })

	lay := func(commit string) {
		t.Helper()
		refs, err := m.ResolveSkills(ctx, []agent.SkillRef{gitSkill("golang", remote, commit, "backend/go/skills/go")})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if _, err := m.MaterializeSkills(ctx, dir, refs); err != nil {
			t.Fatalf("materialize: %v", err)
		}
	}

	lay(first)
	skill := filepath.Join(dir, "skills", "golang", "SKILL.md")
	if err := os.WriteFile(skill, []byte("tampered"), 0o644); err == nil {
		t.Error("an agent could rewrite its own skill mid-run")
	}

	// Re-laying over the sealed tree is what every subsequent run does.
	lay(second)
	body, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("re-materialize left nothing readable: %v", err)
	}
	if !strings.Contains(string(body), "second") {
		t.Errorf("re-materialize kept the stale revision: %s", body)
	}
}

// TestPinnedSkillNeedsNoNetworkTwice. Per-run resolution is only affordable
// if a commit already in the mirror is not re-fetched; if it were, every
// build would pay a round trip per skill to learn what it already knew.
func TestPinnedSkillNeedsNoNetworkTwice(t *testing.T) {
	ctx := context.Background()
	remote, first, _ := skillSource(t)
	m := skillMirror(t)
	ref := gitSkill("golang", remote, first, "backend/go/skills/go")

	if _, err := m.ResolveSkills(ctx, []agent.SkillRef{ref}); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// Break the remote. A resolver that re-fetches now fails; one that trusts
	// the pin it already holds does not.
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	got, err := m.ResolveSkills(ctx, []agent.SkillRef{ref})
	if err != nil {
		t.Fatalf("re-resolving a pin already in the mirror went to the network: %v", err)
	}
	if got[0].Source.Digest == "" {
		t.Error("second resolve produced no digest")
	}
}
