package estate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitCensusCollects(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "r.git")
	mustRun(t, "", "git", "init", "--bare", remote)
	mustRun(t, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	work := filepath.Join(t.TempDir(), "w")
	mustRun(t, "", "git", "clone", remote, work)
	os.WriteFile(filepath.Join(work, "go.mod"), []byte("module example.com/x\n\ngo 1.24\n"), 0o644)
	mustRun(t, work, "git", "add", ".")
	mustRun(t, work, "git", "commit", "-m", "seed")
	mustRun(t, work, "git", "push", "origin", "HEAD:refs/heads/main")

	facts, err := GitCensus{}.Collect(context.Background(), remote, "main")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Fact{}
	for _, f := range facts {
		byKey[f.Key] = f
		if f.Collector != "git-census" {
			t.Fatalf("fact %s not attributed to its collector: %+v", f.Key, f)
		}
	}
	if byKey["go.version"].Value != "1.24" {
		t.Fatalf("go.version: %+v", byKey["go.version"])
	}
	if len(byKey["git.tip"].Value) != 40 {
		t.Fatalf("git.tip: %+v", byKey["git.tip"])
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}
