// Package agent composes an agent's definition — its instructions, its skills,
// the surfaces it may use, and what it is allowed to read — from layers, and
// resolves them into a content-addressed [Bundle].
//
// The bundle, not the material it was built from, is what gets recorded. Layers
// over a moving base mean the same named agent behaves differently on two days
// while every source it names looks unchanged, and change management has to
// answer what instructions an agent was operating under when it made a given
// change. Only the resolved artifact answers that.
//
// Layers may be written as plain text. Git is a way to govern definitions, not
// a requirement for having them.
package agent

import (
	"context"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Role is what kind of work an agent does. Roles are distinct principals, not
// labels: a builder holds no land grant and a gate holds no propose grant, so
// segregation of duties is enforced by authorization rather than recorded after
// the fact.
type Role string

const (
	RoleBuilder  Role = "builder"
	RoleGate     Role = "gate"
	RoleDeployer Role = "deployer"
	RoleCurator  Role = "curator"
	// RolePlanner decomposes an origination into a graph of units. It
	// judges and reads; it never writes a tree, and its default mode says so.
	// A planner is not a coordinator with a standing session — it is one
	// decomposer among several, recorded under an agentref like any run.
	RolePlanner Role = "planner"
)

// Known reports whether r names a role this system has a seat for.
func (r Role) Known() bool {
	switch r {
	case RoleBuilder, RoleGate, RoleDeployer, RoleCurator, RolePlanner:
		return true
	}
	return false
}

// Scope is a layer's position in the composition order, from most general to
// most specific.
type Scope string

const (
	ScopeBase Scope = "base" // org-wide conventions and safety rules
	ScopeRole Scope = "role" // what this kind of agent does
	ScopeBox  Scope = "box"  // this system's stack and policies
	ScopeRepo Scope = "repo" // repository-specific guidance
	// ScopeSection is a region of a repository: what this unit of work is
	// expressed in, applied after the repository and before the task.
	ScopeSection Scope = "section"
	ScopeTask    Scope = "task" // the WorkItem's own instruction
)

// SourceKind is how a layer's material was obtained.
type SourceKind string

const (
	// SourceInline is text written directly into the definition. The bundle
	// carries the content itself, so an inline layer needs nothing external to
	// reproduce.
	SourceInline SourceKind = "inline"
	// SourceFile is a path on the resolving machine.
	SourceFile SourceKind = "file"
	// SourceGit is a repository at a commit.
	SourceGit SourceKind = "git"
)

// Source says where material came from.
//
// Identity is the [Source.Digest]; the origin fields are provenance — where to
// look when a human asks, and what governance the material passed through.
// Content addressing is what makes a layer reproducible, and a git commit is
// one way to obtain a stable digest rather than the only one.
type Source struct {
	Kind SourceKind
	// Digest is the hash of the material. Required for every kind: a layer
	// without one cannot be reproduced or verified.
	Digest string
	// Repo and Commit are set for SourceGit. Commit is a commit, never a
	// branch — a branch names different content on different days.
	Repo   string
	Commit string
	// Path is set for SourceGit and SourceFile.
	Path string
}

// Governed reports whether this source passed through change control, which is
// true only of git-sourced material.
//
// Inline material is fully reproducible — the bundle carries it — but it has no
// review history. That distinction matters by scope rather than universally:
// inline is the natural form for ScopeTask, which is per-request by nature, and
// progressively more questionable further up the stack, where an inline base
// layer means org-wide safety rules nobody reviewed. Which scopes require
// governed sources is a Box policy decision, deliberately not a restriction in
// this type.
func (s Source) Governed() bool { return s.Kind == SourceGit && s.Commit != "" }

// SkillRef names one skill the agent may use.
type SkillRef struct {
	Name   string
	Source Source
}

// PluginRef names one harness plugin the agent loads: a bundle of skills,
// subagents, commands and hooks that arrives as a unit.
//
// Recorded because it is authority. A plugin contributes hooks that run on
// every tool call and subagents that spawn their own sessions, so "which
// plugins were loaded" is the same class of question as "which instructions
// was it operating under" — and it was the one input nothing captured. A live
// probe of the adapter's own dialect found 44 plugins, 79 skills and three
// hook sets reaching the builders through the harness's ambient settings, none
// of it on the record and none of it chosen by this system.
//
// Version and Commit are provenance, not identity: identity is Path plus
// whatever Source.Digest the discovery could establish. A marketplace plugin
// installed from git has a commit; one installed from a local directory has
// nothing but its path, and saying so is better than inventing a pin.
type PluginRef struct {
	// Name is the plugin's own name, as its manifest states it.
	Name string
	// Marketplace is where it was installed from, empty for a local path.
	Marketplace string
	// Path is the plugin directory handed to the harness.
	Path string
	// Version is the manifest version; Commit is the marketplace checkout's
	// commit when there was one.
	Version string
	Commit  string
	// MCP reports whether this plugin's own MCP servers are loaded with it.
	// False is the factory default: a plugin's knowledge is not its
	// credentials, and an unattended agent that inherits a Slack token has
	// been granted something nobody decided to grant.
	MCP bool
}

// ID is the plugin's stable identity for merging and for the record:
// name@marketplace where there is one, the bare name otherwise.
func (p PluginRef) ID() string {
	if p.Marketplace == "" {
		return p.Name
	}
	return p.Name + "@" + p.Marketplace
}

// MCPRef names an MCP server the agent may call.
type MCPRef struct {
	Name string
	// Command or URL, depending on transport. Whichever is set is part of the
	// bundle digest, so a server swapped underneath an agent changes its ref.
	Command []string
	URL     string
	// Tools narrows what may be called on the server. Empty means every tool
	// the server offers, which is rarely what a scoped role wants.
	Tools []string
}

// ContextRepo is one repository an agent may read.
type ContextRepo struct {
	Repo string
	// Commit pins the content. Pinning is a security property as much as a
	// reproducibility one: a context repository whose content can change
	// mid-run is an injection surface an attacker controls the timing of.
	Commit string
	// Paths narrows what is materialized. Empty means the whole tree.
	Paths []string
	// Write is false for context. An agent writes only to the workspace its
	// lease covers; grant this only where a role genuinely requires it.
	Write bool
	// Class is the data class of this repository's contents. It is checked
	// against the envelope in force, so a run classified below its reading
	// material is refused rather than handed it. Unclassified material is
	// readable only by an envelope that says so.
	Class Class
}

// ContextSet is everything an agent may read.
//
// The runtime materializes exactly this and nothing else, so least privilege
// holds by construction — an agent cannot read what it was not granted, rather
// than being instructed not to. Because the set is part of the recorded bundle,
// "which agents could have seen this" stays answerable after the fact, which is
// the question incident response actually asks.
type ContextSet struct {
	Repos []ContextRepo
	// Knowledge names the knowledge scopes this agent may query.
	Knowledge []string
}

// ToolPolicy bounds what an agent may do, enforced at the ACP stream rather
// than requested in prose.
type ToolPolicy struct {
	Allow []string
	Deny  []string
	// PermissionMode is the harness mode to run under. Left empty, the runtime
	// picks the most restrictive mode the role allows rather than the most
	// permissive — an omitted policy must never widen authority.
	PermissionMode string
}

// Layer is one contribution to a composed definition. Every agentic surface an
// agent exposes belongs here, so that shaping an agent is editing layers rather
// than editing the runtime.
type Layer struct {
	Name         string
	Scope        Scope
	Source       Source
	Instructions string
	Skills       []SkillRef
	Plugins      []PluginRef
	MCP          []MCPRef
	Context      ContextSet
	Tools        ToolPolicy
	// Model overrides the runtime default. Empty inherits from the layer
	// beneath.
	//
	// The value is the agent's own — what its model option lists, verbatim.
	// The runtime applies it through the protocol after the session opens
	// and asserts the answer; a value the agent does not list fails the run
	// before a prompt is sent. Left empty everywhere, the agent runs its
	// default, which for a harness that reads the machine's settings is
	// whatever the machine says: a choice nobody made.
	Model string
	// Session is the rest of the session configuration, by the id the agent
	// gives each option — what the protocol calls session config options:
	// effort, a fast mode, a sub-agent, whatever this agent exposes. The
	// same contract as Model: the agent's own values, applied through the
	// protocol, asserted back, refused otherwise. Per key, the most specific
	// non-empty value wins.
	//
	// Model is not spelled here. The protocol gives it a category of its
	// own, every agent has one, and the record calls it out by name.
	Session map[string]string
}

// Request asks for the agent that should run one piece of work.
type Request struct {
	Tenant tenant.ID
	Role   Role
	Box    string
	Repo   string
	Run    string
	// Task is the WorkItem's own instruction, applied as the most specific
	// layer.
	Task string
	// Model is the tier the plan chose for this unit, when it chose one. It
	// rides the task layer — the most specific statement there is — so it
	// wins over every section, role and box default and is part of the
	// agentref. Empty means the plan expressed no preference.
	Model string
	// Extra layers supplied by the caller, applied after those the resolver
	// finds. This is how a definition is written directly in text, and how an
	// exception path overrides one without a commit.
	Extra []Layer
	// Envelope is the data handling this origination declared — what class of
	// data this work may touch and which endpoints may see it. It comes from
	// the origination rather than from the target, because sensitivity travels
	// with the request: the same repository legitimately receives a routine
	// dependency bump and an embargoed fix.
	//
	// Nil means undeclared, which under a [Composer.Ceiling] is refused rather
	// than defaulted. Where a declaration comes from is not settled here: it
	// belongs on the governed kind declaration, which does not exist yet.
	Envelope *Envelope
}

// Bundle is a resolved, content-addressed agent definition.
type Bundle struct {
	// Ref is the digest over the whole resolved bundle — the `agentref`. It
	// travels on every span, every evidence record, and the ChangeSet, and it
	// is sufficient to reconstitute the exact agent that produced a change.
	Ref    string
	Tenant tenant.ID
	Role   Role
	Box    string
	// Layers records what was composed, in application order, so a bundle can
	// be explained and two bundles diffed.
	Layers []Layer
	// Instructions is the composed prose, in layer order.
	Instructions string
	Skills       []SkillRef
	Plugins      []PluginRef
	MCP          []MCPRef
	Context      ContextSet
	Tools        ToolPolicy
	Model        string
	Session      map[string]string
	// Envelope is the data handling actually in force — the origination's
	// declaration intersected with the ceiling it ran under. It is part of the
	// digest, so a different envelope is a different agent on the record,
	// exactly as continuity notes already are. Nil where no classification
	// regime applies.
	Envelope   *Envelope
	ResolvedAt time.Time
}

// Ungoverned returns the layers that did not pass through change control, so a
// Box policy can refuse a bundle whose broad scopes were written inline, and so
// the exception is visible in evidence when policy permits it.
func (b Bundle) Ungoverned() []Layer {
	var out []Layer
	for _, l := range b.Layers {
		if !l.Source.Governed() {
			out = append(out, l)
		}
	}
	return out
}

// Resolver composes layers into a bundle.
//
// Implementations must be deterministic: the same request against the same
// material must produce the same [Bundle.Ref]. Merge semantics are explicit per
// field — structured fields merge under stated rules, prose concatenates in
// layer order — because free-text concatenation alone hides conflicts, letting
// a narrower layer contradict a broader one with nothing to surface it.
type Resolver interface {
	// Resolve composes the bundle that should run this work.
	Resolve(ctx context.Context, req Request) (Bundle, error)
	// Explain renders a bundle for inspection and diffing without running
	// anything, the way a manifest is rendered before it is applied.
	Explain(ctx context.Context, b Bundle) ([]byte, error)
}
