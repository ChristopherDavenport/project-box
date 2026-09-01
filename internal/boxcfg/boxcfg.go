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
	// MaxTokensPerWindow is the brake denominated the way the invoice is;
	// zero means unbounded.
	MaxTokensPerWindow int64 `yaml:"max_tokens_per_window"`
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
	Agent Argv   `yaml:"agent"`
	Seat  string `yaml:"seat"`
	Repos []Repo `yaml:"repos"`
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
