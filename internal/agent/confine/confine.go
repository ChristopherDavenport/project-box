// Package confine renders what goes in FRONT of an agent: a container whose
// filesystem is the two directories the run was granted, and nothing else.
//
// The seam already existed. A box's `agent:` is an argv rather than a path
// precisely so that something can be put in front of the agent (issue #6),
// and what was missing was the thing to put there. This package is that
// thing: a profile, rendered to an argv, with the two per-run paths left as
// placeholders for [Substitute] to fill.
//
// # Why this is not a control on a malicious agent
//
// It is a control on blast radius. The highest-leverage attack on a factory
// like this is an instruction injected into a context repository — poison what
// the agent believes rather than what it runs — and the answer to that is a
// context set that is pinned and read-only. Pinning makes the set stable;
// confinement is what makes it a BOUNDARY. Without it the set describes what
// an agent was granted rather than what it can reach, and a live run already
// showed the difference: an agent explained its escalation by reporting what
// it had found in "the repo, its git history, the factory mirror, and the
// sibling workspace" — three of those four outside anything it was given.
//
// # Why a container rather than an OS sandbox
//
// The prior art on issue #6 measured sandbox-exec on macOS, and it works. It
// is also an allow-list over a filesystem that is otherwise present, so it has
// to enumerate everything a Node runtime touches before the agent will start
// at all — /dev/ptmx, mach-host*, ipc-posix-sem, the Homebrew prefix — and
// each of those is a per-OS fact that rots. A container inverts it: the host's
// filesystem is not there to be excluded, so what the agent can reach is what
// was mounted. That is the same shape as the context set itself — enumerated,
// not excluded — which is why it is the answer recorded in PLAN §10.
//
// # What this does not confine
//
// The network. An agent that cannot reach a model API does nothing, so the
// container keeps docker's default networking and a context repository that
// talks an agent into posting the workspace somewhere is not stopped by
// anything here. Egress is a separate seam and does not exist yet.
//
// Nor does it bound a run in TIME. `docker run` killed by its parent leaves
// the container running, so a build_timeout fires on the wrapper and the agent
// inside carries on spending. Closing that needs a named container and a
// reaper on the daemon's prune sweep; it is not this change, and it is the
// reason a confined box still wants a token ceiling.
package confine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// WorkspacePlaceholder is replaced with the run's workspace path
	// everywhere it appears in an agent argv.
	WorkspacePlaceholder = "{{workspace}}"

	// ContextPlaceholder is replaced with the run's materialized context
	// directory.
	//
	// A wrapper told only about the workspace confines an agent out of its own
	// granted reading material: the context set is materialized as a sibling
	// of the workspace, at a path chosen per run, so an argv that cannot name
	// it makes context sets and confinement mutually exclusive. The directory
	// exists for every run — empty when the bundle granted nothing — so the
	// argv never has to be conditional on what a particular bundle carries.
	ContextPlaceholder = "{{context}}"

	// SkillsPlaceholder is replaced with the run's materialized skill plugin.
	//
	// Separate from the context set because the two are granted separately and
	// mean different things to a reader of the argv: context is reference
	// material the bundle handed the agent, skills are capability it was
	// given. Like the context directory it exists for every run, so the argv
	// stays unconditional; unlike it, a wrapper that omits this one fails
	// quietly rather than loudly — the agent starts, and simply never has the
	// skills its own agentref says it was running.
	SkillsPlaceholder = "{{skills}}"
)

// Substitute fills the per-run placeholders in an agent argv.
//
// It refuses rather than guesses. A placeholder with no path behind it would
// otherwise become the empty string, and `--mount source=,target=` is not a
// narrower confinement than the one that was written — it is a different one
// nobody wrote. A path carrying a comma or a colon is refused for the same
// reason: those are the delimiters a mount argument is parsed on, so such a
// path would change the meaning of the argument rather than fill it.
func Substitute(argv []string, workspace, contextDir, skillsDir string) ([]string, error) {
	out := slices.Clone(argv)
	for _, s := range []struct{ placeholder, path, what string }{
		{WorkspacePlaceholder, workspace, "workspace"},
		{ContextPlaceholder, contextDir, "context directory"},
		{SkillsPlaceholder, skillsDir, "skills directory"},
	} {
		if !slices.ContainsFunc(out, func(a string) bool { return strings.Contains(a, s.placeholder) }) {
			continue
		}
		if s.path == "" {
			return nil, fmt.Errorf("confine: the agent argv names %s but this run has no %s", s.placeholder, s.what)
		}
		if !filepath.IsAbs(s.path) {
			return nil, fmt.Errorf("confine: %s %q is not an absolute path", s.what, s.path)
		}
		if strings.ContainsAny(s.path, ",:") {
			return nil, fmt.Errorf("confine: %s %q carries a comma or a colon, the delimiters a mount argument is parsed on", s.what, s.path)
		}
		for i := range out {
			out[i] = strings.ReplaceAll(out[i], s.placeholder, s.path)
		}
	}
	return out, nil
}

// Profile is a confinement: the image the agent runs in, and how much of the
// host goes in with it.
type Profile struct {
	// Image carries the agent. Required — an unnamed image is not a default,
	// it is a wrapper that cannot start.
	Image string

	// User is the uid:gid the agent runs as; empty means the daemon's own,
	// resolved when the profile is rendered.
	//
	// The default is load-bearing rather than polite. A bind-mounted workspace
	// keeps host ownership, docker's default user is root, and a workspace
	// full of root-owned files is a landing that fails after the tokens are
	// spent — the adapter commits what the agent left, and it cannot.
	User string

	// Env names environment variables forwarded into the container BY NAME —
	// the model credential above all. Values are never written here: a config
	// file is not a secret store, and a name is all docker needs.
	Env []string

	// Network is passed to --network when set. Empty leaves docker's default,
	// because an agent that cannot reach a model API does nothing. See the
	// package comment for what that means this does not confine.
	Network string
}

// Wrap renders the argv that runs agent under the profile, with the per-run
// paths still as placeholders for [Substitute].
//
// Exactly two host paths get in, and they are the two the runtime knows about:
// the workspace, writable, and the materialized context set, read-only. The
// factory's root is not among them, so the mirror, the store, and every other
// run's workspace are unreachable by absence rather than by permission — the
// distinction that matters, because a permission is something a process can be
// tricked into having and an unmounted path is not.
func (p Profile) Wrap(agent []string) ([]string, error) {
	if p.Image == "" {
		return nil, fmt.Errorf("confine: no image — there is nothing to run the agent in")
	}
	if len(agent) == 0 {
		return nil, fmt.Errorf("confine: no agent command to wrap")
	}
	user := p.User
	if user == "" {
		user = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	argv := []string{
		"docker", "run", "--rm",
		// The protocol is newline-delimited JSON-RPC over stdio, so stdin must
		// stay open. Not --tty: there is no terminal, and a pty would put
		// control bytes in the frame stream.
		"--interactive",
		// --mount rather than -v, deliberately: -v CREATES a missing source as
		// a root-owned directory and carries on, so a path that is wrong by one
		// character becomes an empty workspace the agent works in and nobody
		// can land. --mount refuses.
		"--mount", "type=bind,source=" + WorkspacePlaceholder + ",target=" + WorkspacePlaceholder,
		"--mount", "type=bind,source=" + ContextPlaceholder + ",target=" + ContextPlaceholder + ",readonly",
		"--mount", "type=bind,source=" + SkillsPlaceholder + ",target=" + SkillsPlaceholder + ",readonly",
		// The same path inside as out. The prompt names the context directory
		// by its host path and every tool call starts from the workspace, so a
		// remapped root would make both wrong.
		"--workdir", WorkspacePlaceholder,
		"--user", user,
	}
	if p.Network != "" {
		argv = append(argv, "--network", p.Network)
	}
	for _, name := range p.Env {
		if name == "" || strings.ContainsRune(name, '=') {
			return nil, fmt.Errorf("confine: env %q — a NAME, not an assignment; a value here would put a credential in the config file", name)
		}
		argv = append(argv, "--env", name)
	}
	argv = append(argv, p.Image)
	return append(argv, agent...), nil
}
