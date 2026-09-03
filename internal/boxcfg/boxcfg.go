// Package boxcfg is box.yaml: the daemon's declarative configuration —
// boxes, their repositories, their routes, their executors. Several
// repositories in one tenant and one box is the normal shape, not a special
// one: their leases key on (tenant, repo, ref) and contend for nothing, one
// tenant-bound mirror hosts them all, and a ChangeSet may span them.
package boxcfg

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ChristopherDavenport/project-box/internal/agent/confine"
	"github.com/ChristopherDavenport/project-box/internal/capacity"
	"github.com/ChristopherDavenport/project-box/internal/forge"
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
	BuildTimeout string `yaml:"build_timeout"`
	OTLP         string `yaml:"otlp"`
	Budget       Budget `yaml:"budget"`
	Boxes        []Box  `yaml:"boxes"`
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
	Seat    string  `yaml:"seat"`
	Repos   []Repo  `yaml:"repos"`
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
	// Route is the ingress route name that targets this repo; defaults to
	// "<box>-<name>".
	Route string `yaml:"route"`
	Build string `yaml:"build"`
	Check string `yaml:"check"`
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
			if r.Route == "" {
				r.Route = bx.Name + "-" + r.Name
			}
			if prev, dup := seenRoutes[r.Route]; dup {
				return fmt.Errorf("boxcfg: route %q defined by both %s and %s/%s — route names are the API's vocabulary and must be unique", r.Route, prev, bx.Name, r.Name)
			}
			seenRoutes[r.Route] = bx.Name + "/" + r.Name
			if len(bx.Agent) == 0 && r.Build == "" {
				return fmt.Errorf("boxcfg: box %s repo %s: no agent and no build command — nothing would ever do the work", bx.Name, r.Name)
			}
			if r.Check == "" {
				r.Check = "true"
			}
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
