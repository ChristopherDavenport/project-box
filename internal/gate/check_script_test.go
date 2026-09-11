package gate

import (
	"os"
	"regexp"
	"testing"
)

// checkScriptPath and ciWorkflowPath mirror the sibling-of-the-repo-root
// convention internal/boxcfg/dogfood_test.go uses for hack/dogfood/box.yaml:
// read the checked-in file rather than re-describe it, so a change to
// either is caught here instead of discovered at the gate.
const (
	checkScriptPath = "../../hack/check.sh"
	ciWorkflowPath  = "../../.github/workflows/ci.yml"
)

// checkScriptLegs are the legs hack/check.sh runs, in ci.yml's order. A leg
// present in the script with no matching "FAIL <name>" line is exactly the
// refusal issue #68 exists to prevent: the script exits non-zero, but the
// gate's verdict — which keeps only lines isFailureLine matches — has
// nothing to show for it.
var checkScriptLegs = []string{"generate", "gofmt", "vet", "build", "test", "govulncheck"}

var failLineRe = regexp.MustCompile(`FAIL [A-Za-z0-9_-]+`)

// TestCheckScriptFailureLinesAreKept pins that every leg hack/check.sh runs
// prints, on failure, a line the gate's verdict actually keeps
// (isFailureLine) — and that every leg in checkScriptLegs has one, not just
// that the lines present happen to be kept. A prior version of this change
// added the shape for five legs and left generate silent; that gap is what
// this second assertion exists to catch.
func TestCheckScriptFailureLinesAreKept(t *testing.T) {
	script, err := os.ReadFile(checkScriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", checkScriptPath, err)
	}
	content := string(script)

	matches := failLineRe.FindAllString(content, -1)
	if len(matches) == 0 {
		t.Fatal("no FAIL lines found in hack/check.sh")
	}
	for _, m := range matches {
		if !isFailureLine(m) {
			t.Errorf("the gate's verdict would discard this line the script prints on failure: %q", m)
		}
	}

	for _, leg := range checkScriptLegs {
		want := "FAIL " + leg
		found := false
		for _, m := range matches {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("leg %q has no %q line in hack/check.sh — its failure would exit non-zero with nothing the gate's verdict keeps", leg, want)
		}
	}
}

// goCommandRunRe matches a ci.yml step that invokes a go command directly,
// either as a one-line `run: go ...` step or as a `go ...` line inside a
// multi-line run block — the two spellings a run step can take. This is the
// shape that let CI and the gate check different lists of things in the
// first place (issue #68): a go command living in ci.yml instead of in
// hack/check.sh.
var goCommandRunRe = regexp.MustCompile(`(?m)^\s*-?\s*(?:run:\s*)?go\s+(generate|build|vet|test|run)\b`)

// TestCICallsCheckScript pins issue #68's anti-drift claim: ci.yml's checks
// job invokes hack/check.sh, and invokes no go command directly in a run
// step. Without this, ci.yml could regain a seventh check.sh never runs —
// or worse, a go command check.sh already runs, at a different spelling —
// and nothing would say so.
func TestCICallsCheckScript(t *testing.T) {
	wf, err := os.ReadFile(ciWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", ciWorkflowPath, err)
	}
	content := string(wf)

	if !regexp.MustCompile(`(?m)^\s*-?\s*run:\s*\./hack/check\.sh\s*$`).MatchString(content) {
		t.Error("ci.yml does not invoke ./hack/check.sh")
	}
	if m := goCommandRunRe.FindString(content); m != "" {
		t.Errorf("ci.yml runs a go command directly (%q) alongside ./hack/check.sh — CI and the gate can drift again", m)
	}
}
