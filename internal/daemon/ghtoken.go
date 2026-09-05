package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ghAuthToken is the "gh" [gitforge.TokenSource]: it shells out to `gh auth
// token`, fetched fresh per call rather than held, the same idea
// hack/dogfood/run.sh already leans on for git push (`gh auth
// git-credential`) but a different gh subcommand — that one answers a git
// credential helper, this one answers a bearer token for the REST API
// git.PRLander calls directly. It is a stopgap: the GitHub App of Phase 7
// (forge.Minter, internal/forge/credential.go) replaces it at this exact
// seam later, so this never grows toward that.
func ghAuthToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("daemon: gh auth token: %w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
