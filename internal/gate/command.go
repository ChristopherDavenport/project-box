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
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// maxHeadFailureBlocks bounds how many failing-line blocks verdict() keeps
// from the head of a transcript, so a pathologically repeating failure
// cannot make the record unreadable.
const maxHeadFailureBlocks = 40

// buildErrorLine matches a Go compiler error ("path/to/file.go:12:3: ...") or
// a package header ("# example.com/pkg") emitted around a build failure.
var buildErrorLine = regexp.MustCompile(`^(# |[^\s:]+\.go:\d+:\d+: )`)

// unsafeLabelChar matches any run of characters unsafe in a gate-log sibling
// file name: anything but letters, digits, dot, dash and underscore.
var unsafeLabelChar = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeLabel makes a gate Label safe to use as a path component, mirroring
// the sibling-of-the-workspace convention in [forge.ContextDir] and
// [forge.SkillsDir].
func sanitizeLabel(label string) string {
	return unsafeLabelChar.ReplaceAllString(label, "_")
}

// isFailureLine reports whether line marks a test failure, a panic, or a Go
// build error — the lines Command.verdict hunts for near the head of a
// transcript that a 2048-byte tail would otherwise push out entirely.
func isFailureLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "--- FAIL"):
		return true
	case strings.HasPrefix(line, "FAIL"):
		return true
	case strings.HasPrefix(line, "panic:"):
		return true
	case buildErrorLine.MatchString(line):
		return true
	}
	return false
}

// headFailures scans transcript line by line and returns every failure line
// found (see isFailureLine), each preceded by the line immediately above it
// for context, in original order. The number of matched blocks is capped at
// maxHeadFailureBlocks.
func headFailures(transcript []byte) string {
	lines := strings.Split(string(transcript), "\n")
	var out []string
	blocks := 0
	for i, line := range lines {
		if blocks >= maxHeadFailureBlocks {
			break
		}
		if !isFailureLine(line) {
			continue
		}
		if i > 0 {
			out = append(out, lines[i-1])
		}
		out = append(out, line)
		blocks++
	}
	return strings.Join(out, "\n")
}

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
		return c.verdict(forge.DecisionFail, fmt.Sprintf("%q exited non-zero", strings.Join(argv, " ")), transcript.Bytes())
	}
	return c.verdict(forge.DecisionPass, "all commands passed", transcript.Bytes())
}

// verdict builds the Verdict for a finished run. The full transcript is
// written to a sibling of the workspace directory — never inlined — and
// EvidenceRef.URI points at it, per [forge.EvidenceRef]'s own promise that
// evidence is referenced by location. Reason carries any head-of-transcript
// failure lines found (see headFailures) ahead of the existing tail-of-2048
// bytes, so a failure early in a long run is not pushed out entirely; on a
// clean pass headFailures ordinarily finds nothing and Reason's shape is
// unchanged from before.
func (c Command) verdict(d forge.Decision, reason string, transcript []byte) (forge.Verdict, error) {
	sum := sha256.Sum256(transcript)
	tail := transcript
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}

	fullReason := reason
	if head := headFailures(transcript); head != "" {
		fullReason += "\n" + head
	}
	fullReason += "\n" + string(tail)

	path := c.Dir + ".gate-" + sanitizeLabel(c.Label) + ".log"
	if err := os.WriteFile(path, transcript, 0o644); err != nil {
		return forge.Verdict{}, fmt.Errorf("gate %s: write transcript: %w", c.Label, err)
	}

	return forge.Verdict{
		Gate: c.Label, Decision: d,
		Reason: fullReason,
		Evidence: []forge.EvidenceRef{{
			Kind: "transcript", URI: path, Digest: hex.EncodeToString(sum[:]),
		}},
	}, nil
}
