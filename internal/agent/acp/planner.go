package acp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	gitforge "github.com/ChristopherDavenport/project-box/internal/forge/git"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/loop"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/seat"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// PlannerName is the decomposer a kind declaration opts into judgement with:
//
//	kinds:
//	  - name: issue
//	    decompose: {decomposer: planner}
const PlannerName = "planner"

// Planner is a judgement decomposer: it turns one origination into a graph of
// units — what each is, where in the repository it is expressed, what it
// waits on, and the tier its difficulty allows — by running an agent under the
// planner role over a read-only tree and asserting its answer.
//
// It is not a coordinator. The coordinator-shaped design keeps a standing
// session with a home directory and a name, dispatches work from it, and
// remembers the fleet in a context window nobody can read back. Here the
// decomposition is a unit of expression like any other: one run, one
// agentref, one plan record on the origination's chain, carrying the model
// that judged and every fact it read. The continuity a coordinator has by
// living long, a planner has by seat — its handoff notes wake the next plan,
// and they are on the record.
//
// It is opt-in per kind, and the reason is #5's invariant. Intake runs on
// every origination, so a judgement on the default path would put an agent on
// the critical path of admitting work. Declared, it sits in a seat whose
// failure parks the one origination that asked for it — see [plan.Store.Fail].
type Planner struct {
	Store  store.Store
	Tenant tenant.ID
	Box    string
	// Command is the agent argv; Env is appended to its environment.
	Command []string
	Env     []string
	// EnvNames are additional names forwarded from the daemon's own
	// environment — see [Runtime.EnvNames].
	EnvNames []string
	// MaxTokens bounds one planning run, as it bounds one build.
	MaxTokens int64
	// Composer resolves the bundle under [agent.RolePlanner]: the same
	// harness source the builders use, which is what makes the planner's
	// tier a role in box.yaml rather than a second configuration.
	Composer *agent.Composer
	// Runner is the declared runner NAME this plan was given — see
	// [agent.Request.Runner]. Wired by the daemon; unset in production for
	// now.
	Runner string
	// Seat gives the planner continuity: its handoff notes wake the next
	// plan. Nil plans cold every time.
	Seat      *seat.Seat
	WakeNotes int
	// Mirror holds the repositories; Repos are the box's, with their
	// sections — the catalogue the planner chooses from.
	Mirror *gitforge.Mirror
	Repos  []forge.Repo
	// Root is where the read-only trees a plan is judged over are laid out,
	// one per origination, reclaimed after the run.
	Root string
	// InheritSettings, Spend and Transcript are as on [Builder].
	InheritSettings bool
	Spend           func(ctx context.Context, run string, u Usage)
	// Progress, when set, receives the judgement's stream as it arrives,
	// tagged with the run — "plan-<ingress>" — exactly as [Builder.Progress]
	// tags a unit's. A planner that judged for eight minutes with nothing
	// on any surface was the first thing round 6 taught about the room.
	Progress   func(ctx context.Context, run string, u Update)
	Transcript io.Writer
}

// Name satisfies [plan.Decomposer].
func (p *Planner) Name() string { return PlannerName }

// Replayable satisfies [plan.Decomposer]: a judgement is not.
func (p *Planner) Replayable() bool { return false }

// Decompose runs one planning session and returns the graph it judged.
func (p *Planner) Decompose(ctx context.Context, ing ingress.Ingress, decl kind.Declaration) (plan.Result, error) {
	repo, err := p.repo(ing.Repo)
	if err != nil {
		return plan.Result{}, err
	}
	run := "plan-" + ing.ID
	if err := p.Mirror.Fetch(ctx, repo); err != nil {
		return plan.Result{}, fmt.Errorf("acp: planner: fetch %s: %w", repo.Name, err)
	}
	head, err := p.Mirror.Resolve(ctx, repo, "refs/heads/"+repo.DefaultBranch)
	if err != nil {
		return plan.Result{}, fmt.Errorf("acp: planner: resolve %s: %w", repo.Name, err)
	}
	// The tree the judgement is made over: read-only, at the exact commit
	// the plan's facts name, reclaimed afterwards. A planner that could
	// write would be a builder with a different name.
	// Absolute, whatever the config's root was: the adapter refuses a
	// relative cwd, and the first live planning run found out.
	dir, err := filepath.Abs(filepath.Join(p.Root, run))
	if err != nil {
		return plan.Result{}, fmt.Errorf("acp: planner: %w", err)
	}
	if err := p.Mirror.MaterializeContext(ctx, dir, agent.ContextSet{Repos: []agent.ContextRepo{
		{Repo: repo.Name, Commit: string(head)},
	}}); err != nil {
		return plan.Result{}, fmt.Errorf("acp: planner: materializing %s@%s: %w", repo.Name, head, err)
	}
	defer gitforge.RemoveContext(dir)
	tree := filepath.Join(dir, repo.Name)

	req := agent.Request{
		Tenant: p.Tenant, Role: agent.RolePlanner, Box: p.Box, Repo: repo.Name, Run: run,
		Task: plan.TaskOf(ing), Envelope: decl.Envelope, Runner: p.Runner,
		Extra: []agent.Layer{sectionsLayer(repo)},
	}
	if p.Seat != nil {
		wake, ok, err := p.Seat.WakeLayer(ctx, p.WakeNotes)
		if err != nil {
			return plan.Result{}, fmt.Errorf("acp: planner: wake layer: %w", err)
		}
		if ok {
			req.Extra = append(req.Extra, wake)
		}
	}
	bundle, err := p.Composer.Resolve(ctx, req)
	if err != nil {
		return plan.Result{}, err
	}
	rec := recorder{Store: p.Store, Tenant: p.Tenant, Scope: ingress.Scope(ing.ID), Actor: p.seatName(), OnBehalfOf: ing.Requester}
	layers := make([]layerRecord, 0, len(bundle.Layers))
	for _, l := range bundle.Layers {
		layers = append(layers, layerRecord{Scope: string(l.Scope), Name: l.Name, Source: string(l.Source.Kind), Digest: l.Source.Digest})
	}
	envelope := "(none declared)"
	if bundle.Envelope != nil {
		envelope = bundle.Envelope.String()
	}
	rec.json(ctx, "agent-run", bundle.Ref, runRecord{
		AgentRef: bundle.Ref, Role: string(agent.RolePlanner), Mode: bundle.Tools.PermissionMode,
		Seat: p.seatName(), Envelope: envelope, Layers: layers, Runner: bundle.Runner,
	})

	// The same three paths every run gets, so a confining wrapper is told
	// the same things whether it wraps a build or a plan.
	contextDir, skillsDir := forge.ContextDir(tree), forge.SkillsDir(tree)
	for _, d := range []string{contextDir, skillsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return plan.Result{}, err
		}
	}
	loaded := ""
	if len(bundle.Skills) > 0 {
		wrote, err := p.Mirror.MaterializeSkills(ctx, skillsDir, bundle.Skills)
		if err != nil {
			return plan.Result{}, err
		}
		if wrote {
			loaded = skillsDir
		}
	}
	sent := plannerPrompt(bundle, req.Task)
	explained, _ := p.Composer.Explain(ctx, bundle)
	rec.json(ctx, "agent-prompt", bundle.Ref, promptRecord{AgentRef: bundle.Ref, Prompt: sent, Bundle: string(explained)})

	rt := &Runtime{Command: p.Command, Dir: tree, ContextDir: "", Env: p.Env, EnvNames: p.EnvNames,
		MaxTokens: p.MaxTokens, Transcript: p.Transcript,
		SkillsDir: skillsDir, LoadedSkills: loaded, InheritSettings: p.InheritSettings,
		Progress: p.progress(ctx, run, bundle.Ref)}
	if p.Progress != nil {
		p.Progress(ctx, run, Update{Kind: UpdateStarted, Text: bundle.Ref, Session: bundle.Ref})
	}
	started := time.Now()
	res, err := rt.Run(ctx, bundle, sent)
	elapsed := time.Since(started)
	if res.Session.Opened {
		rec.json(ctx, "agent-session", bundle.Ref, sessionRecord{
			AgentRef: bundle.Ref, Model: res.Session.Model(), Modes: res.Session.Modes,
			Config: res.Session.Config, Commands: res.Session.Commands,
			Credential: res.Usage.Credential(), Env: res.EnvNames,
			Session: res.Session.ID, Role: "planner", Runner: bundle.Runner,
		})
	}
	if p.Spend != nil && res.Prompted {
		p.Spend(ctx, run, res.Usage)
	}
	if err != nil {
		// The one seam where a planner error means the run never answered: a
		// transport failure, a limit refusal, a mid-response server error. The
		// loop side (pump.intakeOne) retries and defers on [plan.ErrNoAnswer]
		// rather than parking an exception for a run nobody judged.
		//
		// A session-limit refusal additionally carries *loop.WindowSpent
		// (issue #112): both wrap into the same error tree, via Go's
		// multi-%w, so plan.Make's caller can still errors.Is its way to
		// plan.ErrNoAnswer while pump.dispatchJudgement errors.As its way to
		// the typed window and defers to the reset instead of failing.
		werr := error(err)
		if spent := windowSpent(res, err); spent != nil {
			werr = spent
		}
		return plan.Result{}, fmt.Errorf("acp: planner: %w: %w", plan.ErrNoAnswer, werr)
	}
	rec.raw(ctx, "agent-result", bundle.Ref, []byte(resultBody(res, elapsed, bundle.Runner)))
	rec.json(ctx, "agent-text", bundle.Ref, textRecord{AgentRef: bundle.Ref, Text: res.Text, StopReason: res.StopReason})
	if p.Seat != nil {
		if notes := handoffNotes(res.Text); len(notes) > 0 {
			// Bound to no commit: a plan lands nothing, and notes bound to
			// the head it read would be swept the moment the branch moved.
			if err := p.Seat.RecordHandoff(ctx, queue.WorkItem{ID: run, Tenant: p.Tenant, Box: p.Box, Requester: ing.Requester}, "", notes); err != nil {
				return plan.Result{}, fmt.Errorf("acp: planner: recording the handoff: %w", err)
			}
		}
	}
	if reason, needs, paths := escalation(res); reason != "" {
		return plan.Result{}, &loop.Escalation{Seat: p.seatName(), Reason: reason, NeedsSections: needs, Paths: paths}
	}
	nodes, err := parsePlan(res.Text, repo)
	if err != nil {
		return plan.Result{}, fmt.Errorf("acp: planner: %w", err)
	}
	return plan.Result{
		Nodes:    nodes,
		Facts:    []plan.Fact{{Key: "repo/" + repo.Name + "/head", Value: string(head), AsOf: started}},
		AgentRef: bundle.Ref,
	}, nil
}

// Replan satisfies [plan.Replanner]: a unit's builder escalated for a need
// [loop.WidenTests] could not grant in place, and this runs one narrow
// judgement over the section catalogue [sectionsLayer] already renders for
// Decompose — not a fresh decomposition of the whole origination, only
// whether this one unit's sections and task should change to grant what it
// asked for.
func (p *Planner) Replan(ctx context.Context, origination ingress.Ingress, pl plan.Plan, unit plan.ReplanUnit, esc plan.ReplanEscalation) (plan.ReplanNode, error) {
	repo, err := p.repo(origination.Repo)
	if err != nil {
		return plan.ReplanNode{}, err
	}
	run := "replan-" + unit.ID
	if err := p.Mirror.Fetch(ctx, repo); err != nil {
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: fetch %s: %w", repo.Name, err)
	}
	head, err := p.Mirror.Resolve(ctx, repo, "refs/heads/"+repo.DefaultBranch)
	if err != nil {
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: resolve %s: %w", repo.Name, err)
	}
	dir, err := filepath.Abs(filepath.Join(p.Root, run))
	if err != nil {
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: %w", err)
	}
	if err := p.Mirror.MaterializeContext(ctx, dir, agent.ContextSet{Repos: []agent.ContextRepo{
		{Repo: repo.Name, Commit: string(head)},
	}}); err != nil {
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: materializing %s@%s: %w", repo.Name, head, err)
	}
	defer gitforge.RemoveContext(dir)
	tree := filepath.Join(dir, repo.Name)

	req := agent.Request{
		Tenant: p.Tenant, Role: agent.RolePlanner, Box: p.Box, Repo: repo.Name, Run: run,
		Task: unit.Task, Runner: p.Runner, Extra: []agent.Layer{sectionsLayer(repo)},
	}
	bundle, err := p.Composer.Resolve(ctx, req)
	if err != nil {
		return plan.ReplanNode{}, err
	}
	rec := recorder{Store: p.Store, Tenant: p.Tenant, Scope: ingress.Scope(origination.ID), Actor: p.seatName(), OnBehalfOf: origination.Requester}
	layers := make([]layerRecord, 0, len(bundle.Layers))
	for _, l := range bundle.Layers {
		layers = append(layers, layerRecord{Scope: string(l.Scope), Name: l.Name, Source: string(l.Source.Kind), Digest: l.Source.Digest})
	}
	envelope := "(none declared)"
	if bundle.Envelope != nil {
		envelope = bundle.Envelope.String()
	}
	rec.json(ctx, "agent-run", bundle.Ref, runRecord{
		AgentRef: bundle.Ref, Role: string(agent.RolePlanner), Mode: bundle.Tools.PermissionMode,
		Seat: p.seatName(), Envelope: envelope, Layers: layers, Runner: bundle.Runner,
	})

	contextDir, skillsDir := forge.ContextDir(tree), forge.SkillsDir(tree)
	for _, d := range []string{contextDir, skillsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return plan.ReplanNode{}, err
		}
	}
	loaded := ""
	if len(bundle.Skills) > 0 {
		wrote, err := p.Mirror.MaterializeSkills(ctx, skillsDir, bundle.Skills)
		if err != nil {
			return plan.ReplanNode{}, err
		}
		if wrote {
			loaded = skillsDir
		}
	}
	sent := replanPrompt(bundle, unit, pl, esc)
	explained, _ := p.Composer.Explain(ctx, bundle)
	rec.json(ctx, "agent-prompt", bundle.Ref, promptRecord{AgentRef: bundle.Ref, Prompt: sent, Bundle: string(explained)})

	rt := &Runtime{Command: p.Command, Dir: tree, ContextDir: "", Env: p.Env, EnvNames: p.EnvNames,
		MaxTokens: p.MaxTokens, Transcript: p.Transcript,
		SkillsDir: skillsDir, LoadedSkills: loaded, InheritSettings: p.InheritSettings,
		Progress: p.progress(ctx, run, bundle.Ref)}
	if p.Progress != nil {
		p.Progress(ctx, run, Update{Kind: UpdateStarted, Text: bundle.Ref, Session: bundle.Ref})
	}
	started := time.Now()
	res, err := rt.Run(ctx, bundle, sent)
	elapsed := time.Since(started)
	if res.Session.Opened {
		rec.json(ctx, "agent-session", bundle.Ref, sessionRecord{
			AgentRef: bundle.Ref, Model: res.Session.Model(), Modes: res.Session.Modes,
			Config: res.Session.Config, Commands: res.Session.Commands,
			Credential: res.Usage.Credential(), Env: res.EnvNames,
			Session: res.Session.ID, Role: "planner", Runner: bundle.Runner,
		})
	}
	if p.Spend != nil && res.Prompted {
		p.Spend(ctx, run, res.Usage)
	}
	if err != nil {
		// Same seam as Decompose's own: a transport failure or a mid-response
		// server error here means the replan never answered, not that it
		// answered and declined.
		werr := error(err)
		if spent := windowSpent(res, err); spent != nil {
			werr = spent
		}
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: %w: %w", plan.ErrNoAnswer, werr)
	}
	rec.raw(ctx, "agent-result", bundle.Ref, []byte(resultBody(res, elapsed, bundle.Runner)))
	rec.json(ctx, "agent-text", bundle.Ref, textRecord{AgentRef: bundle.Ref, Text: res.Text, StopReason: res.StopReason})
	// Unwrapped, exactly as Decompose returns its own escalation (line
	// above): [Pump.runReplan] parks on any error this returns, and a
	// judgement that itself declines to grant the escalation must surface
	// that decision rather than being swallowed into a parse failure.
	if reason, needs, paths := escalation(res); reason != "" {
		return plan.ReplanNode{}, &loop.Escalation{Seat: p.seatName(), Reason: reason, NeedsSections: needs, Paths: paths}
	}
	node, err := parseReplanNode(res.Text, repo)
	if err != nil {
		return plan.ReplanNode{}, fmt.Errorf("acp: planner: replan: %w", err)
	}
	return node, nil
}

func (p *Planner) seatName() string {
	if p.Seat == nil {
		return "planner"
	}
	return p.Seat.Name
}

// repo finds the repository an origination targets. An empty name is the
// box's only repository, as it has always been for single-repository boxes.
func (p *Planner) repo(name string) (forge.Repo, error) {
	if name == "" && len(p.Repos) == 1 {
		return p.Repos[0], nil
	}
	for _, r := range p.Repos {
		if r.Name == name {
			return r, nil
		}
	}
	return forge.Repo{}, fmt.Errorf("acp: planner: box %s has no repository %q to plan over", p.Box, name)
}

// Planners is the planner decomposer as the registry sees it: one name,
// dispatched by box. The registry keys decomposers by name; a planner is per
// box — its agent, its seat, its repositories; and a box with no agent has no
// planner, so an origination asking for one there is refused with the reason
// rather than planned by nobody.
type Planners struct {
	mu    sync.RWMutex
	byBox map[string]*Planner
}

// Name satisfies [plan.Decomposer].
func (ps *Planners) Name() string { return PlannerName }

// Replayable satisfies [plan.Decomposer].
func (ps *Planners) Replayable() bool { return false }

// Set installs a box's planner; Delete removes one. Both are what a config
// reload does when a box gains or loses its agent.
func (ps *Planners) Set(box string, p *Planner) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.byBox == nil {
		ps.byBox = map[string]*Planner{}
	}
	ps.byBox[box] = p
}

// Delete removes a box's planner.
func (ps *Planners) Delete(box string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.byBox, box)
}

// For answers a box's planner.
func (ps *Planners) For(box string) (*Planner, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.byBox[box]
	return p, ok
}

// Decompose satisfies [plan.Decomposer] by dispatching to the origination's box.
func (ps *Planners) Decompose(ctx context.Context, ing ingress.Ingress, decl kind.Declaration) (plan.Result, error) {
	p, ok := ps.For(ing.Box)
	if !ok {
		return plan.Result{}, fmt.Errorf("acp: box %s asks the planner to decompose %s and has no agent to judge with", ing.Box, ing.ID)
	}
	return p.Decompose(ctx, ing, decl)
}

// sectionsLayer is the catalogue a planner chooses from and the contract its
// answer must meet, as a layer of the repository's scope: the same origination
// planned over a repository with different sections is a different plan, and
// the agentref says so.
func sectionsLayer(repo forge.Repo) agent.Layer {
	var w strings.Builder
	fmt.Fprintf(&w, "## Sections of %s\n", repo.Name)
	if len(repo.Sections) == 0 {
		w.WriteString("This repository declares no sections: every unit is the whole repository, and units take turns.\n")
	} else {
		w.WriteString("Two units in disjoint sections run at once; two in one section take turns; a unit that names none is the whole repository and waits for all of them.\n")
		for _, s := range repo.Sections {
			fmt.Fprintf(&w, "- %s", s.Name)
			if s.Purpose != "" {
				fmt.Fprintf(&w, " — %s", s.Purpose)
			}
			fmt.Fprintf(&w, " (paths: %s", strings.Join(s.Paths, ", "))
			if s.Check != "" {
				fmt.Fprintf(&w, "; check: %s", s.Check)
			}
			w.WriteString(")\n")
		}
	}
	w.WriteString(plannerContract)
	text := w.String()
	sum := sha256.Sum256([]byte(text))
	return agent.Layer{
		Name: "sections", Scope: agent.ScopeRepo,
		Source:       agent.Source{Kind: agent.SourceInline, Digest: hex.EncodeToString(sum[:])},
		Instructions: text,
	}
}

// plannerContract is what a plan is, in the planner's own instructions.
const plannerContract = `
## Your answer
You are planning, not building: read what you need to decide how this origination becomes units of work, and edit nothing. Reply with ONE JSON object as the last thing in your message:

{"units":[{"id":"api-endpoint","task":"...","summary":"...","sections":["api"],"depends_on":[{"on":"schema","for":"db.UsersTable"}],"model":"","attempts":0,"priority":""}],"why":"one paragraph"}

- Prefer one unit. Split only where two units can be built and verified independently, or where one must land before another can start.
- id: a plain token, unique in this plan. task: the complete instruction a builder needs, self-contained — the builder will not see this conversation.
- summary: one line the room shows for this unit; the task is what the builder reads.
- sections: the declared section names the unit's paths fall in. Empty means the whole repository, which waits for every section.
- depends_on: a unit depends on another ONLY when it compiles or tests against code that unit lands — ideas, names and conventions are not dependencies, the task text carries them. Prefer units in disjoint sections with no edges between them, so they run side by side rather than in a chain. Each edge is {"on": the id it waits for, "for": the symbol or file it waits for} — never a bare id; a dependency with nothing to name in "for" is not a dependency.
- model: leave empty unless the unit is trivial enough for a cheaper tier or hard enough to need a stronger one; a value the harness lists.
- attempts: leave at 0 to inherit the box's default budget; set a small integer only when the unit's difficulty warrants more or fewer tries than that default.
- priority: one of low, normal, high, urgent. Leave empty to inherit the origination's priority — the default posture, and right for nearly every unit. Set it only when this unit's own urgency genuinely differs from its origination's (a hotfix split out of a routine chore, or a speculative follow-up split out of an urgent one), and say why in "why".
- If the origination is a question, a decision, or already satisfied by the code as it stands, do not plan: reply with a single line starting with ESCALATE: and the reason.
`

// plannerPrompt is the composed instructions, the task once, and the
// planner's working contract.
func plannerPrompt(bundle agent.Bundle, task string) string {
	var w strings.Builder
	w.WriteString(taskOnce(bundle, task))
	w.WriteString(`
## Working contract
- Read only. Do not edit, create, or commit anything: a plan is not a change.
- End your message with the JSON object described above, or with an ESCALATE: line. Nothing after it.
- End with lines starting with "HANDOFF: " carrying anything the next plan should know about this repository.
`)
	return w.String()
}

// replanContract is what a replan judgement is asked to answer, in the
// judgement's own instructions — the same shape [plan.ReplanNode] carries,
// since a replan may change only WHERE a unit works and WHAT it asks for,
// never the graph around it.
const replanContract = `
## Your answer
Decide this unit's revised shape: whether it now belongs in different sections, and whether its task must say so. Reply with ONE JSON object as the last thing in your message:

{"sections":["..."],"task":"","why":"one paragraph"}

- sections: the unit's COMPLETE section list from here on, not an addition to the old one. Empty means the unit now needs the whole repository.
- task: the unit's revised task, only when it must change to reflect the new sections; empty leaves the task exactly as it already is.
- If the escalation should not be granted as asked — it does not belong in this decomposition, or the repository has no section for what it names — do not answer with JSON: reply with a single line starting with ESCALATE: and the reason.
`

// replanPrompt is the composed instructions for a replan judgement: the same
// task-once heading Decompose sends, the unit as it stands and the rest of
// the plan it belongs to, why its builder escalated instead of grinding, and
// the narrowed working contract above.
func replanPrompt(bundle agent.Bundle, unit plan.ReplanUnit, pl plan.Plan, esc plan.ReplanEscalation) string {
	var w strings.Builder
	w.WriteString(taskOnce(bundle, unit.Task))
	fmt.Fprintf(&w, "\n## The unit as planned\nid: %s\nsections: %v\n", unit.ID, unit.Sections)
	if len(pl.Nodes) > 1 {
		w.WriteString("\n## The rest of this plan\n")
		for _, n := range pl.Nodes {
			if n.ID == unit.ID {
				continue
			}
			fmt.Fprintf(&w, "- %s (sections: %v): %s\n", n.ID, n.Sections, n.Summary)
		}
	}
	fmt.Fprintf(&w, "\n## Why this unit is being replanned\nThe builder working this unit escalated: %s\n", esc.Reason)
	if len(esc.NeedsSections) > 0 {
		fmt.Fprintf(&w, "It named needing these sections: %v\n", esc.NeedsSections)
	}
	if len(esc.Paths) > 0 {
		fmt.Fprintf(&w, "It named these paths: %v\n", esc.Paths)
	}
	w.WriteString(`
## Working contract
- Read only. Do not edit, create, or commit anything: a replan is not a change.
- End your message with the JSON object described above, or with an ESCALATE: line. Nothing after it.
`)
	w.WriteString(replanContract)
	return w.String()
}

// planAnswer is the shape the planner replies in. "units" rather than the
// package's "nodes": the agent is told what it is expressing, not how the
// graph engine spells it.
type planAnswer struct {
	Units []struct {
		ID        string          `json:"id"`
		Task      string          `json:"task"`
		Summary   string          `json:"summary"`
		Sections  []string        `json:"sections"`
		DependsOn []dependsOnEdge `json:"depends_on"`
		Model     string          `json:"model"`
		Attempts  int             `json:"attempts"`
		Priority  string          `json:"priority"`
	} `json:"units"`
	Why string `json:"why"`
}

// dependsOnEdge is one entry of a unit's depends_on list. It accepts both
// shapes a planner may reply with: the old bare id string, and the new
// {"on": "<id>", "for": "<symbol or file it waits for>"} object — so a
// running dogfood planner mid-upgrade, and every fixture and transcript
// already recorded against the old schema, keep parsing. On is the
// dependency's id either way; For is empty for the bare-string shape, which
// [parsePlan] refuses for a judgement plan: a chain with no reason is exactly
// the failure #201 exists to close.
type dependsOnEdge struct {
	On  string
	For string
}

// UnmarshalJSON accepts a bare JSON string (On, no reason) or an object with
// "on" and "for".
func (d *dependsOnEdge) UnmarshalJSON(b []byte) error {
	var id string
	if err := json.Unmarshal(b, &id); err == nil {
		d.On, d.For = id, ""
		return nil
	}
	var obj struct {
		On  string `json:"on"`
		For string `json:"for"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("depends_on entry is neither a bare id string nor {\"on\":...,\"for\":...}: %w", err)
	}
	d.On, d.For = obj.On, obj.For
	return nil
}

// parsePlan reads the planner's answer out of its message and checks it
// against the repository it was planning over. The plan package validates the
// graph again when it seals; this names the disagreements a graph check
// cannot — a section the repository does not declare, a unit with no task —
// in the planner's own terms.
func parsePlan(text string, repo forge.Repo) ([]plan.Node, error) {
	obj, ok := lastObjectWith(text, `"units"`)
	if !ok {
		return nil, fmt.Errorf("the answer carries no plan — no JSON object with \"units\" — and no ESCALATE: line")
	}
	var a planAnswer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return nil, fmt.Errorf("the plan does not parse: %w", err)
	}
	if len(a.Units) == 0 {
		return nil, fmt.Errorf("the plan has no units — a decomposition to nothing must be an ESCALATE: line, never an empty graph")
	}
	ids := map[string]bool{}
	for _, u := range a.Units {
		ids[u.ID] = true
	}
	nodes := make([]plan.Node, 0, len(a.Units))
	for _, u := range a.Units {
		switch {
		case u.ID == "" || strings.ContainsAny(u.ID, " \t/"):
			return nil, fmt.Errorf("unit id %q is not a plain token", u.ID)
		case strings.TrimSpace(u.Task) == "":
			return nil, fmt.Errorf("unit %s has no task", u.ID)
		case strings.TrimSpace(u.Summary) == "":
			return nil, fmt.Errorf("unit %s has no summary — the one line the room shows; a plan without one is incomplete", u.ID)
		}
		for _, s := range u.Sections {
			if _, ok := repo.Section(s); !ok {
				return nil, fmt.Errorf("unit %s names section %q, which repository %s does not declare (declared: %v)", u.ID, s, repo.Name, repo.SectionNames())
			}
		}
		switch queue.Priority(u.Priority) {
		case "", queue.PriorityLow, queue.PriorityNormal, queue.PriorityHigh, queue.PriorityUrgent:
		default:
			return nil, fmt.Errorf("unit %s names priority %q — the levels are low, normal, high, urgent", u.ID, u.Priority)
		}
		deps := make([]string, 0, len(u.DependsOn))
		var reasons map[string]string
		for _, d := range u.DependsOn {
			if !ids[d.On] {
				return nil, fmt.Errorf("unit %s depends on %q, which the plan does not contain", u.ID, d.On)
			}
			if strings.TrimSpace(d.For) == "" {
				return nil, fmt.Errorf("unit %s depends on %q with no \"for\" — a judgement planner's edge must name the symbol or file it waits for", u.ID, d.On)
			}
			deps = append(deps, d.On)
			if reasons == nil {
				reasons = map[string]string{}
			}
			reasons[d.On] = strings.TrimSpace(d.For)
		}
		nodes = append(nodes, plan.Node{ID: u.ID, Task: strings.TrimSpace(u.Task), Summary: strings.TrimSpace(u.Summary), Sections: u.Sections, DependsOn: deps, EdgeReasons: reasons, Model: strings.TrimSpace(u.Model), Attempts: u.Attempts, Priority: queue.Priority(u.Priority)})
	}
	return nodes, nil
}

// replanNodeAnswer is the shape a replan judgement replies in — the same two
// fields [plan.ReplanNode] carries.
type replanNodeAnswer struct {
	Sections []string `json:"sections"`
	Task     string   `json:"task"`
	Why      string   `json:"why"`
}

// parseReplanNode reads a replan judgement's answer out of its message the
// same way [parsePlan] reads a fresh decomposition's: the last balanced JSON
// object naming "sections", checked against the repository it was judged
// over so a section the repository does not declare is refused in the
// planner's own terms rather than surfacing later as a graph error. Unlike a
// freshly decomposed unit, an empty task here is not refused — see
// [plan.ReplanNode.Task] — a replan may leave the task exactly as it already
// stood and say so by leaving it out.
func parseReplanNode(text string, repo forge.Repo) (plan.ReplanNode, error) {
	obj, ok := lastObjectWith(text, `"sections"`)
	if !ok {
		return plan.ReplanNode{}, fmt.Errorf("the answer carries no revised node — no JSON object with \"sections\" — and no ESCALATE: line")
	}
	var a replanNodeAnswer
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return plan.ReplanNode{}, fmt.Errorf("the revised node does not parse: %w", err)
	}
	for _, s := range a.Sections {
		if _, ok := repo.Section(s); !ok {
			return plan.ReplanNode{}, fmt.Errorf("the revised node names section %q, which repository %s does not declare (declared: %v)", s, repo.Name, repo.SectionNames())
		}
	}
	return plan.ReplanNode{Sections: a.Sections, Task: strings.TrimSpace(a.Task)}, nil
}

// lastObjectWith finds the last balanced JSON object in text that contains
// the marker, string-aware so a brace inside a task's prose does not end it.
func lastObjectWith(text, marker string) (string, bool) {
	at := strings.LastIndex(text, marker)
	if at < 0 {
		return "", false
	}
	open := strings.LastIndex(text[:at], "{")
	for open >= 0 {
		if end, ok := closeOf(text, open); ok && end > at {
			return text[open : end+1], true
		}
		open = strings.LastIndex(text[:open], "{")
	}
	return "", false
}

// closeOf returns the index of the brace that closes the object opened at
// open, skipping strings and their escapes.
func closeOf(text string, open int) (int, bool) {
	depth, inString, escaped := 0, false, false
	for i := open; i < len(text); i++ {
		c := text[i]
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// recorder writes a run's narrative records to one chain with one identity.
type recorder struct {
	Store      store.Store
	Tenant     tenant.ID
	Scope      string
	Actor      string
	OnBehalfOf string
}

func (r recorder) raw(ctx context.Context, kind, subject string, body []byte) {
	r.Store.Log().Append(ctx, evidence.Record{
		Tenant: r.Tenant, Scope: r.Scope, Kind: kind, Subject: subject,
		Actor: r.Actor, OnBehalfOf: r.OnBehalfOf, Body: body,
	})
}

func (r recorder) json(ctx context.Context, kind, subject string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		body = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	r.raw(ctx, kind, subject, body)
}

// progress adapts the run-tagged hook to the runtime's untagged one and
// stamps ref as the session that produced each update; a runtime with no
// hook does no work per update.
func (p *Planner) progress(ctx context.Context, run, ref string) func(Update) {
	if p.Progress == nil {
		return nil
	}
	return func(u Update) {
		u.Session = ref
		p.Progress(ctx, run, u)
	}
}
