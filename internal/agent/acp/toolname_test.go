package acp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestToolsAreNamedByTheHarnessNotByTheirProse.
//
// Measured across four real dogfood runs: 232 tool calls, of which 91 arrived
// with no title at all, and one run of 53 calls produced 46 "distinct tools"
// because every Bash invocation was keyed on its own command line. Neither
// number is a tally an operator can use to answer where the tokens went, and
// the same string was what the policy matched on.
//
// toolName is the stable identifier the dialect has been carrying all along.
func TestToolsAreNamedByTheHarnessNotByTheirProse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "metaname").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("tool calls = %v, want two", res.ToolCalls)
	}
	for _, got := range res.ToolCalls {
		if got != "Bash" {
			t.Errorf("tool named %q, want the stable harness name %q", got, "Bash")
		}
	}
	if res.Unenforced != 0 {
		t.Errorf("Unenforced = %d; both calls named themselves", res.Unenforced)
	}
}

// TestADeniedToolNamedOnlyInMetadataIsStillDenied is the hole this closes.
// tool_call_update never carries a title, and the enforcement path skipped
// anything untitled — so a denied tool announced the way the real adapter
// announces tool calls was not checked at all.
func TestADeniedToolNamedOnlyInMetadataIsStillDenied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := runtimeFor(t, "metadenied").Run(ctx, bundle(), "do the work")
	if !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("err = %v, want ErrPolicyViolation: a denied tool named in _meta walked through", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the deadline ended the run, not the policy")
	}
}

// TestUnmatchableCallsAreCountedNotShrugged. When neither a title nor a
// toolName is present there is genuinely nothing to match, and the honest
// report of that is a number: "some calls were unchecked" is a caveat, "3 of
// them were" is a finding.
func TestUnmatchableCallsAreCountedNotShrugged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := runtimeFor(t, "nameless").Run(ctx, bundle(), "do the work")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Unenforced != 3 {
		t.Fatalf("Unenforced = %d, want 3", res.Unenforced)
	}
	if !slices.Contains(res.UnknownUpdates, "tool_call:unnamed") {
		t.Fatalf("the dialect gap went unremarked: %v", res.UnknownUpdates)
	}
}

// TestTheRecordSaysWhereTheTokensWent. The runtime has parsed all four token
// figures since the field names were pinned, and then dropped them at the
// record site — leaving "20,076,747 tokens" as the entire account of a $13.60
// run. Cache reads are the bulk of an agent re-reading a repository and are
// priced an order of magnitude below input, so the total alone cannot explain
// a cost: four dogfood runs came in between $0.68 and $1.54 per million and
// nothing on the chain could say why.
func TestTheRecordSaysWhereTheTokensWent(t *testing.T) {
	s, lp, router, _, _ := rig(t, "metered")
	ctx := context.Background()
	// This scenario reports its cost and then changes nothing, so the working
	// contract parks it. That is the right outcome and beside the point here:
	// the cost is recorded before the contract is judged, because a run that
	// spent money and then failed still spent it.
	runItem(t, s, lp, router, "issue-split", "spend something")
	body := ""
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-split", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind == "agent-result" {
			body = string(r.Body)
		}
	}
	if body == "" {
		t.Fatal("no agent-result on the chain")
	}
	// The metered scenario reports 1200 in, 300 out, 9000 cache-read, 500
	// cache-write. Reading only the total (11000) would look entirely
	// plausible and would hide that 86% of it was cache.
	for _, want := range []string{
		`"tokens":11000`,
		`"input_tokens":1200`,
		`"output_tokens":300`,
		`"cached_read_tokens":9000`,
		`"cached_write_tokens":500`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("agent-result missing %s:\n%s", want, body)
		}
	}
}

// TestThePromptIsReadableAfterTheRun.
//
// The agentref is a sha256 of the bundle: it settles whether two runs had the
// same instructions and can never show you either of them. Nothing anywhere
// held the text, so the most consequential input in the system — the thing
// that decides what an agent does with twenty million tokens — was the one
// input nobody could read back. Composer.Explain had rendered exactly this
// since Phase 2 and had no caller.
func TestThePromptIsReadableAfterTheRun(t *testing.T) {
	s, lp, router, _, _ := rig(t, "worker")
	ctx := context.Background()
	runItem(t, s, lp, router, "issue-prompt", "make the change")

	var prompt, bundleText, agentText string
	for r, err := range s.Log().Since(ctx, btn, "workitem/wi-issue-prompt", 1) {
		if err != nil {
			t.Fatal(err)
		}
		switch r.Kind {
		case "agent-prompt":
			var p struct{ Prompt, Bundle string }
			if err := json.Unmarshal(r.Body, &p); err != nil {
				t.Fatal(err)
			}
			prompt, bundleText = p.Prompt, p.Bundle
		case "agent-text":
			var tx struct{ Text string }
			if err := json.Unmarshal(r.Body, &tx); err != nil {
				t.Fatal(err)
			}
			agentText = tx.Text
		}
	}

	// The literal text, not a digest of it: the task, and the contract that
	// every run in this repository has executed under.
	for _, want := range []string{"make the change", "## Task", "## Working contract", "_box/escalate"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the recorded prompt is missing %q:\n%s", want, prompt)
		}
	}
	// Once. The composer ends the instructions with the task layer and the
	// prompt used to append the task again under its heading, so every
	// builder prompt carried the whole issue twice — re-read on every turn.
	if n := strings.Count(prompt, "make the change"); n != 1 {
		t.Errorf("the task appears %d times in the prompt, want once:\n%s", n, prompt)
	}
	// And where it came from — layers with their digests, the tool policy,
	// which is what makes the agentref checkable against content rather than
	// merely comparable to itself.
	for _, want := range []string{"agentref:", "layers (in application order):", "tools:", "instructions:"} {
		if !strings.Contains(bundleText, want) {
			t.Errorf("the bundle explanation is missing %q:\n%s", want, bundleText)
		}
	}
	if agentText == "" {
		t.Error("the agent's own account of the run was not recorded")
	}
}
