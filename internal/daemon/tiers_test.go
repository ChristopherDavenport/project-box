package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent"
)

// TestARoleTierReachesTheAgent: box.yaml's roles block was validated at load
// and then dropped — the harness source was built from the box's model and
// session alone, so round 6 asked for effort high on both roles and every
// session asserted xhigh, with nothing on the record to say a tier had been
// named at all. The tier a role names is the tier its bundle carries, per
// key over the box's own, and the box's own is what a role that says
// nothing gets.
func TestARoleTierReachesTheAgent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    agent: ['true']
    model: sonnet
    session: {effort: xhigh, verbose: 'true'}
    roles:
      planner: {model: haiku, session: {effort: high}}
      builder: {session: {effort: medium}}
    kinds:
      - name: issue
        decompose: {decomposer: planner}
        completion: {graph: true}
    repos:
      - name: app
        remote: %s
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), bare(t, "app"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, ok := d.planners.For("shop")
	if !ok {
		t.Fatal("the box declares a planner and the daemon registered none")
	}

	cases := []struct {
		role    agent.Role
		model   string
		session map[string]string
	}{
		// The role's model and session win; the box's other keys still apply.
		{agent.RolePlanner, "haiku", map[string]string{"effort": "high", "verbose": "true"}},
		// A role stating only a session keeps the box's model.
		{agent.RoleBuilder, "sonnet", map[string]string{"effort": "medium", "verbose": "true"}},
		// A role the block does not name is the box's own tier.
		{agent.RoleGate, "sonnet", map[string]string{"effort": "xhigh", "verbose": "true"}},
	}
	for _, c := range cases {
		b, err := p.Composer.Resolve(context.Background(), agent.Request{
			Tenant: d.tenant, Role: c.role, Box: "shop", Repo: "app", Run: "run-" + string(c.role),
		})
		if err != nil {
			t.Fatalf("%s: resolving the bundle: %v", c.role, err)
		}
		if b.Model != c.model {
			t.Errorf("%s: model %q, want %q", c.role, b.Model, c.model)
		}
		if len(b.Session) != len(c.session) {
			t.Errorf("%s: session %v, want %v", c.role, b.Session, c.session)
		}
		for k, want := range c.session {
			if got := b.Session[k]; got != want {
				t.Errorf("%s: session.%s = %q, want %q", c.role, k, got, want)
			}
		}
	}
}
