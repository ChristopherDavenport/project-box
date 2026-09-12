package acp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// ReviewerName is this gate's name in a [forge.Verdict] and in the audit
// trail. It is what a lane's gate list names to put a judgement in front of a
// landing.
const ReviewerName = "review"

// Reviewer is the occupant of the gate role: an agent run that reads a built
// change and answers pass or fail.
//
// The gate role has had a name and no occupant since the roles existed. Every
// other gate in this system judges by running something — a check exits zero
// or it does not — which answers whether the change WORKS and never whether it
// is the change that was asked for. That second question is what a human
// reviewer is for on a pull request, and a factory whose agents open their own
// pull requests has nobody standing there. This is that reader: it is handed
// the diff, the origination it is supposed to satisfy, the sections the unit
// declared, and the settled decisions of the repository as they stood in the
// tree the change was built on, and it says which of those the change honours.
//
// It is a [forge.Gate] and obeys that interface's hardest rule: trouble is
// never a rejection. An answer that did not parse, a work item that cannot be
// read, a workspace that will not produce a diff — each is an error, because a
// reviewer whose answer nobody could read said nothing about the change, and
// silence must never be recorded as either verdict. There is no pending
// answer: this gate judges synchronously or it fails to judge.
//
// Read-onlyness is asserted twice, deliberately. On disk the material is a
// 0555/0444 tree and the agent's working directory is that tree rather than
// the workspace, which is what actually stops it writing; in the policy the
// reviewer's own layer states "plan", so the mode a review ran under is on the
// record rather than inferred from a role default that a broader layer
// silently outranks.
type Reviewer struct {
	Store  store.Store
	Tenant tenant.ID
	Box    string
	// Command is the agent argv; Env is appended to its environment.
	Command []string
	Env     []string
	// EnvNames are additional names forwarded from the daemon's own
	// environment — see [Runtime.EnvNames].
	EnvNames []string
	// MaxTokens bounds one review, as it bounds one build.
	MaxTokens int64
	// Composer resolves the bundle under [agent.RoleGate]: the same harness
	// source the builders use, which is what makes the reviewer's tier a
	// role in box.yaml rather than a second configuration.
	Composer *agent.Composer
	// Seat gives the reviewer continuity: what one review learned about a
	// repository wakes the next one.
	Seat      *seat.Seat
	WakeNotes int
	// Repo is the repository under review, for its sections and its remote.
	Repo forge.Repo
	// Mirror materializes the bundle's skills beside the reading material,
	// as the Builder's and the Planner's do. Nil refuses a bundle that grants
	// any: the daemon always supplies one, and a reviewer assembled without
	// it must not run as if the skills had been offered.
	Mirror *gitforge.Mirror
	// Workspace is the BUILT tree — a clone holding both the base and the
	// head — and is the only place the diff can come from: the mirror was
	// never pushed the head, so `git archive` there cannot see it. It is the
	// source of the reading material and never the agent's cwd; a gate that
	// judged from inside the tree it judges could edit its way to a pass.
	Workspace forge.Workspace
	// Root is where the reading material is laid out, one directory per run,
	// reclaimed after it.
	Root string
	// InheritSettings, Spend, Progress and Transcript are as on [Builder].
	InheritSettings bool
	Spend           func(ctx context.Context, run string, u Usage)
	Progress        func(ctx context.Context, run string, u Update)
	Transcript      io.Writer
}

// Name satisfies [forge.Gate].
func (r *Reviewer) Name() string { return ReviewerName }

// maxDiffBytes bounds the patch a reviewer is handed. A change larger than
// this is shown its first part and told so, in the material and in the
// prompt: a reviewer that silently read half a change and passed it is
// precisely the failure this gate exists to prevent. A var rather than a
// const so a test can shrink it to something a fixture can exceed.
var maxDiffBytes = 256 << 10

// Evaluate lays out what the change has to be judged against, runs one agent
// over it, and returns the judgement it gave.
func (r *Reviewer) Evaluate(ctx context.Context, cs forge.ChangeSet) (forge.Verdict, error) {
	if len(cs.Members) == 0 {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: change set %s has no member to review", cs.ID)
	}
	m := cs.Members[0]
	if m.Base == "" || m.Head == "" {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %s proposes %s..%s — a review needs both ends of the change", cs.ID, m.Base, m.Head)
	}
	run := cs.Run
	if run == "" {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: change set %s names no run, so there is no work item to read it against", cs.ID)
	}
	// The item, not the change set: Sections, Plan and Node are what the unit
	// declared about itself and none of them ride a ChangeSet. An item that
	// cannot be read is an error and never a verdict — a review that did not
	// know what was asked has not reviewed anything.
	item, err := (&queue.Queue{Store: r.Store}).Load(ctx, r.Tenant, run)
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: reading work item %s: %w", run, err)
	}

	// Absolute, whatever the config's root was: the adapter refuses a
	// relative cwd, and the first live planning run found that out.
	dir, err := filepath.Abs(filepath.Join(r.Root, run))
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %w", err)
	}
	defer gitforge.RemoveContext(dir)
	mat, err := r.materialize(ctx, dir, cs, m, item)
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %w", err)
	}

	req := agent.Request{
		Tenant: r.Tenant, Role: agent.RoleGate, Box: r.Box, Repo: r.Repo.Name, Run: run,
		Task:  cs.Intent,
		Extra: []agent.Layer{reviewerLayer()},
	}
	if r.Seat != nil {
		wake, ok, err := r.Seat.WakeLayer(ctx, r.WakeNotes)
		if err != nil {
			return forge.Verdict{}, fmt.Errorf("acp: reviewer: wake layer: %w", err)
		}
		if ok {
			req.Extra = append(req.Extra, wake)
		}
	}
	bundle, err := r.Composer.Resolve(ctx, req)
	if err != nil {
		return forge.Verdict{}, err
	}

	// The unit's chain, not the origination's: a review is one more thing
	// that happened to this work item, and it belongs beside the build whose
	// output it read — the scope the loop and the builder already write to.
	rec := recorder{Store: r.Store, Tenant: r.Tenant, Scope: "workitem/" + run,
		Actor: r.seatName(), OnBehalfOf: item.Requester}
	layers := make([]layerRecord, 0, len(bundle.Layers))
	for _, l := range bundle.Layers {
		layers = append(layers, layerRecord{Scope: string(l.Scope), Name: l.Name, Source: string(l.Source.Kind), Digest: l.Source.Digest})
	}
	envelope := "(none declared)"
	if bundle.Envelope != nil {
		envelope = bundle.Envelope.String()
	}
	rec.json(ctx, "agent-run", bundle.Ref, runRecord{
		AgentRef: bundle.Ref, Role: string(agent.RoleGate), Mode: bundle.Tools.PermissionMode,
		Seat: r.seatName(), Envelope: envelope, Layers: layers,
	})

	// The same three paths every run gets, so a confining wrapper is told the
	// same things whether it wraps a build, a plan or a review.
	contextDir, skillsDir := forge.ContextDir(mat.Dir), forge.SkillsDir(mat.Dir)
	for _, d := range []string{contextDir, skillsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return forge.Verdict{}, err
		}
	}
	// A bundle that names skills and an agent that never received them is the
	// silent-divergence case the agentref exists to make impossible, and from
	// outside it looks exactly like a reviewer that chose not to use them.
	// So the skills are laid out beside the material, exactly as a build's
	// are, and a reviewer with no mirror to lay them out from refuses rather
	// than proceeding with a promise it cannot keep. The first landing
	// through this gate on the dogfood found the refusal and nothing behind
	// it: the box grants two skills to every role, and only this role had
	// no way to materialize them.
	loaded := "" // handed to the harness only when something is actually there
	if len(bundle.Skills) > 0 {
		if r.Mirror == nil {
			return forge.Verdict{}, fmt.Errorf("acp: reviewer: bundle grants %d skills and the reviewer has no mirror to materialize them", len(bundle.Skills))
		}
		wrote, err := r.Mirror.MaterializeSkills(ctx, skillsDir, bundle.Skills)
		if err != nil {
			return forge.Verdict{}, fmt.Errorf("acp: reviewer: %w", err)
		}
		if wrote {
			loaded = skillsDir
		}
	}

	sent := reviewerPrompt(bundle, cs.Intent, mat)
	explained, _ := r.Composer.Explain(ctx, bundle)
	rec.json(ctx, "agent-prompt", bundle.Ref, promptRecord{AgentRef: bundle.Ref, Prompt: sent, Bundle: string(explained)})

	rt := &Runtime{Command: r.Command, Dir: mat.Dir, ContextDir: "", Env: r.Env, EnvNames: r.EnvNames,
		MaxTokens: r.MaxTokens, Transcript: r.Transcript,
		SkillsDir: skillsDir, LoadedSkills: loaded, InheritSettings: r.InheritSettings,
		Progress: r.progress(ctx, run)}
	if r.Progress != nil {
		r.Progress(ctx, run, Update{Kind: UpdateStarted, Text: bundle.Ref})
	}
	started := time.Now()
	res, err := rt.Run(ctx, bundle, sent)
	elapsed := time.Since(started)
	// What the agent said it was, the moment it said so: the model that
	// judged is the one fact about a review that cannot be recovered later.
	if res.Session.Opened {
		rec.json(ctx, "agent-session", bundle.Ref, sessionRecord{
			AgentRef: bundle.Ref, Model: res.Session.Model(), Modes: res.Session.Modes,
			Config: res.Session.Config, Commands: res.Session.Commands,
			Credential: res.Usage.Credential(), Env: res.EnvNames,
			Session: res.Session.ID, Role: "reviewer",
		})
	}
	// The unit's own run key, not a run of the reviewer's own: a unit's cost
	// includes the cost of reviewing it, and the frames belong on the unit's
	// page beside the build they are judging.
	if r.Spend != nil && res.Prompted {
		r.Spend(ctx, run, res.Usage)
	}
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %w", err)
	}
	rec.raw(ctx, "agent-result", bundle.Ref, []byte(resultBody(res, elapsed)))
	rec.json(ctx, "agent-text", bundle.Ref, textRecord{AgentRef: bundle.Ref, Text: res.Text, StopReason: res.StopReason})
	if r.Seat != nil {
		if notes := handoffNotes(res.Text); len(notes) > 0 {
			// Bound to the built commit, unlike a plan's notes: this
			// judgement is about one specific tree, and a note that outlives
			// the knowledge of which one is a claim about nothing.
			reviewed := queue.WorkItem{ID: run, Tenant: r.Tenant, Box: r.Box, Requester: item.Requester, Head: string(m.Head)}
			if err := r.Seat.RecordHandoff(ctx, reviewed, r.Repo.Remote, notes); err != nil {
				return forge.Verdict{}, fmt.Errorf("acp: reviewer: recording the handoff: %w", err)
			}
		}
	}
	// An agent that declines to judge has not failed the change, so this is
	// an error like every other unreadable answer.
	if reason := escalation(res); reason != "" {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %s declined to judge %s: %s", r.seatName(), run, reason)
	}
	v, err := parseVerdict(res.Text)
	if err != nil {
		return forge.Verdict{}, fmt.Errorf("acp: reviewer: %w", err)
	}
	return v, nil
}

func (r *Reviewer) seatName() string {
	if r.Seat == nil {
		return "reviewer"
	}
	return r.Seat.Name
}

// progress adapts the run-tagged hook to the runtime's untagged one.
func (r *Reviewer) progress(ctx context.Context, run string) func(Update) {
	if r.Progress == nil {
		return nil
	}
	return func(u Update) { r.Progress(ctx, run, u) }
}

// material is the reading a review was laid out, and what is true about it.
type material struct {
	// Dir is the tree the agent is pointed at, read-only all the way down.
	Dir string
	// Base and Head are the ends of the diff, as the material names them.
	Base, Head string
	// PatchBytes is the size of the whole diff; Truncated says the agent was
	// shown only the first maxDiffBytes of it.
	PatchBytes int
	Truncated  bool
	// Decisions says whether the settled-decisions section was found in the
	// built tree. Absent is stated, never silently omitted.
	Decisions bool
	// Origination is what the unit was asked to do, as the material states
	// it. It rides the prompt as well as the file, because the file is
	// reclaimed with the run and the agent-prompt record is not: the facts a
	// judgement was made against have to be readable after the tree is gone,
	// and re-deriving them would need the same item and the same repository
	// configuration still saying the same things.
	Origination string
}

// materialize lays out what the change is judged against: the diff, the
// origination it is supposed to satisfy, and the decisions it was made
// against, as files nothing in the run can write.
//
// Every fact comes from the workspace rather than the mirror. A workspace is a
// clone that holds both ends of the change; the built head was never pushed,
// so the mirror has never seen it and cannot archive it.
func (r *Reviewer) materialize(ctx context.Context, dir string, cs forge.ChangeSet, m forge.Member, item queue.WorkItem) (material, error) {
	if r.Workspace.Dir == "" {
		return material{}, fmt.Errorf("no workspace to read the change out of")
	}
	root := filepath.Join(dir, "review")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return material{}, err
	}
	mat := material{Dir: root, Base: string(m.Base), Head: string(m.Head)}

	stat, err := r.git(ctx, "diff", "--stat", mat.Base, mat.Head)
	if err != nil {
		return material{}, err
	}
	patch, err := r.git(ctx, "diff", mat.Base, mat.Head)
	if err != nil {
		return material{}, err
	}
	mat.PatchBytes = len(patch)
	shown, cut := capPatch(patch, maxDiffBytes)
	mat.Truncated = cut
	if cut {
		shown = append(shown, fmt.Appendf(nil, "\n*** TRUNCATED — you have NOT been shown the whole change. ***\n"+
			"The diff of %s against %s is %d bytes; the %d bytes above are its first part, cut at a line boundary.\n"+
			"Do not pass a change you have only partly read: if what you were shown is not enough to judge, ESCALATE: and say so.\n",
			mat.Head, mat.Base, mat.PatchBytes, len(shown))...)
	}

	decisions, found, err := r.settled(ctx, mat.Head)
	if err != nil {
		return material{}, err
	}
	mat.Decisions = found

	mat.Origination = originationText(cs, m, item, r.Repo)
	files := map[string][]byte{
		"diff.stat":      stat,
		"diff.patch":     shown,
		"origination.md": []byte(mat.Origination),
		"decisions.md":   []byte(decisions),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
			return material{}, err
		}
	}

	// The built tree itself, so the reviewer can read around the diff rather
	// than judging a hunk with no surroundings. From the workspace, at the
	// head, as plain files: no .git, nothing to fetch past the pin with.
	if err := r.archive(ctx, filepath.Join(root, "tree"), mat.Head); err != nil {
		return material{}, err
	}

	// What actually stops this agent writing. The stated "plan" mode is the
	// same fact in the policy; neither is trusted to be the only one.
	if err := sealReadOnly(root); err != nil {
		return material{}, err
	}
	return mat, nil
}

// git runs one read of the workspace.
func (r *Reviewer) git(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Workspace.Dir
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), r.Workspace.Dir, err, strings.TrimSpace(errb.String()))
	}
	return out, nil
}

// archive extracts the workspace's tree at commit into dest as plain files.
func (r *Reviewer) archive(ctx context.Context, dest, commit string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tar := exec.CommandContext(ctx, "tar", "-x", "-C", dest)
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit)
	archive.Dir = r.Workspace.Dir
	var errb bytes.Buffer
	archive.Stderr = &errb
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	tar.Stdin = pipe
	if err := tar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s in %s: %w: %s", commit, r.Workspace.Dir, err, strings.TrimSpace(errb.String()))
	}
	if err := tar.Wait(); err != nil {
		return fmt.Errorf("extracting %s: %w", commit, err)
	}
	return nil
}

// settled reads the decisions the change was made against out of the BUILT
// tree, so the reviewer holds the §6 that was in force at the head rather than
// whatever it says on the default branch by the time the review runs.
//
// A repository with no such document has none, which the material says in so
// many words. An absent section read as an empty one would let a change
// contradict every decision there is and be told it contradicted nothing.
func (r *Reviewer) settled(ctx context.Context, head string) (string, bool, error) {
	out, err := r.git(ctx, "show", head+":docs/PLAN.md")
	if err != nil {
		return fmt.Sprintf("# Settled decisions\n\nThis repository has no docs/PLAN.md at %s, so there is no settled-decisions section to read this change against. Judge it against the origination alone, and say so in your reason.\n", head), false, nil
	}
	section, ok := settledSection(string(out))
	if !ok {
		return fmt.Sprintf("# Settled decisions\n\ndocs/PLAN.md exists at %s and has no \"Decisions already settled\" heading. Judge this change against the origination alone, and say so in your reason.\n", head), false, nil
	}
	return "# Settled decisions, from docs/PLAN.md at " + head + "\n\n" + section + "\n", true, nil
}

// settledSection cuts the settled-decisions heading out of a PLAN document:
// the heading through the line before the next one at the same level.
func settledSection(doc string) (string, bool) {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") && strings.Contains(l, "Decisions already settled") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n"), true
}

// capPatch bounds the patch at a line boundary, so the last hunk the reviewer
// sees is a whole line rather than half of one.
func capPatch(patch []byte, max int) ([]byte, bool) {
	if len(patch) <= max {
		return patch, false
	}
	cut := patch[:max]
	if i := bytes.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i+1]
	}
	return bytes.Clone(cut), true
}

// sealReadOnly is the posture a materialized context set has, applied to a
// tree this package laid out itself: 0555 directories, 0444 files.
// [gitforge.RemoveContext] reclaims it, restoring write on the way down.
func sealReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(path, 0o555)
		}
		return os.Chmod(path, 0o444)
	})
}

// originationText is what the unit was asked to do, in the reviewer's own
// reading material: the task verbatim, where the unit was allowed to express
// itself, and the commits it moved.
func originationText(cs forge.ChangeSet, m forge.Member, item queue.WorkItem, repo forge.Repo) string {
	var w strings.Builder
	w.WriteString("## What this unit was asked to do\n\n")
	fmt.Fprintf(&w, "- Work item: %s\n", item.ID)
	fmt.Fprintf(&w, "- Box: %s\n", cs.Box)
	fmt.Fprintf(&w, "- Repository: %s\n", m.Repo)
	fmt.Fprintf(&w, "- Branch: %s\n", m.Branch)
	fmt.Fprintf(&w, "- Base: %s\n", m.Base)
	fmt.Fprintf(&w, "- Head: %s\n", m.Head)
	fmt.Fprintf(&w, "- Summary: %s\n", orNone(m.Summary))
	fmt.Fprintf(&w, "- Plan: %s\n", orNone(item.Plan))
	fmt.Fprintf(&w, "- Node in that plan: %s\n", orNone(item.Node))

	w.WriteString("\n### Where it was allowed to express itself\n\n")
	if len(item.Sections) == 0 {
		fmt.Fprintf(&w, "This unit declares no section: it is the whole of %s, and every path in the diff is within its reach.\n", m.Repo)
	} else {
		w.WriteString("A change reaching outside these paths is outside what the unit declared, and that is a failing.\n\n")
		for _, name := range item.Sections {
			s, ok := repo.Section(name)
			if !ok {
				fmt.Fprintf(&w, "- %s — NOT DECLARED by repository %s (declared: %s)\n", name, m.Repo, strings.Join(repo.SectionNames(), ", "))
				continue
			}
			fmt.Fprintf(&w, "- %s", s.Name)
			if s.Purpose != "" {
				fmt.Fprintf(&w, " — %s", s.Purpose)
			}
			fmt.Fprintf(&w, " (paths: %s)\n", strings.Join(s.Paths, ", "))
		}
	}
	fmt.Fprintf(&w, "\n### The task, verbatim\n\n%s\n", cs.Intent)
	return w.String()
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// reviewerContract is what a judgement is, in the reviewer's own instructions.
const reviewerContract = `## You are the gate
You are reviewing a change that has already been built and has already passed its build and test check. Nothing else stands between it and the default branch: your judgement is the last one, and there is no reviewer after you.

Your working directory is your reading material, and it is read-only:
- origination.md — what this unit was asked to do, where it was allowed to express itself, and the commits it moved.
- diff.stat and diff.patch — the change itself, head against base.
- decisions.md — the decisions this repository had already settled, taken from the tree the change was built on.
- tree/ — the built tree at the head, so you can read around a hunk instead of judging it alone.

Fail a change that:
- does not do what the origination asked, or does something else as well;
- contradicts a settled decision without saying so in its commit message — contradicting one silently is wrong even when it compiles;
- reaches outside the paths the unit declared;
- asserts a property that nothing tests, where the property is the point of the change.
Pass a change that does what was asked, within its declared paths, consistently with the settled decisions. A style preference is not a failing, and neither is a change you would have written differently.

## Your answer
Reply with ONE JSON object as the last thing in your message:

{"verdict":"pass","reason":"one paragraph"}

- verdict is exactly "pass" or "fail". Nothing else is an answer.
- reason says why, in the terms above. It is required on a fail: a rejection nobody can act on is not a rejection.
- If you cannot judge — the material is missing, the diff is unreadable, the origination and the change are about different things and you cannot tell which is wrong — reply with a single line starting with ESCALATE: and the reason. Never guess a verdict: the absence of a judgement is not a pass, and it will be recorded as trouble rather than as either answer.
`

// reviewerLayer is the reviewer's contract as an inline layer of the role's
// scope, and the place the permission mode is stated.
//
// Stating it matters. [agent.Composer] applies a role's restrictive default
// only when NO layer states a mode, and a base layer that says acceptEdits for
// every role means one always does — so a gate-role bundle would resolve to
// acceptEdits and the read-onlyness would rest entirely on the tree's
// permissions. "plan" ranks first in restrictiveness, so a stated one wins
// over anything broader, and the mode a review ran under lands on the
// agent-run record as a fact rather than as an assumption.
func reviewerLayer() agent.Layer {
	sum := sha256.Sum256([]byte(reviewerContract))
	return agent.Layer{
		Name: "reviewer", Scope: agent.ScopeRole,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: hex.EncodeToString(sum[:])},
		Instructions: reviewerContract,
		Tools:        agent.ToolPolicy{PermissionMode: "plan"},
	}
}

// reviewerPrompt is the composed instructions, the task once, and what this
// particular run was actually laid out — including, when the change was too
// large to send whole, that it was not sent whole.
func reviewerPrompt(bundle agent.Bundle, task string, mat material) string {
	var w strings.Builder
	w.WriteString(taskOnce(bundle, task))
	fmt.Fprintf(&w, "\n%s\n", mat.Origination)
	w.WriteString("## The change under review\n")
	fmt.Fprintf(&w, "It is %s against %s, in this directory as diff.stat and diff.patch.\n", mat.Head, mat.Base)
	if mat.Truncated {
		fmt.Fprintf(&w, "THE PATCH IS TRUNCATED. The whole diff is %d bytes and you were given only its first part; diff.patch says where it was cut. You have not read the whole change, so do not pass it on the strength of the part you did read — ESCALATE: if what you were shown is not enough to judge.\n", mat.PatchBytes)
	}
	if !mat.Decisions {
		w.WriteString("This repository states no settled decisions at this commit; decisions.md says so. Judge against the origination alone and say so in your reason.\n")
	}
	w.WriteString(`
## Working contract
- Read only. Do not edit, create, or commit anything: judging a change is not making one.
- End your message with the JSON object described above, or with an ESCALATE: line. Nothing after it.
- End with lines starting with "HANDOFF: " carrying anything the next review of this repository should know.
`)
	return w.String()
}

// reviewAnswer is the shape the reviewer replies in.
type reviewAnswer struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// parseVerdict reads the judgement out of the agent's message.
//
// Everything that is not one of the two words is an error, never a fail. A
// [forge.Gate] must not record infrastructure trouble as a rejected change,
// and an answer that did not parse said nothing whatsoever about the change:
// reading it as a rejection would blame a builder for a reviewer's silence,
// and reading it as a pass would land unreviewed work.
func parseVerdict(text string) (forge.Verdict, error) {
	obj, ok := lastObjectWith(text, `"verdict"`)
	if !ok {
		return forge.Verdict{}, fmt.Errorf("the answer carries no judgement — no JSON object with \"verdict\" — and no ESCALATE: line; an absent judgement is not a pass")
	}
	var a reviewAnswer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return forge.Verdict{}, fmt.Errorf("the judgement does not parse: %w", err)
	}
	reason := strings.TrimSpace(a.Reason)
	switch strings.TrimSpace(a.Verdict) {
	case "pass":
		return forge.Verdict{Gate: ReviewerName, Decision: forge.DecisionPass, Reason: reason}, nil
	case "fail":
		if reason == "" {
			return forge.Verdict{}, fmt.Errorf("the judgement fails the change and gives no reason; a rejection the builder cannot act on is not a rejection")
		}
		return forge.Verdict{Gate: ReviewerName, Decision: forge.DecisionFail, Reason: reason}, nil
	default:
		return forge.Verdict{}, fmt.Errorf("the judgement says %q, which is neither \"pass\" nor \"fail\"", a.Verdict)
	}
}
