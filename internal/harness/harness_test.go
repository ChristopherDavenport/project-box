package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// configDir writes a harness configuration directory: settings.json with the
// given enabledPlugins body, and an install record for every plugin named in
// installs. Install paths are created, because the harness will not load a
// plugin whose directory is gone and neither should discovery report one.
func configDir(t *testing.T, enabled string, installs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", `{"enabledPlugins":`+enabled+`}`)

	var entries []string
	for id, body := range installs {
		body = strings.ReplaceAll(body, "{{dir}}", dir)
		entries = append(entries, `"`+id+`":`+body)
	}
	write(filepath.Join("plugins", "installed_plugins.json"),
		`{"version":2,"plugins":{`+strings.Join(entries, ",")+`}}`)

	for _, sub := range []string{"go", "slack", "local-only"} {
		if err := os.MkdirAll(filepath.Join(dir, "cache", sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func userInstall(version, sha, sub string) string {
	return `[{"scope":"user","installPath":"{{dir}}/cache/` + sub + `","version":"` + version + `","gitCommitSha":"` + sha + `"}]`
}

// TestDiscoveryReportsWhatTheHarnessWouldLoad, which is the whole contract:
// discovery is a reading of the same files, so a plugin it names must be one
// the agent will actually get and vice versa.
func TestDiscoveryReportsWhatTheHarnessWouldLoad(t *testing.T) {
	dir := configDir(t, `{
		"go@mkt": true,
		"slack@mkt": false,
		"gone@mkt": true,
		"uninstalled@mkt": true
	}`, map[string]string{
		"go@mkt":    userInstall("2026.830.1", "611d9835", "go"),
		"slack@mkt": userInstall("1.0.0", "abc", "slack"),
		"gone@mkt":  `[{"scope":"user","installPath":"{{dir}}/cache/vanished","version":"1"}]`,
	})

	got, err := Discovery{Dir: dir}.Plugins()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only the enabled, installed, present plugin; got %v", ids(got))
	}
	p := got[0]
	if p.ID() != "go@mkt" || p.Name != "go" || p.Marketplace != "mkt" {
		t.Errorf("identity wrong: %+v", p)
	}
	if p.Version != "2026.830.1" || p.Commit != "611d9835" {
		t.Errorf("provenance dropped: version=%q commit=%q", p.Version, p.Commit)
	}
	if p.MCP {
		t.Error("MCP defaulted on — a plugin's knowledge is not its credentials")
	}
}

// TestProjectScopedPluginsAreNotReported. The install record also carries
// entries keyed to the directory they were installed from; a factory
// workspace is never that directory, so reporting them would name plugins
// that will not load.
func TestProjectScopedPluginsAreNotReported(t *testing.T) {
	dir := configDir(t, `{"local-only@mkt": true}`, map[string]string{
		"local-only@mkt": `[{"scope":"local","projectPath":"/somewhere/else","installPath":"{{dir}}/cache/local-only","version":"0.1.1"}]`,
	})
	got, err := Discovery{Dir: dir}.Plugins()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("reported a project-scoped plugin the factory will never load: %v", ids(got))
	}
}

// TestExtendedToggleFormCountsAsEnabled. Settings also accept an object per
// plugin. Reading it as "not true, therefore off" would silently under-report
// a plugin that is in fact loaded, which is the worse direction to be wrong.
func TestExtendedToggleFormCountsAsEnabled(t *testing.T) {
	dir := configDir(t, `{
		"go@mkt": {"version": "^2026"},
		"slack@mkt": {"enabled": false}
	}`, map[string]string{
		"go@mkt":    userInstall("2026.830.1", "sha", "go"),
		"slack@mkt": userInstall("1.0.0", "sha", "slack"),
	})
	got, err := Discovery{Dir: dir}.Plugins()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(got) != 1 || got[0].ID() != "go@mkt" {
		t.Errorf("extended toggle misread: %v", ids(got))
	}
}

// TestDiscoveryIsOrderedAndCarriesTheMCPDecision. Order has to be stable
// because the set is hashed into the agentref: a map-order shuffle would make
// two identical agents look different on every run.
func TestDiscoveryIsOrderedAndCarriesTheMCPDecision(t *testing.T) {
	dir := configDir(t, `{"slack@mkt": true, "go@mkt": true}`, map[string]string{
		"go@mkt":    userInstall("1", "sha", "go"),
		"slack@mkt": userInstall("1", "sha", "slack"),
	})
	for i := 0; i < 5; i++ {
		got, err := Discovery{Dir: dir, MCP: true}.Plugins()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID() != "go@mkt" || got[1].ID() != "slack@mkt" {
			t.Fatalf("unstable order: %v", ids(got))
		}
		if !got[0].MCP {
			t.Fatal("the MCP decision did not reach the ref that records it")
		}
	}
}

// TestMissingConfigIsNotAnError, but malformed config is. A machine with no
// harness settings has builders that load no plugins; a machine whose JSON
// moved has builders that load nothing FOR A REASON NOBODY SEES, which is the
// failure this package exists to prevent.
func TestMissingConfigIsNotAnError(t *testing.T) {
	got, err := Discovery{Dir: filepath.Join(t.TempDir(), "absent")}.Plugins()
	if err != nil || got != nil {
		t.Errorf("a machine with no harness config should discover nothing quietly: %v %v", got, err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledPlugins":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Discovery{Dir: dir}).Plugins(); err == nil {
		t.Error("unparseable settings read as an empty plugin set")
	}
}

func ids(ps []agent.PluginRef) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID())
	}
	return out
}
