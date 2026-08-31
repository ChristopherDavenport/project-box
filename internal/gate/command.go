// Package gate holds Gate implementations. The first is the plainest and the
// most load-bearing: run the build and the tests, and let the exit codes
// judge. Automated approval is a stronger control than human review only
// while the gate itself is dumb, versioned, and impossible to sweet-talk.
package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// Command evaluates a ChangeSet by running commands in the built workspace.
type Command struct {
	// Label names the gate in verdicts and evidence.
	Label string
	// Dir is the workspace the ChangeSet was built in.
	Dir string
	// Commands run in order; the first non-zero exit fails the gate.
	Commands [][]string
}

// Name identifies the gate.
func (c Command) Name() string { return c.Label }

// Evaluate runs the commands. A command that cannot START is an error — the
// infrastructure failed, not the change — while a command that runs and exits
// non-zero is a failing verdict. Recording the first as the second would
// punish changes for the runner's problems.
func (c Command) Evaluate(ctx context.Context, cs forge.ChangeSet) (forge.Verdict, error) {
	var transcript bytes.Buffer
	for _, argv := range c.Commands {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = c.Dir
		cmd.Stdout, cmd.Stderr = &transcript, &transcript
		fmt.Fprintf(&transcript, "$ %s\n", strings.Join(argv, " "))
		err := cmd.Run()
		if err == nil {
			continue
		}
		if _, ran := err.(*exec.ExitError); !ran {
			return forge.Verdict{}, fmt.Errorf("gate %s: cannot run %q: %w", c.Label, argv[0], err)
		}
		return c.verdict(forge.DecisionFail, fmt.Sprintf("%q exited non-zero", strings.Join(argv, " ")), transcript.Bytes()), nil
	}
	return c.verdict(forge.DecisionPass, "all commands passed", transcript.Bytes()), nil
}

func (c Command) verdict(d forge.Decision, reason string, transcript []byte) forge.Verdict {
	sum := sha256.Sum256(transcript)
	tail := transcript
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	return forge.Verdict{
		Gate: c.Label, Decision: d,
		Reason: reason + "\n" + string(tail),
		Evidence: []forge.EvidenceRef{{
			Kind: "transcript", URI: "inline", Digest: hex.EncodeToString(sum[:]),
		}},
	}
}
