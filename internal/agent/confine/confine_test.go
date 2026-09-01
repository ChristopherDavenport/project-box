package confine

import (
	"slices"
	"strings"
	"testing"
)

func profile() Profile {
	// User pinned rather than defaulted, so the render is the same on every
	// machine; the default itself is asserted separately.
	return Profile{Image: "box/agent:1", User: "1000:1000"}
}

// TestExactlyTwoHostPathsGetIn is the whole confinement in one assertion. The
// property is not "the mirror is denied" — it is that the only host paths
// named anywhere in the wrapper are the two the runtime substitutes, so
// everything else is unreachable by absence rather than by a rule someone has
// to keep correct.
func TestExactlyTwoHostPathsGetIn(t *testing.T) {
	argv, err := profile().Wrap([]string{"claude-agent-acp"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	var mounts []string
	for i, a := range argv {
		if a == "--mount" || a == "-v" || a == "--volume" {
			if i+1 >= len(argv) {
				t.Fatalf("%s with no argument", a)
			}
			mounts = append(mounts, argv[i+1])
		}
	}
	if len(mounts) != 2 {
		t.Fatalf("mounts = %v, want exactly two: the workspace and the context set", mounts)
	}
	if want := "type=bind,source=" + WorkspacePlaceholder + ",target=" + WorkspacePlaceholder; mounts[0] != want {
		t.Errorf("workspace mount = %q, want %q", mounts[0], want)
	}
	if want := "type=bind,source=" + ContextPlaceholder + ",target=" + ContextPlaceholder + ",readonly"; mounts[1] != want {
		t.Errorf("context mount = %q, want %q — granted reading material is read-only or it is not a context set", mounts[1], want)
	}

	// Nothing in the wrapper names a host path of its own. Everything from the
	// image onward runs INSIDE the container, so only the prefix is checked.
	image := slices.Index(argv, "box/agent:1")
	if image < 0 {
		t.Fatal("the image is not in the argv")
	}
	for _, a := range argv[:image] {
		if strings.HasPrefix(a, "/") {
			t.Errorf("wrapper argument %q is a host path that is neither placeholder", a)
		}
	}

	// stdio is the protocol: without --interactive the agent's stdin is closed
	// and the session cannot start.
	if !slices.Contains(argv, "--interactive") {
		t.Error("no --interactive: ACP is newline-delimited JSON-RPC over stdio")
	}
	if slices.Contains(argv, "--tty") || slices.Contains(argv, "-t") {
		t.Error("a tty would put control bytes in the frame stream")
	}
	// The agent argv is the tail, unmangled.
	if got := argv[len(argv)-1]; got != "claude-agent-acp" {
		t.Errorf("agent command = %q, want it last and untouched", got)
	}
}

// TestTheWorkspaceStaysLandable: docker's default user is root, a bind mount
// keeps host ownership, and root-owned files in the workspace are a landing
// that fails after the tokens are spent. So an unset user resolves to the
// daemon's own rather than to docker's.
func TestTheWorkspaceStaysLandable(t *testing.T) {
	argv, err := Profile{Image: "box/agent:1"}.Wrap([]string{"claude-agent-acp"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	i := slices.Index(argv, "--user")
	if i < 0 || i+1 >= len(argv) {
		t.Fatal("no --user: the agent would write root-owned files into the workspace")
	}
	if !strings.Contains(argv[i+1], ":") {
		t.Errorf("--user %q, want uid:gid", argv[i+1])
	}
}

func TestWrapRefusesWhatWouldBeAHoleRatherThanAConfinement(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Profile
		argv []string
	}{
		{"no image", Profile{User: "1000:1000"}, []string{"claude-agent-acp"}},
		{"no agent", profile(), nil},
		{"env carries a value", Profile{Image: "i", User: "1000:1000", Env: []string{"ANTHROPIC_API_KEY=sk-live"}}, []string{"a"}},
		{"env is empty", Profile{Image: "i", User: "1000:1000", Env: []string{""}}, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := tc.p.Wrap(tc.argv); err == nil {
				t.Fatalf("wrapped anyway: %v", got)
			}
		})
	}
}

func TestEnvIsForwardedByNameOnly(t *testing.T) {
	argv, err := Profile{Image: "i", User: "1000:1000", Env: []string{"ANTHROPIC_API_KEY"}}.Wrap([]string{"a"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	i := slices.Index(argv, "--env")
	if i < 0 || argv[i+1] != "ANTHROPIC_API_KEY" {
		t.Fatalf("argv = %v, want the credential forwarded by name", argv)
	}
}

func TestSubstituteFillsBothPaths(t *testing.T) {
	argv, err := profile().Wrap([]string{"claude-agent-acp"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	got, err := Substitute(argv, "/forge/work/acme/run-1/app", "/forge/work/acme/run-1/app.context")
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	for _, a := range got {
		if strings.Contains(a, "{{") {
			t.Fatalf("placeholder survived substitution: %q", a)
		}
	}
	if !slices.Contains(got, "type=bind,source=/forge/work/acme/run-1/app.context,target=/forge/work/acme/run-1/app.context,readonly") {
		t.Fatalf("context mount not filled in: %v", got)
	}
}

// TestSubstituteRefusesRatherThanGuesses: every one of these would produce a
// wrapper that starts and confines something other than what was written,
// which is worse than one that refuses to start.
func TestSubstituteRefusesRatherThanGuesses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		argv       []string
		ws, ctxDir string
	}{
		{"no context for a placeholder that names one",
			[]string{"docker", "--mount", "source=" + ContextPlaceholder}, "/ws", ""},
		{"no workspace at all",
			[]string{"docker", "--workdir", WorkspacePlaceholder}, "", "/ctx"},
		{"a relative path",
			[]string{"docker", "--workdir", WorkspacePlaceholder}, "work/run-1/app", "/ctx"},
		{"a comma, which is what --mount splits on",
			[]string{"docker", "--mount", "source=" + WorkspacePlaceholder + ",target=" + WorkspacePlaceholder}, "/forge/a,readonly/app", "/ctx"},
		{"a colon, which is what -v splits on",
			[]string{"docker", "--mount", "source=" + WorkspacePlaceholder}, "/forge/a:b/app", "/ctx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := Substitute(tc.argv, tc.ws, tc.ctxDir); err == nil {
				t.Fatalf("substituted anyway: %v", got)
			}
		})
	}
}

// A path this runtime never substitutes is left exactly as written: an
// operator's own wrapper must not be second-guessed.
func TestSubstituteLeavesAnArgvItDoesNotRecognise(t *testing.T) {
	argv := []string{"sandbox-exec", "-f", "/etc/box/agent.sb", "claude-agent-acp"}
	got, err := Substitute(argv, "", "")
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if !slices.Equal(got, argv) {
		t.Fatalf("argv = %v, want it untouched", got)
	}
}
