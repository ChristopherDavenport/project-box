package boxcfg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRouteScalarKeepsTodaysMeaning: a bare route name is still exactly
// today's whole meaning — a name, polling off — so every existing box.yaml
// stays valid unchanged.
func TestRouteScalarKeepsTodaysMeaning(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: issues
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0].Route
	if r.Name != "issues" || r.Poll != "" || r.Label != "" || r.Contract != "" {
		t.Fatalf("route = %+v, want name issues and everything else empty", r)
	}
	if d := r.PollDuration(); d != 0 {
		t.Fatalf("PollDuration = %v, want 0 (polling off)", d)
	}
}

// TestRouteMappingParsesEveryField: the mapping form names poll, label and
// contract alongside the route's name.
func TestRouteMappingParsesEveryField(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, poll: 5m, label: box, contract: "read CLAUDE.md first"}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0].Route
	if r.Name != "issues" {
		t.Fatalf("name = %q, want issues", r.Name)
	}
	if r.Label != "box" {
		t.Fatalf("label = %q, want box", r.Label)
	}
	if r.Contract != "read CLAUDE.md first" {
		t.Fatalf("contract = %q, want the literal text", r.Contract)
	}
	if got, want := r.PollDuration(), 5*time.Minute; got != want {
		t.Fatalf("PollDuration = %v, want %v", got, want)
	}
}

// TestPollingRouteWithNoLabelIsRefused: unknown never reads as zero — an
// absent label is not "every open issue", and a person has to decide.
func TestPollingRouteWithNoLabelIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, poll: 5m}
`))
	if err == nil {
		t.Fatal("a polling route with no label was accepted")
	}
}

// TestUnparsablePollDurationIsRefused.
func TestUnparsablePollDurationIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, poll: banana, label: box}
`))
	if err == nil {
		t.Fatal("an unparsable poll duration was accepted")
	}
}

// TestPollingRouteRequiresAGitHubRemote: the reader speaks only that forge,
// so a polling route against anything else is refused at Load rather than
// discovered the first time the poller runs.
func TestPollingRouteRequiresAGitHubRemote(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, poll: 5m, label: box}
`))
	if err == nil {
		t.Fatal("a polling route against a non-github remote was accepted")
	}
}

// TestRouteContractAsAPathIsLoadedFromFile: contract is text or a path to a
// file holding it, read once at Load.
func TestRouteContractAsAPathIsLoadedFromFile(t *testing.T) {
	dir := t.TempDir()
	contractPath := filepath.Join(dir, "contract.md")
	if err := os.WriteFile(contractPath, []byte("the working contract, from a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, contract: `+contractPath+`}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Boxes[0].Repos[0].Route.Contract, "the working contract, from a file\n"; got != want {
		t.Fatalf("contract = %q, want %q", got, want)
	}
}

// TestRouteContractAsAnUnresolvablePathIsRefused: a path-shaped value that
// does not resolve is refused rather than silently used as the contract's
// own text.
func TestRouteContractAsAnUnresolvablePathIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, contract: missing-contract.md}
`))
	if err == nil {
		t.Fatal("a contract path that does not resolve was accepted")
	}
}
