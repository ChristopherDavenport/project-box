// Package harness discovers what the coding harness on this machine would
// load into an agent, and turns it into refs the factory can record.
//
// The factory does not install plugins and has no opinion about which ones an
// operator wants. What it must not do is stay ignorant of them. A live probe
// of claude-agent-acp's own dialect (2026-09-01) found the adapter starting
// every builder with settingSources user,project,local — which loads whatever
// ~/.claude has enabled: 44 plugins, 79 skills, three hook sets that fire on
// every tool call, and every MCP server those plugins declare, several of them
// authenticated to Slack, Jira, GitHub, Datadog and Honeycomb. None of it was
// chosen by a box, and none of it reached the agentref, so two runs a week
// apart could differ in most of their capability surface while the bundle they
// recorded looked identical.
//
// Reading the same files the harness reads is the cheapest way to close that:
// discovery here happens PER RUN, so enabling a plugin in the harness reaches
// the next build with no reload, and the set is on the record because it was
// resolved into the bundle before the agent started.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// ConfigDir returns the harness configuration directory: CLAUDE_CONFIG_DIR
// when set, ~/.claude otherwise — the same resolution the harness itself does,
// so a daemon and the agents it spawns never disagree about which machine
// state is in play.
func ConfigDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("harness: locating the config dir: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// Discovery reads one harness configuration directory.
type Discovery struct {
	// Dir is the config dir; empty resolves through [ConfigDir].
	Dir string
	// MCP says whether discovered plugins are reported as carrying their own
	// MCP servers. It does not itself enable anything — the runtime enforces
	// the split — but it is what the recorded ref will claim, so the two must
	// be set from the same decision.
	MCP bool
}

// enabledPlugins is the settings shape: plugin-id -> true, or -> an object in
// the extended form. An object with no explicit enabled:false counts as on,
// which matches the harness and errs toward reporting a plugin that is loaded
// rather than hiding one.
type pluginToggle struct {
	on bool
}

func (t *pluginToggle) UnmarshalJSON(b []byte) error {
	var on bool
	if err := json.Unmarshal(b, &on); err == nil {
		t.on = on
		return nil
	}
	var obj struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	t.on = obj.Enabled == nil || *obj.Enabled
	return nil
}

type installEntry struct {
	Scope        string `json:"scope"`
	ProjectPath  string `json:"projectPath"`
	InstallPath  string `json:"installPath"`
	Version      string `json:"version"`
	GitCommitSha string `json:"gitCommitSha"`
}

// Plugins returns the plugins the harness would load for a user-scope session,
// in a stable order.
//
// User scope only, deliberately. The install record also carries project- and
// local-scope entries keyed to the directory they were installed from, and a
// factory workspace is never that directory — a run materializes a fresh tree
// under the mirror root. Reporting them would name plugins that will not load.
//
// A missing config dir is not an error: a machine with no harness settings is
// a machine whose builders load no plugins, which is a fact rather than a
// failure. A malformed one IS an error — silently loading nothing because the
// JSON moved is the failure mode this package exists to prevent.
func (d Discovery) Plugins() ([]agent.PluginRef, error) {
	dir := d.Dir
	if dir == "" {
		var err error
		if dir, err = ConfigDir(); err != nil {
			return nil, err
		}
	}

	var settings struct {
		EnabledPlugins map[string]pluginToggle `json:"enabledPlugins"`
	}
	if err := readJSON(filepath.Join(dir, "settings.json"), &settings); err != nil {
		return nil, err
	}
	if len(settings.EnabledPlugins) == 0 {
		return nil, nil
	}

	var installed struct {
		Plugins map[string][]installEntry `json:"plugins"`
	}
	if err := readJSON(filepath.Join(dir, "plugins", "installed_plugins.json"), &installed); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(settings.EnabledPlugins))
	for id := range settings.EnabledPlugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []agent.PluginRef
	for _, id := range ids {
		if !settings.EnabledPlugins[id].on {
			continue
		}
		entry, ok := userScope(installed.Plugins[id])
		if !ok {
			// Enabled but not installed at user scope: the harness will not
			// load it either, so neither claim it nor fail the run over it.
			continue
		}
		if _, err := os.Stat(entry.InstallPath); err != nil {
			continue
		}
		name, marketplace := split(id)
		out = append(out, agent.PluginRef{
			Name: name, Marketplace: marketplace, Path: entry.InstallPath,
			Version: entry.Version, Commit: entry.GitCommitSha, MCP: d.MCP,
		})
	}
	return out, nil
}

func userScope(entries []installEntry) (installEntry, bool) {
	for _, e := range entries {
		if e.Scope == "user" {
			return e, true
		}
	}
	return installEntry{}, false
}

func split(id string) (name, marketplace string) {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '@' {
			return id[:i], id[i+1:]
		}
	}
	return id, ""
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("harness: reading %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("harness: parsing %s: %w", path, err)
	}
	return nil
}
