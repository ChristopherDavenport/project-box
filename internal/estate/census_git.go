package estate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitCensus describes repositories by asking git — the collector that works
// against any remote, forge API or not. Facts it produces are namespaced
// "git.*" and "go.*" so a wrong fact leads back here.
type GitCensus struct{}

// Collect gathers facts from a repository's remote without a checkout: the
// tip of the default branch, and go.version parsed from go.mod at that tip.
func (GitCensus) Collect(ctx context.Context, remote, defaultBranch string) ([]Fact, error) {
	now := time.Now().UTC()
	tip, err := lsRemote(ctx, remote, "refs/heads/"+defaultBranch)
	if err != nil {
		return nil, err
	}
	facts := []Fact{
		{Key: "git.default-branch", Value: defaultBranch, Collector: "git-census", Observed: now},
		{Key: "git.tip", Value: tip, Collector: "git-census", Observed: now},
	}
	if gomod, err := show(ctx, remote, tip, "go.mod"); err == nil {
		for _, line := range strings.Split(gomod, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
				facts = append(facts, Fact{Key: "go.version", Value: strings.TrimSpace(v), Collector: "git-census", Observed: now})
				break
			}
		}
	}
	return facts, nil
}

func lsRemote(ctx context.Context, remote, ref string) (string, error) {
	out, err := gitOut(ctx, "", "ls-remote", remote, ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("estate: %s has no %s", remote, ref)
	}
	return fields[0], nil
}

// show reads one file at one commit. It needs the objects locally, so it
// fetches the tip into a throwaway bare repo — census work never touches a
// mirror another run is using.
func show(ctx context.Context, remote, tip, path string) (string, error) {
	dir, err := os.MkdirTemp("", "census-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if _, err := gitOut(ctx, "", "init", "--bare", "-q", dir); err != nil {
		return "", err
	}
	if _, err := gitOut(ctx, dir, "fetch", "-q", "--depth=1", remote, tip); err != nil {
		return "", err
	}
	return gitOut(ctx, dir, "show", tip+":"+path)
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
