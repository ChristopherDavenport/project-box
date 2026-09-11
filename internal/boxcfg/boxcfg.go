// Package boxcfg is box.yaml: the daemon's declarative configuration —
// boxes, their repositories, their routes, their executors. Several
// repositories in one tenant and one box is the normal shape, not a special
// one: their leases key on (tenant, repo, ref) and contend for nothing, one
// tenant-bound mirror hosts them all, and a ChangeSet may span them.
package boxcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Config is the whole daemon.
type Config struct {
	Tenant string `yaml:"tenant"`
	Runner string `yaml:"runner"`
	DB     string `yaml:"db"`
	Root   string `yaml:"root"`
	// Listen is the daemon's unix socket. The daemon is socket-only by
	// design: network exposure, tokens, TLS, and OIDC are layers in front
	// (boxgate), never the daemon's own concern.
	Listen string `yaml:"listen"`
	Every  string `yaml:"every"`
	// Drain bounds the graceful-shutdown wait for in-flight runs; empty
	// means 30s. SIGTERM stops new claims immediately; running work gets
	// this long to settle before it is cancelled.
	Drain string `yaml:"drain"`
	// BuildTimeout bounds one build; empty means 30m, and "-1s" (any negative
	// duration) means no bound. Nothing bounded an agent session before: the
	// lease TTL does not, because Checkout renews it.
	BuildTimeout string    `yaml:"build_timeout"`
	OTLP         string    `yaml:"otlp"`
	Budget       Budget    `yaml:"budget"`
	Retention    Retention `yaml:"retention"`
	// Runs is the run log's policy: how long what an agent did while it ran
	// stays readable after the run. Telemetry, kept beside the chain.
	Runs  Runs  `yaml:"runs"`
	Boxes []Box `yaml:"boxes"`
}

// Runs is the retention of the run log — every tool call and its title, what
// the agent said, what policy refused — which the room shows while a unit
// runs and reads back afterwards. Absent, frames are kept for ever, on the
// same reasoning as [Retention]: the destructive act is the one a silent
// default must not take, and an operator who has said nothing has not asked
// for a run's record dropped. Unlike the chain it is not evidence, so an age
// is the whole policy: nothing is exported, nothing is summarised.
type Runs struct {
	// KeepFor is the age past which a run's frames are pruned; empty keeps
	// them.
	KeepFor string `yaml:"keep_for"`
}

// KeepForDuration is the parsed age; zero when unset or unparseable, which
// keeps everything rather than guessing at an interval.
func (r Runs) KeepForDuration() time.Duration {
	if r.KeepFor == "" {
		return 0
	}
	d, _ := time.ParseDuration(r.KeepFor)
	return d
}

// Retention is how much evidence stays in the hot store, and where the rest
// goes. Absent, nothing is ever compacted.
//
// The default is off, and deliberately: everywhere else here an unset policy
// resolves to the strict end because the permissive end is the dangerous one,
// and pruning inverts that — the destructive act is the one a silent default
// would take. An operator who has said nothing has not asked for their
// evidence moved off the hot store. Unbounded growth gets noticed; a default
// that quietly pruned would be noticed by an auditor.
type Retention struct {
	// Keep is how many of the newest records stay hot per scope, and KeepFor
	// is the age below which a record is never pruned. BOTH are required for
	// anything to happen, and the stricter of the two wins: a count alone
	// would flatten a quiet scope's whole history the moment it crossed the
	// number, and an age alone would leave a scope that took a million
	// records in an hour entirely hot.
	Keep    int    `yaml:"keep"`
	KeepFor string `yaml:"keep_for"`
	// MinPrune is the smallest run worth a seam; empty means 100. A seam is
	// not free — every summary is a record a verifier parses and an auditor
	// reasons across — so compacting a handful of rows at a time would buy a
	// slightly smaller store with a chain that is mostly boundaries.
	MinPrune int `yaml:"min_prune"`
	// Dir is where exported objects land; empty means `<root>/worm`.
	//
	// A local directory proves the SHAPE of WORM and not the property, and
	// the receipt says so by carrying no retention lock. The bucket with the
	// lock on it is what an auditor tests, and it replaces this at the Sink
	// seam without the daemon changing.
	Dir string `yaml:"dir"`
}

// KeepForDuration is the parsed age bound; zero when unset or unparseable,
// which disables compaction rather than guessing at an interval.
func (r Retention) KeepForDuration() time.Duration {
	if r.KeepFor == "" {
		return 0
	}
	d, _ := time.ParseDuration(r.KeepFor)
	return d
}

// Budget mirrors capacity.Budget in config form.
type Budget struct {
	MaxConcurrent int `yaml:"max_concurrent"`
	MaxPerWindow  int `yaml:"max_per_window"`
	// MaxTokensPerWindow is the VOLUME brake — how many tokens an hour the
	// factory may push through a provider; zero means unbounded.
	//
	// It was once described here as the brake denominated the way the invoice
	// is, and it is not: a cached read bills at roughly a tenth of a fresh
	// input token and an output token at several times either, so this number
	// cannot tell a cheap window from an expensive one. For the invoice, set
	// MaxCostPerWindow.
	MaxTokensPerWindow int64 `yaml:"max_tokens_per_window"`
	// MaxCostPerWindow is the MONEY brake, in dollars — `25.00` is
	// twenty-five dollars an hour on a one-hour window. Zero means unbounded.
	//
	// Written in dollars because that is the unit an operator is deciding in,
	// and converted once to integer micro-dollars at load: money that
	// accumulates over a window must not drift, and a ceiling nobody can
	// reproduce by adding up the charges is not one they can audit.
	//
	// Setting it also means a run that reports tokens and no cost stops
	// claims until the window turns, exactly as an unreported run does under
	// a token budget — see capacity.ErrUnpriced. Silence is never zero, in
	// either currency.
	MaxCostPerWindow float64 `yaml:"max_cost_per_window"`
	// MaxTokensPerRun bounds ONE run, killing the session at the stream when
	// it spends past this; zero means unbounded.
	//
	// The window bound cannot do this job. It gates whether a run may START,
	// so the ceiling it really enforces is the window plus one whole session,
	// and one session was measured at 20,076,747 tokens against a 6,000,000
	// window. Setting only the window is asking for a limit and getting a
	// suggestion.
	MaxTokensPerRun int64 `yaml:"max_tokens_per_run"`
	// AllowUnmetered keeps the factory working after a run that would not say
	// what it cost. Off by default: measurement is the precondition for a
	// budget, not a feature of one, and an agent whose spend is invisible is
	// a position nobody chooses on purpose.
	AllowUnmetered bool   `yaml:"allow_unmetered"`
	Window         string `yaml:"window"`
	// WindowReserve refuses new starts while the ACCOUNT's own window — the
	// subscription's five-hour allowance, as the last run reported it — is
	// at or above this utilization (0 to 1), until it resets. The share of
	// the window the human on the same account keeps for themselves; `0.5`
	// stops the factory at half. Zero means only a rejected status refuses.
	// See capacity.Budget.WindowReserve for what it can and cannot see.
	WindowReserve float64 `yaml:"window_reserve"`
	// Credential names which account this box runs on: `plan` for a
	// subscription, whose brake is WindowReserve, or `key` for a metered API
	// key, whose brake is MaxCostPerWindow. Absent is refused the moment
	// either brake is set — see validate — because a brake that does not say
	// which account it is protecting protects nothing: a key left in the
	// daemon's environment on a box that believes it is on a subscription
	// would move every run onto the invoice with WindowReserve refusing
	// nothing, and a subscription run under a cost ceiling prices nothing
	// for it to enforce. A run that reports the OTHER kind than the one
	// named here stops the box; see capacity.ErrCredential.
	Credential string `yaml:"credential"`
}

// Argv is a command, written in YAML either as one string for the common
// case or as a list when it needs arguments:
//
//	agent: claude-agent-acp
//	agent: [docker, run, --rm, -v, "{{workspace}}:{{workspace}}",
//	        -w, "{{workspace}}", box/agent:1, claude-agent-acp]
//
// A bare string is split on nothing — it is one argument, not a shell line —
// because a config that quietly reinterprets spaces is a config that runs
// something the operator did not write.
type Argv []string

// UnmarshalYAML accepts a scalar or a sequence.
func (a *Argv) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		if s == "" {
			*a = nil
			return nil
		}
		*a = Argv{s}
		return nil
	}
	var xs []string
	if err := n.Decode(&xs); err != nil {
		return err
	}
	*a = xs
	return nil
}

// Route is a repo's ingress route: a scalar name keeps today's whole
// meaning — polling off, every delivery by hand — or a mapping names a poll
// interval alongside the label a polled issue must carry and the working
// contract appended to its task text:
//
//	route: issues
//	route: {name: issues, poll: 5m, label: box, contract: hack/dogfood/contract.md}
//
// See [Route.UnmarshalYAML].
type Route struct {
	Name string `yaml:"name"`
	// Poll is how often this route's forge is checked for new issues
	// carrying Label; empty means polling is off, today's whole behavior.
	// Parsed the way Config's Every and Drain are — see [Route.PollDuration].
	Poll string `yaml:"poll"`
	// Label is the forge label a polling route only opens issues carrying.
	// Required whenever Poll is set: the label is a person's decision about
	// what the box takes, and an absent label is not "every open issue" —
	// unknown never reads as zero.
	Label string `yaml:"label"`
	// Contract is the working contract appended to a polled issue's task
	// text after a "---" separator, in the shape hack/dogfood/submit.py's
	// CONTRACT constant carried by hand. It is text, or a path to a file
	// holding it, read once at Load relative to the process's working
	// directory — see [loadContract].
	Contract string `yaml:"contract"`
}

// UnmarshalYAML accepts a scalar (a bare route name, polling off — today's
// entire meaning) or a mapping naming poll/label/contract alongside it,
// modeled on [Argv.UnmarshalYAML]'s scalar-or-sequence precedent.
func (r *Route) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*r = Route{Name: s}
		return nil
	}
	type plain Route
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*r = Route(p)
	return nil
}

// PollDuration returns the parsed poll interval; zero means polling is off.
// The error half is checked at Load (validate); by the time anything calls
// this the value is known good, the same contract Config's EveryDuration and
// DrainDuration keep.
func (r Route) PollDuration() time.Duration {
	if r.Poll == "" {
		return 0
	}
	d, _ := time.ParseDuration(r.Poll)
	return d
}

// Box is one system: many repos, one optional agent identity.
type Box struct {
	Name string `yaml:"name"`
	// Agent is the ACP agent's argv; when set, every repo in the box builds
	// through it (with Seat's continuity). When empty, each repo's Build
	// command runs instead.
	//
	// An argv rather than a path because runner confinement is a wrapper
	// (issue #6): `docker run` or `sandbox-exec` goes in front of the agent,
	// and a single-token field could not express one. A live experiment
	// already found an agent reading the factory's mirror and a sibling
	// workspace, so the ability to put something in front of it is the
	// difference between confinement being configurable and being a plan.
	Agent Argv `yaml:"agent"`
	// Model is what the box's builders run, by the value the agent lists for
	// its model option — `sonnet`, `opus[1m]`, whatever the agent's own
	// picker calls it. Applied through the protocol once the session is open
	// and asserted back: a value the agent does not list, or answers with
	// something else, fails the run before a prompt is sent, and the
	// agent-session record says what it offered instead.
	//
	// Empty inherits the agent's default. For an agent that reads the
	// machine's settings that is whatever the machine says — a choice nobody
	// made and, before the session was recorded, nothing could show. The
	// dogfood ran four rounds that way and turned out to be on the most
	// expensive model with a million-token window.
	Model string `yaml:"model"`
	// Session is the rest of the session configuration, by the option id
	// the agent lists: effort, a fast mode, a sub-agent. Same contract as
	// Model — the agent's own values, applied and asserted, refused
	// otherwise. `model` is not spelled here because it has the field above,
	// and `mode` is not because the permission mode belongs to the tool
	// policy and is set through session/set_mode.
	Session map[string]string `yaml:"session"`
	// Attempts bounds how many builds one claim may spend on a gate or
	// review refusal before it parks, for every repo that names no override
	// of its own — see [Repo.Attempts]. Absent means the loop's own default
	// (loop.DefaultAttempts); an explicit zero or negative is refused at
	// validate, because a budget of nothing builds nothing and unknown must
	// not read as zero.
	Attempts *int `yaml:"attempts"`
	// GateRetries bounds how many more times a gate that returned an error
	// is re-asked in place before the item parks, for every repo that names
	// no override of its own — see [Repo.GateRetries]. Absent means the
	// loop's own default (loop.DefaultGateRetries); an explicit zero or
	// negative is refused at validate, because a bound of nothing asks
	// nothing and unknown must not read as zero.
	GateRetries *int `yaml:"gate_retries"`
	// Roles are per-role tiers: what the planner judges on, what the builder
	// expresses on. A role's model or session wins over the box's for that
	// role's runs and loses to a section's and to a plan node's. Keys are the
	// roles this system has seats for; an unknown one is refused at load.
	//
	// This is Gas City's per-agent option_defaults without the agent: a
	// planner is not a person with a home directory, it is a decomposer that
	// happens to judge, and the only thing worth saying about it here is
	// which tier the judgement is worth.
	Roles map[string]Tier `yaml:"roles"`
	// Confine, when set, renders that wrapper instead of making the operator
	// write it. Load folds it into Agent, so the daemon runs one argv and
	// knows nothing about confinement — the seam stays exactly one thing.
	//
	// Written out by hand the argv is a dozen tokens whose security depends on
	// every one of them, and the two that matter most are the ones a reader
	// skims: a `-v` where `--mount` belongs silently invents an empty
	// workspace, and an unset `--user` puts root-owned files in a tree the
	// lander then cannot commit. Declaring the image and letting the profile
	// render the rest is the same "derived, not authored" trade the deploy
	// artifacts make.
	Confine *Confine `yaml:"confine"`
	// Harness is what the box's builders load: plugins, pinned skills, and
	// the MCP servers they are actually granted.
	Harness Harness `yaml:"harness"`
	// Capabilities is the box's CEILING — the most any kind declared under it
	// may carry. Absent grants nothing, which is this system's rule everywhere
	// and matters most here: the one dangerous authority in the factory is
	// break-glass, and a config file that quietly conferred it by saying
	// nothing would be the worst default available.
	//
	// Delegation intersects and never unions, so a kind declaring more than
	// this is refused at load rather than narrowed. Silent narrowing leaves an
	// operator believing something false about their factory, and the only way
	// to discover which side won would be to have an emergency.
	Capabilities kind.Capabilities `yaml:"capabilities"`
	// Kinds are the governed declarations this box accepts work under, joined
	// to and overriding the seeded ones. See [Kind].
	Kinds []Kind `yaml:"kinds"`
	Seat  string `yaml:"seat"`
	Repos []Repo `yaml:"repos"`
	// SelfDeploy, set, turns on the standing chore that stops claims, waits
	// for running work to settle, checks and rebuilds the daemon at the
	// branch's new head, and re-execs on the same socket and store. Absent
	// means never — the daemon only changes binaries when somebody restarts
	// it, today's behavior and the default every existing config keeps.
	SelfDeploy *SelfDeploy `yaml:"self_deploy"`
}

// SelfDeploy is box.yaml's self_deploy: block — see [Box.SelfDeploy].
type SelfDeploy struct {
	// From is the branch this box follows; a moved head is a deploy
	// candidate.
	From string `yaml:"from"`
	// Repo names which of the box's repositories the chore rebuilds from.
	// Required when the box declares more than one repo — picking one
	// implicitly would be exactly the derived value this system refuses
	// everywhere else. A box with exactly one repo may leave this empty and
	// get that repo.
	Repo string `yaml:"repo"`
}

// Kind is box.yaml's spelling of [kind.Declaration].
//
// An entry is EITHER a ref or an inline declaration, never both, and the
// difference is governance rather than convenience. A ref names a repository,
// a commit and a path exactly as a pinned skill does, and it is the only form
// that may grant a capability: a declaration that says what an origination is
// allowed to do has to pass through change control, for the same reason and by
// the same test as an inline base layer meaning org-wide safety rules nobody
// reviewed. An inline declaration may say how work decomposes, how it
// escalates, and when it is finished — everything except authority.
type Kind struct {
	Name string `yaml:"name"`
	// Ref pins the declaration to a governed source. Mutually exclusive with
	// every field below it.
	Ref *KindRef `yaml:"ref"`

	Capabilities kind.Capabilities `yaml:"capabilities"`
	Escalation   Escalation        `yaml:"escalation"`
	Decompose    Decompose         `yaml:"decompose"`
	Completion   kind.Completion   `yaml:"completion"`
	Envelope     *Envelope         `yaml:"envelope"`
}

// KindRef pins a declaration to a tree.
type KindRef struct {
	Repo   string `yaml:"repo"`
	Commit string `yaml:"commit"`
	Path   string `yaml:"path"`
}

// Escalation is box.yaml's spelling of [kind.Escalation]; Objective is a
// duration string because that is what an operator writes.
type Escalation struct {
	Severity         string `yaml:"severity"`
	Objective        string `yaml:"objective"`
	MaxReescalations int    `yaml:"max_reescalations"`
	Route            string `yaml:"route"`
}

// Decompose is box.yaml's spelling of [kind.Decomposition].
type Decompose struct {
	Decomposer string      `yaml:"decomposer"`
	Standing   bool        `yaml:"standing"`
	Steps      []kind.Step `yaml:"steps"`
}

// Envelope is box.yaml's spelling of [agent.Envelope]. The class names are the
// operator's own: nothing here ships a taxonomy or an order over one, because
// the same finding is material non-public information in one estate and an
// ordinary ticket in another.
type Envelope struct {
	Class     string   `yaml:"class"`
	Endpoints []string `yaml:"endpoints"`
	Context   []string `yaml:"context"`
	Redaction string   `yaml:"redaction"`
	Payload   string   `yaml:"payload"`
}

// Declaration converts one configured kind into the governed form, given the
// source it was obtained from.
func (k Kind) Declaration(src agent.Source) (kind.Declaration, error) {
	d := kind.Declaration{
		Name:         k.Name,
		Capabilities: k.Capabilities,
		Escalation: kind.Escalation{
			Severity:         queue.Severity(k.Escalation.Severity),
			MaxReescalations: k.Escalation.MaxReescalations,
			Route:            k.Escalation.Route,
		},
		Decompose: kind.Decomposition{
			Decomposer: k.Decompose.Decomposer,
			Standing:   k.Decompose.Standing,
			Steps:      k.Decompose.Steps,
		},
		Completion: k.Completion,
		Source:     src,
	}
	if k.Escalation.Objective != "" {
		obj, err := time.ParseDuration(k.Escalation.Objective)
		if err != nil {
			return kind.Declaration{}, fmt.Errorf("boxcfg: kind %s: escalation.objective: %w", k.Name, err)
		}
		d.Escalation.Objective = obj
	}
	if k.Escalation.Severity != "" && queue.Severity(k.Escalation.Severity).OrDefault() != queue.Severity(k.Escalation.Severity) {
		return kind.Declaration{}, fmt.Errorf("boxcfg: kind %s: escalation.severity %q is not one of low, medium, high, critical — an unrecognised severity resolves to medium, and a floor that silently became medium is a floor nobody set",
			k.Name, k.Escalation.Severity)
	}
	if k.Envelope != nil {
		ctxClasses := make([]agent.Class, 0, len(k.Envelope.Context))
		for _, c := range k.Envelope.Context {
			ctxClasses = append(ctxClasses, agent.Class(c))
		}
		d.Envelope = &agent.Envelope{
			Class:     agent.Class(k.Envelope.Class),
			Endpoints: k.Envelope.Endpoints,
			Context:   ctxClasses,
			Redaction: agent.Redaction(k.Envelope.Redaction),
			Payload:   agent.Payload(k.Envelope.Payload),
		}
	}
	return d, nil
}

// InlineSource is the source a declaration written into box.yaml carries. Not
// governed, by [agent.Source.Governed]'s own test, which is what refuses it a
// capability: the file is on the operator's disk and its history is whatever
// their deployment happens to keep, where a ref names a commit.
func (k Kind) InlineSource(configPath string) agent.Source {
	body, _ := yaml.Marshal(k)
	sum := sha256.Sum256(body)
	return agent.Source{Kind: agent.SourceFile, Path: configPath, Digest: hex.EncodeToString(sum[:])}
}

// Tier is a model and session configuration, the unit a role, a section, or
// a plan node chooses when it chooses what to run on.
type Tier struct {
	Model   string            `yaml:"model"`
	Session map[string]string `yaml:"session"`
}

// Section is box.yaml's spelling of [forge.Section]: a named region of a
// repository, with the check a builder runs while inside it and the tier its
// work runs on by default.
type Section struct {
	Name    string            `yaml:"name"`
	Purpose string            `yaml:"purpose"`
	Paths   []string          `yaml:"paths"`
	Check   string            `yaml:"check"`
	Model   string            `yaml:"model"`
	Session map[string]string `yaml:"session"`
}

// Confine is box.yaml's spelling of [confine.Profile].
type Confine struct {
	// Image carries the agent. Required when the block is present.
	Image string `yaml:"image"`
	// User is the uid:gid inside the container; empty means the daemon's own,
	// which is what keeps the workspace landable.
	User string `yaml:"user"`
	// Env forwards environment variables into the container BY NAME — the
	// model credential above all. Never name=value: a config file is not a
	// secret store.
	Env []string `yaml:"env"`
	// Network is docker's --network when set. Empty keeps the default,
	// because an agent that cannot reach a model API does nothing.
	Network string `yaml:"network"`
}

// Harness is the agentic surface handed to a box's builders.
//
// It exists because the surface was already there and nobody had declared it.
// The ACP adapter starts every builder with the machine's own harness settings
// in play, so a box inherited whatever ~/.claude had enabled — plugins, their
// skills, their hooks, and their MCP servers — without a line of configuration
// saying so and without any of it reaching the agentref. These fields make the
// inheritance a decision.
type Harness struct {
	// Plugins is where the builders' plugins come from:
	//
	//   user  the machine's enabled set, read fresh on EVERY run, so
	//         enabling a plugin in the harness reaches the next build with
	//         no reload. The default, because a factory nobody can extend
	//         without a restart is a factory nobody extends.
	//   none  nothing but what skills: pins. The bundle is the only
	//         authority and the machine's configuration is not consulted.
	//
	// Either way the resolved set is recorded in the bundle: what changes is
	// who decides it, never whether it is known.
	Plugins string `yaml:"plugins"`
	// PluginMCP loads each plugin's own MCP servers along with the plugin.
	//
	// Off by default, and this is the one default here chosen for safety
	// rather than convenience. A plugin's knowledge is not its credentials: a
	// probe of the running dogfood found builders inheriting authenticated
	// servers for Slack, Jira, GitHub, Datadog and Honeycomb, none of which a
	// box had asked for and any of which an unattended agent could have
	// written through. Grant servers deliberately under mcp: instead.
	PluginMCP bool `yaml:"plugin_mcp"`
	// Skills are pinned by ref: a repo, a commit, and a path. They do not
	// live in the box's own repository, so a skill can be governed and
	// versioned where it belongs and still name an exact tree here.
	Skills []Skill `yaml:"skills"`
	// MCP names the servers the builders may call. Empty means none: with
	// plugin MCP off and nothing listed, a builder has no MCP surface at all,
	// which is the right starting point for an agent nobody is watching.
	MCP []MCPServer `yaml:"mcp"`
	// Env forwards environment variables from the daemon's own process into
	// the agent BY NAME — the same discipline [Confine.Env] already keeps,
	// and for the same reason: the daemon used to hand every builder its
	// whole environment, every token and key on the machine included, and
	// none of it reached the agentref. Never name=value: a config file is
	// not a secret store.
	Env []string `yaml:"env"`
}

// Skill pins one skill to the exact tree it names.
type Skill struct {
	// Name is what the skill is called to the agent, and the directory it is
	// materialized under. It need not match the name in the source repo.
	Name string `yaml:"name"`
	// Repo is the remote the skill comes from, Commit the commit it is pinned
	// at — a commit, never a branch — and Path where the skill directory sits
	// in that tree.
	Repo   string `yaml:"repo"`
	Commit string `yaml:"commit"`
	Path   string `yaml:"path"`
}

// MCPServer is one MCP server granted to a box's builders.
type MCPServer struct {
	Name string `yaml:"name"`
	// Command is a stdio server's argv; URL is an HTTP one. Exactly one.
	Command []string `yaml:"command"`
	URL     string   `yaml:"url"`
	// Tools narrows what may be called, enforced at the stream by the
	// bundle's tool policy. Empty is every tool the server offers.
	Tools []string `yaml:"tools"`
}

// Repo is one repository in a Box.
type Repo struct {
	Name   string `yaml:"name"`
	Remote string `yaml:"remote"`
	Branch string `yaml:"branch"`
	Role   string `yaml:"role"`
	Lane   string `yaml:"lane"`
	// Route is the ingress route that targets this repo; defaults to
	// "<box>-<name>" with polling off. See [Route].
	Route Route  `yaml:"route"`
	Build string `yaml:"build"`
	Check string `yaml:"check"`
	// Land selects how a build reaches the base branch. "" (the default)
	// pushes the built commit directly with the fenced force-with-lease
	// push, today's behavior. "pull_request" opens or reuses a GitHub pull
	// request first (see [git.PRLander]) and lets that same fenced push
	// land once GitHub notices the PR's head is reachable from base.
	Land string `yaml:"land"`
	// LandToken names where a pull_request landing gets its bearer token.
	// Only "gh" (gh auth token) is known today — a stopgap ahead of the
	// GitHub App (docs/PLAN.md Phase 7). A repo asking for pull_request
	// landing without one is refused at Load: the daemon would have no way
	// to open the pull request when the loop first tries to land.
	LandToken string `yaml:"land_token"`
	// Review names the judgement a change must pass before it lands. ""
	// (the default) is today's behavior: the command gate alone decides.
	// "judgement" puts the gate-role reviewer agent
	// (internal/agent/acp.Reviewer) after the command gate, so a change
	// also has to satisfy a reader before it reaches the branch.
	Review string `yaml:"review"`
	// Approval names the human sign-off a change must collect before it
	// lands. Absent, or Count zero (the default), is today's behavior:
	// machine-gated, no person asked — the common case.
	Approval Approval `yaml:"approval"`
	// Sections are this repository's named regions — see [forge.Section].
	// A repository that declares none has one region: itself, and every
	// unit of work in it takes turns.
	Sections []Section `yaml:"sections"`
	// Attempts overrides the box's attempt budget for this repository alone
	// — see [Box.Attempts]. Absent inherits the box's value, and the loop's
	// own default when the box names none either. Refused at validate when
	// it is an explicit zero or negative, for the same reason the box-level
	// field is.
	Attempts *int `yaml:"attempts"`
	// GateRetries overrides the box's gate-retry bound for this repository
	// alone — see [Box.GateRetries]. Absent inherits the box's value, and
	// the loop's own default when the box names none either. Refused at
	// validate when it is an explicit zero or negative, for the same reason
	// the box-level field is.
	GateRetries *int `yaml:"gate_retries"`
}

// Approval is a [Repo]'s human sign-off policy.
type Approval struct {
	// Count is how many distinct approvals must cover the built head before
	// the change lands. Zero (the default) is machine-gated.
	Count int `yaml:"count"`
	// Sources restricts where an approval may come from. Empty means
	// "github" — the only source known today (see the refusal in
	// validate): nothing in this tree records a direct attestation as an
	// approval.Approval, so naming anything else would wait forever on an
	// approval no interface can give.
	Sources []string `yaml:"sources"`
}

// LandPullRequest is the [Repo.Land] value that opens a pull request before
// landing. Any other value, including "", lands with a direct fenced push.
const LandPullRequest = "pull_request"

// ReviewJudgement is the [Repo.Review] value that puts the gate-role reviewer
// after the command gate. Any other value, including "", leaves the command
// gate as the only judgement.
const ReviewJudgement = "judgement"

// Sections converts the declared regions to the forge's form.
func (r Repo) forgeSections() []forge.Section {
	out := make([]forge.Section, 0, len(r.Sections))
	for _, s := range r.Sections {
		out = append(out, forge.Section{
			Name: s.Name, Purpose: s.Purpose, Paths: s.Paths, Check: s.Check,
			Model: s.Model, Session: s.Session,
		})
	}
	return out
}

// ForgeRepo is the repository as the forge sees it, sections included.
func (r Repo) ForgeRepo() forge.Repo {
	return forge.Repo{Name: r.Name, Remote: r.Remote, Role: forge.Role(r.Role),
		DefaultBranch: r.Branch, Sections: r.forgeSections(), Check: r.Check}
}

// Load reads and validates a config.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("boxcfg: %s: %w", path, err)
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	if err := tenant.ID(c.Tenant).Validate(); err != nil {
		return fmt.Errorf("boxcfg: tenant: %w", err)
	}
	if c.Runner == "" {
		c.Runner = "boxd"
	}
	if c.DB == "" {
		c.DB = "box.db"
	}
	if c.Root == "" {
		c.Root = "forge"
	}
	if c.Listen == "" {
		c.Listen = "unix:boxd.sock"
	}
	if !strings.HasPrefix(c.Listen, "unix:") {
		return fmt.Errorf("boxcfg: listen %q — the daemon is socket-only; front it with boxgate for the network", c.Listen)
	}
	if c.Every == "" {
		c.Every = "15s"
	}
	if _, err := time.ParseDuration(c.Every); err != nil {
		return fmt.Errorf("boxcfg: every: %w", err)
	}
	if c.Drain == "" {
		c.Drain = "30s"
	}
	if _, err := time.ParseDuration(c.Drain); err != nil {
		return fmt.Errorf("boxcfg: drain: %w", err)
	}
	if c.BuildTimeout != "" {
		if _, err := time.ParseDuration(c.BuildTimeout); err != nil {
			return fmt.Errorf("boxcfg: build_timeout: %w", err)
		}
	}
	if c.Budget.Window != "" {
		if _, err := time.ParseDuration(c.Budget.Window); err != nil {
			return fmt.Errorf("boxcfg: budget.window: %w", err)
		}
	}
	if r := c.Budget.WindowReserve; r < 0 || r > 1 {
		return fmt.Errorf("boxcfg: budget.window_reserve %v — a share of the account's window, 0 to 1", r)
	}
	switch c.Budget.Credential {
	case "", "plan", "key":
	default:
		return fmt.Errorf("boxcfg: budget.credential %q — this box offers plan (a subscription) or key (a metered API key)", c.Budget.Credential)
	}
	// A brake with nobody saying which account it binds is a brake that does
	// not know what it is protecting: the same call the config already makes
	// about half a retention policy, an operator who set a brake and got
	// silence should be told at load, not discover it the day a key in the
	// daemon's environment moves every run onto an invoice the box thought
	// it was on a subscription.
	if c.Budget.Credential == "" && (c.Budget.WindowReserve > 0 || c.Budget.MaxCostPerWindow > 0) {
		return fmt.Errorf("boxcfg: budget.window_reserve or budget.max_cost_per_window is set with no budget.credential — name plan or key, or neither brake means what it claims")
	}
	if c.Retention.KeepFor != "" {
		if _, err := time.ParseDuration(c.Retention.KeepFor); err != nil {
			return fmt.Errorf("boxcfg: retention.keep_for: %w", err)
		}
	}
	if c.Runs.KeepFor != "" {
		d, err := time.ParseDuration(c.Runs.KeepFor)
		if err != nil {
			return fmt.Errorf("boxcfg: runs.keep_for: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("boxcfg: runs.keep_for %q — a positive age, or absent to keep every frame", c.Runs.KeepFor)
		}
	}
	// Half a retention policy compacts nothing, silently. Refusing is the
	// same call the config already makes about a repo with neither an agent
	// nor a build command: an operator who configured a thing and got nothing
	// should be told at load, not discover it from a store that never shrank.
	if half := (c.Retention.Keep > 0) != (c.Retention.KeepForDuration() > 0); half {
		return fmt.Errorf("boxcfg: retention needs BOTH keep and keep_for (got keep=%d keep_for=%q) — a count alone would flatten a quiet scope's whole history the moment it crossed the number, and an age alone would leave a busy one entirely hot",
			c.Retention.Keep, c.Retention.KeepFor)
	}
	if len(c.Boxes) == 0 {
		return fmt.Errorf("boxcfg: no boxes configured")
	}
	seenRoutes := map[string]string{}
	for i := range c.Boxes {
		bx := &c.Boxes[i]
		if bx.Name == "" {
			return fmt.Errorf("boxcfg: box %d has no name", i)
		}
		if len(bx.Repos) == 0 {
			return fmt.Errorf("boxcfg: box %s has no repos", bx.Name)
		}
		if len(bx.Agent) > 0 && bx.Seat == "" {
			bx.Seat = "mason"
		}
		if err := bx.Harness.validate(bx.Name); err != nil {
			return err
		}
		if err := validateSession("box "+bx.Name, bx.Session); err != nil {
			return err
		}
		if (bx.Model != "" || len(bx.Session) > 0) && len(bx.Agent) == 0 {
			return fmt.Errorf("boxcfg: box %s: model/session with no agent — a build command opens no session to configure", bx.Name)
		}
		if bx.Attempts != nil && *bx.Attempts <= 0 {
			return fmt.Errorf("boxcfg: box %s: attempts %d — a budget of nothing builds nothing; leave it unset for the loop's own default", bx.Name, *bx.Attempts)
		}
		if bx.GateRetries != nil && *bx.GateRetries <= 0 {
			return fmt.Errorf("boxcfg: box %s: gate_retries %d — a bound of nothing asks nothing; leave it unset for the loop's own default", bx.Name, *bx.GateRetries)
		}
		for _, role := range slices.Sorted(maps.Keys(bx.Roles)) {
			if !agent.Role(role).Known() {
				return fmt.Errorf("boxcfg: box %s: roles.%s — no such role; the seats are builder, planner, gate, deployer and curator", bx.Name, role)
			}
			if err := validateSession("box "+bx.Name+" roles."+role, bx.Roles[role].Session); err != nil {
				return err
			}
			if len(bx.Agent) == 0 {
				return fmt.Errorf("boxcfg: box %s: roles.%s with no agent — a build command has no roles to tier", bx.Name, role)
			}
		}
		if err := validateKinds(bx); err != nil {
			return err
		}
		if bx.Confine != nil {
			if len(bx.Agent) == 0 {
				return fmt.Errorf("boxcfg: box %s: confine with no agent — a repo's build command runs on the daemon itself, and there is nothing here to put a container around", bx.Name)
			}
			wrapped, err := confine.Profile{
				Image: bx.Confine.Image, User: bx.Confine.User,
				Env: bx.Confine.Env, Network: bx.Confine.Network,
			}.Wrap(bx.Agent)
			if err != nil {
				return fmt.Errorf("boxcfg: box %s: %w", bx.Name, err)
			}
			bx.Agent = wrapped
		}
		explicitCheck := map[string]bool{}
		for j := range bx.Repos {
			r := &bx.Repos[j]
			if r.Name == "" || r.Remote == "" {
				return fmt.Errorf("boxcfg: box %s repo %d needs name and remote", bx.Name, j)
			}
			if r.Branch == "" {
				r.Branch = "main"
			}
			if r.Role == "" {
				r.Role = string(forge.RoleService)
			}
			if r.Lane == "" {
				r.Lane = string(forge.LaneOptimistic)
			}
			if err := forge.ValidateLane(forge.Lane(r.Lane), forge.Role(r.Role)); err != nil {
				return fmt.Errorf("boxcfg: box %s repo %s: %w", bx.Name, r.Name, err)
			}
			if r.Route.Name == "" {
				r.Route.Name = bx.Name + "-" + r.Name
			}
			if prev, dup := seenRoutes[r.Route.Name]; dup {
				return fmt.Errorf("boxcfg: route %q defined by both %s and %s/%s — route names are the API's vocabulary and must be unique", r.Route.Name, prev, bx.Name, r.Name)
			}
			seenRoutes[r.Route.Name] = bx.Name + "/" + r.Name
			if r.Route.Poll != "" {
				if _, err := time.ParseDuration(r.Route.Poll); err != nil {
					return fmt.Errorf("boxcfg: box %s repo %s: route.poll: %w", bx.Name, r.Name, err)
				}
			}
			if r.Route.PollDuration() > 0 {
				if r.Route.Label == "" {
					return fmt.Errorf("boxcfg: box %s repo %s: route polls with no label — the label is a person's decision about what the box takes, and an absent label is not \"every open issue\"", bx.Name, r.Name)
				}
				if !isGitHubRemote(r.Remote) {
					return fmt.Errorf("boxcfg: box %s repo %s: route polls remote %q, which is not a github.com remote — polling speaks only that forge", bx.Name, r.Name, r.Remote)
				}
			}
			if r.Route.Contract != "" {
				resolved, err := loadContract(r.Route.Contract)
				if err != nil {
					return fmt.Errorf("boxcfg: box %s repo %s: route.contract: %w", bx.Name, r.Name, err)
				}
				r.Route.Contract = resolved
			}
			if len(bx.Agent) == 0 && r.Build == "" {
				return fmt.Errorf("boxcfg: box %s repo %s: no agent and no build command — nothing would ever do the work", bx.Name, r.Name)
			}
			explicitCheck[r.Name] = r.Check != ""
			if r.Check == "" {
				r.Check = "true"
			}
			if r.Land != "" && r.Land != LandPullRequest {
				return fmt.Errorf("boxcfg: box %s repo %s: land %q — only %q is known, or leave it unset for a direct push", bx.Name, r.Name, r.Land, LandPullRequest)
			}
			if r.Land == LandPullRequest && r.LandToken != "gh" {
				return fmt.Errorf("boxcfg: box %s repo %s: land: %s needs land_token — only \"gh\" (gh auth token) is known today, and without one the daemon has no way to open the pull request", bx.Name, r.Name, LandPullRequest)
			}
			if r.Review != "" && r.Review != ReviewJudgement {
				return fmt.Errorf("boxcfg: box %s repo %s: review %q — only %q is known, or leave it unset for the command gate alone", bx.Name, r.Name, r.Review, ReviewJudgement)
			}
			if r.Review == ReviewJudgement && len(bx.Agent) == 0 {
				return fmt.Errorf("boxcfg: box %s repo %s: review: %s with no agent — a build command cannot judge a diff", bx.Name, r.Name, ReviewJudgement)
			}
			if r.Review == ReviewJudgement && forge.Lane(r.Lane) == forge.LaneOptimistic {
				return fmt.Errorf("boxcfg: box %s repo %s: review: %s with lane: %s — the optimistic lane lands first and gates afterward, so a review there would read a diff already on the branch, never a review before the push", bx.Name, r.Name, ReviewJudgement, forge.LaneOptimistic)
			}
			if r.Approval.Count < 0 {
				return fmt.Errorf("boxcfg: box %s repo %s: approval.count %d — a negative count", bx.Name, r.Name, r.Approval.Count)
			}
			if r.Approval.Count > 0 && r.Land != LandPullRequest {
				return fmt.Errorf("boxcfg: box %s repo %s: approval.count with land %q — a GitHub review has nowhere to be without land: %s", bx.Name, r.Name, r.Land, LandPullRequest)
			}
			if r.Approval.Count > 0 && forge.Lane(r.Lane) == forge.LaneOptimistic {
				return fmt.Errorf("boxcfg: box %s repo %s: approval.count with lane: %s — the optimistic lane lands first and gates afterward, so an approver there would be asked about a change already on the branch, never before it landed", bx.Name, r.Name, forge.LaneOptimistic)
			}
			for _, s := range r.Approval.Sources {
				if s != "github" {
					return fmt.Errorf("boxcfg: box %s repo %s: approval.sources %q — only \"github\" is known today; nothing records a direct attestation as an approval yet", bx.Name, r.Name, s)
				}
			}
			if r.Attempts != nil && *r.Attempts <= 0 {
				return fmt.Errorf("boxcfg: box %s repo %s: attempts %d — a budget of nothing builds nothing; leave it unset to inherit the box's, or the loop's own default", bx.Name, r.Name, *r.Attempts)
			}
			if r.GateRetries != nil && *r.GateRetries <= 0 {
				return fmt.Errorf("boxcfg: box %s repo %s: gate_retries %d — a bound of nothing asks nothing; leave it unset to inherit the box's, or the loop's own default", bx.Name, r.Name, *r.GateRetries)
			}
			if err := validateSections(bx, r); err != nil {
				return err
			}
		}
		if err := validateSelfDeploy(bx, explicitCheck); err != nil {
			return err
		}
	}
	return nil
}

// isGitHubRemote reports whether remote names a github.com repository. A
// plain prefix check, not a parse: this package has no mirror to resolve a
// remote against, and a polling route only needs to know which forge to
// ask — internal/daemon's reader does the actual owner/repo split.
func isGitHubRemote(remote string) bool {
	return strings.HasPrefix(remote, "https://github.com/") ||
		strings.HasPrefix(remote, "http://github.com/") ||
		strings.HasPrefix(remote, "git@github.com:")
}

// loadContract resolves a route's contract: text, used exactly as written,
// or a path to a file holding it, read once here relative to the process's
// working directory. A value is treated as a path only when it is a single
// token with no whitespace — anything else is plainly prose, not a filename
// — and a path-shaped value that does not resolve is REFUSED rather than
// silently used as the contract's own text: an operator who wrote a
// filename and mistyped it should be told at Load, not find their box
// running with the filename itself as the working contract.
func loadContract(raw string) (string, error) {
	if strings.ContainsAny(raw, " \t\n") {
		return raw, nil
	}
	b, err := os.ReadFile(raw)
	if err != nil {
		return "", fmt.Errorf("%q looks like a path and does not resolve: %w", raw, err)
	}
	return string(b), nil
}

// validateSelfDeploy resolves and checks a box's self_deploy: block, if it
// has one. explicitCheck records — per repo name, before the loop above
// defaulted an absent check: to "true" — whether the operator actually wrote
// one: a self-deploy target that never got a real check would otherwise look
// exactly like one that always passes, and a commit that cannot be judged
// must not be deployed.
func validateSelfDeploy(bx *Box, explicitCheck map[string]bool) error {
	sd := bx.SelfDeploy
	if sd == nil {
		return nil
	}
	if sd.From == "" {
		return fmt.Errorf("boxcfg: box %s: self_deploy needs from — the branch this box follows", bx.Name)
	}
	if sd.Repo == "" {
		if len(bx.Repos) != 1 {
			return fmt.Errorf("boxcfg: box %s: self_deploy has no repo, and the box declares %d — picking one implicitly is the derived value this system refuses everywhere else; name it in self_deploy.repo", bx.Name, len(bx.Repos))
		}
		sd.Repo = bx.Repos[0].Name
	}
	had, ok := explicitCheck[sd.Repo]
	if !ok {
		return fmt.Errorf("boxcfg: box %s: self_deploy.repo %q is not one of this box's repositories", bx.Name, sd.Repo)
	}
	if !had {
		return fmt.Errorf("boxcfg: box %s: self_deploy.repo %s has no check — a self-deploy target with no check cannot be judged, and a commit that cannot be judged must not be deployed", bx.Name, sd.Repo)
	}
	return nil
}

// validateSession is the one rule every session block obeys, wherever it
// sits: ids are non-empty, values are non-empty, and the two things that have
// fields of their own are not spelled here.
func validateSession(where string, session map[string]string) error {
	for _, k := range slices.Sorted(maps.Keys(session)) {
		switch v := session[k]; {
		case k == "":
			return fmt.Errorf("boxcfg: %s: a session option with an empty id", where)
		case k == "model":
			return fmt.Errorf("boxcfg: %s: session.model — the model has its own field: model: %s", where, v)
		case k == "mode":
			return fmt.Errorf("boxcfg: %s: session.mode — the permission mode is the tool policy's, set through session/set_mode, not a session option", where)
		case v == "":
			return fmt.Errorf("boxcfg: %s: session.%s has no value", where, k)
		}
	}
	return nil
}

// validateSections refuses regions the loop could not lease or a builder could
// not be told about: a name that is not a token, a region with no paths, a
// path that reaches outside the repository, a tier with no agent to run it.
func validateSections(bx *Box, r *Repo) error {
	seen := map[string]bool{}
	for i, s := range r.Sections {
		where := fmt.Sprintf("box %s repo %s section %d", bx.Name, r.Name, i)
		if s.Name == "" {
			return fmt.Errorf("boxcfg: %s has no name", where)
		}
		where = fmt.Sprintf("box %s repo %s section %s", bx.Name, r.Name, s.Name)
		if strings.ContainsAny(s.Name, " \t/") {
			return fmt.Errorf("boxcfg: %s — a section name is a plain token; it becomes a lease ref and a layer name", where)
		}
		if seen[s.Name] {
			return fmt.Errorf("boxcfg: %s is declared twice — two regions under one name would lease as one and be described as two", where)
		}
		seen[s.Name] = true
		if len(s.Paths) == 0 {
			return fmt.Errorf("boxcfg: %s covers no paths — a region of nothing cannot be worked in", where)
		}
		for _, p := range s.Paths {
			clean := filepath.ToSlash(filepath.Clean(p))
			if p == "" || filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, "../") {
				return fmt.Errorf("boxcfg: %s: path %q — paths are repository-relative and stay inside it", where, p)
			}
		}
		if err := validateSession(where, s.Session); err != nil {
			return err
		}
		if (s.Model != "" || len(s.Session) > 0) && len(bx.Agent) == 0 {
			return fmt.Errorf("boxcfg: %s: a tier with no agent — a build command opens no session to configure", where)
		}
	}
	return nil
}

// validateKinds checks the shape of a box's declarations at load, and the
// governance rule for the inline ones.
//
// A ref'd declaration cannot be checked here — its content is in a repository
// this package has no mirror for — so it is checked at assembly, where the
// resolver is. What CAN be caught at load is caught at load: an operator who
// wrote a capability into their config file should be told when they start the
// daemon, not the first time an incident tries to break glass.
func validateKinds(bx *Box) error {
	seen := map[string]bool{}
	for i := range bx.Kinds {
		k := &bx.Kinds[i]
		switch {
		case k.Name == "":
			return fmt.Errorf("boxcfg: box %s: kind %d has no name", bx.Name, i)
		case seen[k.Name]:
			return fmt.Errorf("boxcfg: box %s: kind %q is declared twice — the later would silently replace the earlier, and a capability grant is not a thing to resolve by ordering", bx.Name, k.Name)
		}
		seen[k.Name] = true
		if k.Ref == nil {
			decl, err := k.Declaration(k.InlineSource("box.yaml"))
			if err != nil {
				return err
			}
			if err := decl.Validate(bx.Capabilities); err != nil {
				return fmt.Errorf("boxcfg: box %s: %w", bx.Name, err)
			}
			continue
		}
		if k.Capabilities.Any() || k.Escalation != (Escalation{}) || k.Decompose.Decomposer != "" ||
			k.Decompose.Standing || len(k.Decompose.Steps) > 0 ||
			k.Completion.Graph || len(k.Completion.Facts) > 0 || k.Envelope != nil {
			return fmt.Errorf("boxcfg: box %s: kind %q has both a ref and inline fields — one of the two would be ignored, and which one is not a thing to leave to a reader of this file", bx.Name, k.Name)
		}
		switch {
		case k.Ref.Repo == "":
			return fmt.Errorf("boxcfg: box %s: kind %q has no ref.repo", bx.Name, k.Name)
		case k.Ref.Path == "":
			return fmt.Errorf("boxcfg: box %s: kind %q has no ref.path", bx.Name, k.Name)
		case len(k.Ref.Commit) != commitLen:
			return fmt.Errorf("boxcfg: box %s: kind %q is pinned to %q — a governed declaration needs a full %d-character commit, so what an origination was permitted to do is answerable later",
				bx.Name, k.Name, k.Ref.Commit, commitLen)
		}
	}
	return nil
}

// PluginsInherit and PluginsNone are the values of [Harness.Plugins].
const (
	PluginsInherit = "user"
	PluginsNone    = "none"
)

// commitLen is the length of a full hex object id. Skill pins are required to
// be one: an abbreviated sha is ambiguous across a repository's lifetime, and
// a branch name would resolve to different content on different days — which
// is exactly the failure the pin exists to prevent, arriving silently.
const commitLen = 40

func (h *Harness) validate(box string) error {
	switch h.Plugins {
	case "":
		h.Plugins = PluginsInherit
	case PluginsInherit, PluginsNone:
	default:
		return fmt.Errorf("boxcfg: box %s: harness.plugins %q — expected %q or %q",
			box, h.Plugins, PluginsInherit, PluginsNone)
	}
	seen := map[string]bool{}
	for i := range h.Skills {
		s := &h.Skills[i]
		switch {
		case s.Name == "":
			return fmt.Errorf("boxcfg: box %s: skill %d has no name", box, i)
		case strings.ContainsAny(s.Name, "/\\") || s.Name == "." || s.Name == "..":
			return fmt.Errorf("boxcfg: box %s: skill name %q must be a plain directory name", box, s.Name)
		case seen[s.Name]:
			return fmt.Errorf("boxcfg: box %s: skill %q is declared twice — the later pin would silently win", box, s.Name)
		case s.Repo == "":
			return fmt.Errorf("boxcfg: box %s: skill %s has no repo", box, s.Name)
		case s.Path == "":
			return fmt.Errorf("boxcfg: box %s: skill %s has no path", box, s.Name)
		case len(s.Commit) != commitLen:
			return fmt.Errorf("boxcfg: box %s: skill %s is pinned to %q — a skill ref needs a full %d-character commit, so the exact version a builder ran is answerable later",
				box, s.Name, s.Commit, commitLen)
		}
		seen[s.Name] = true
	}
	names := map[string]bool{}
	for i := range h.MCP {
		m := &h.MCP[i]
		switch {
		case m.Name == "":
			return fmt.Errorf("boxcfg: box %s: mcp server %d has no name", box, i)
		case names[m.Name]:
			return fmt.Errorf("boxcfg: box %s: mcp server %q is declared twice", box, m.Name)
		case (len(m.Command) == 0) == (m.URL == ""):
			return fmt.Errorf("boxcfg: box %s: mcp server %s needs exactly one of command or url", box, m.Name)
		}
		names[m.Name] = true
	}
	for _, name := range h.Env {
		if name == "" || strings.ContainsRune(name, '=') {
			return fmt.Errorf("boxcfg: box %s: harness.env %q — a NAME, not an assignment; a value here would put a credential in the config file", box, name)
		}
	}
	return nil
}

// EveryDuration returns the parsed tick interval.
func (c Config) EveryDuration() time.Duration {
	d, _ := time.ParseDuration(c.Every)
	return d
}

// DrainDuration returns the parsed graceful-shutdown window.
func (c Config) DrainDuration() time.Duration {
	d, _ := time.ParseDuration(c.Drain)
	return d
}

// BuildTimeoutDuration returns the parsed build bound; zero means the loop's
// own default applies.
func (c Config) BuildTimeoutDuration() time.Duration {
	if c.BuildTimeout == "" {
		return 0
	}
	d, _ := time.ParseDuration(c.BuildTimeout)
	return d
}

// WindowDuration returns the parsed budget window (zero when unset).
func (c Config) WindowDuration() time.Duration {
	if c.Budget.Window == "" {
		return 0
	}
	d, _ := time.ParseDuration(c.Budget.Window)
	return d
}

// MaxCostMicros is the configured cost ceiling in micro-dollars — the one
// place the operator's dollars become the integer the meter accumulates in.
func (c Config) MaxCostMicros() int64 { return capacity.Micros(c.Budget.MaxCostPerWindow) }

// RetentionDir is where exported evidence lands, defaulting beside the
// mirrors under the daemon's root.
func (c Config) RetentionDir() string {
	if c.Retention.Dir != "" {
		return c.Retention.Dir
	}
	return filepath.Join(c.Root, "worm")
}
