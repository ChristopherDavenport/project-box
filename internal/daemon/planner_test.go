package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPlannerNeedsAnAgent: a declaration asking for judgement in a box with
// nobody to judge would fail every origination it admitted, one exception at
// a time. Refused at load, where the operator can see it coming.
func TestAPlannerNeedsAnAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "box.yaml")
	if err := os.WriteFile(cfg, []byte(`
tenant: acme
db: `+filepath.Join(dir, "box.db")+`
root: `+filepath.Join(dir, "forge")+`
listen: unix:/tmp/never-used.sock
boxes:
  - name: shop
    kinds:
      - name: issue
        decompose: {decomposer: planner}
        completion: {graph: true}
    repos:
      - name: app
        remote: /nonexistent/app.git
        build: 'true'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "planner") || !strings.Contains(err.Error(), "no agent") {
		t.Fatalf("a planner declaration without an agent was not refused at load: %v", err)
	}
}
