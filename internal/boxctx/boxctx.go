// Package boxctx is `box context` — named daemon endpoints in the client's
// own config, the docker-context lesson: an operator working several
// daemons should name them once, not paste -daemon and -token into every
// command.
package boxctx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Context is one named endpoint.
type Context struct {
	Daemon string `yaml:"daemon"`          // unix:/path or tcp:host:port (a gate)
	Token  string `yaml:"token,omitempty"` // for gates that want one
	Tenant string `yaml:"tenant,omitempty"`
	Actor  string `yaml:"actor,omitempty"`
}

// File is the whole client config.
type File struct {
	Current  string             `yaml:"current,omitempty"`
	Contexts map[string]Context `yaml:"contexts,omitempty"`
}

// Path returns the config location, overridable for tests via BOX_CONFIG.
func Path() string {
	if p := os.Getenv("BOX_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "box", "contexts.yaml")
}

// Load reads the file; a missing file is an empty config, not an error.
func Load() (File, error) {
	var f File
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return File{Contexts: map[string]Context{}}, nil
	}
	if err != nil {
		return f, err
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("boxctx: %s: %w", Path(), err)
	}
	if f.Contexts == nil {
		f.Contexts = map[string]Context{}
	}
	return f, nil
}

// Save writes the file with owner-only permissions — it may carry tokens.
func Save(f File) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Resolve returns the context to use: the named one, else the current one,
// else nothing (and callers fall back to flags).
func Resolve(name string) (Context, string, bool, error) {
	f, err := Load()
	if err != nil {
		return Context{}, "", false, err
	}
	if name == "" {
		name = f.Current
	}
	if name == "" {
		return Context{}, "", false, nil
	}
	c, ok := f.Contexts[name]
	if !ok {
		return Context{}, "", false, fmt.Errorf("boxctx: no context %q (have: %v)", name, names(f))
	}
	return c, name, true, nil
}

func names(f File) []string {
	out := make([]string, 0, len(f.Contexts))
	for n := range f.Contexts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Names lists configured contexts, current first-marked by the caller.
func Names(f File) []string { return names(f) }
