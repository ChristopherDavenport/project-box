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

// TestRouteSectionsAndOpensParse: a route can narrow which sections its work
// inherits, and name a non-default kind it opens — both riding the mapping
// form new fields always take.
func TestRouteSectionsAndOpensParse(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    kinds:
      - name: hotfix
        decompose: {decomposer: identity}
        completion: {graph: true}
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, sections: [daemon], opens: hotfix}
        sections:
          - name: daemon
            purpose: the daemon
            paths: [internal/daemon]
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0].Route
	if len(r.Sections) != 1 || r.Sections[0] != "daemon" {
		t.Fatalf("route.sections = %v, want [daemon]", r.Sections)
	}
	if r.Opens != "hotfix" {
		t.Fatalf("route.opens = %q, want hotfix", r.Opens)
	}
}

// TestRouteSectionsNamingAnUndeclaredSectionIsRefused: mirrors
// self_deploy.sections' own refusal — a route cannot narrow to a section the
// repo never declared.
func TestRouteSectionsNamingAnUndeclaredSectionIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, sections: [nope]}
`))
	if err == nil {
		t.Fatal("a route naming an undeclared section was accepted")
	}
}

// TestRouteOpensAnUnrecognisedKindIsRefused: opens must name a kind this box
// actually declares, or one of the seeds — never defaulted past silently.
func TestRouteOpensAnUnrecognisedKindIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, opens: nonesuch}
`))
	if err == nil {
		t.Fatal("a route naming an unrecognised kind was accepted")
	}
}

// TestRouteOpensASeedKindIsAccepted: the three seeds are always valid opens
// targets even when the box declares no kinds of its own.
func TestRouteOpensASeedKindIsAccepted(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: {name: issues, opens: incident}
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boxes[0].Repos[0].Route.Opens != "incident" {
		t.Fatalf("route.opens = %q, want incident", c.Boxes[0].Repos[0].Route.Opens)
	}
}

// TestFlakeRouteParsesScalarAndMapping: flake_route is the same [Route]
// type, so it takes both the bare-name scalar and the full mapping form.
func TestFlakeRouteParsesScalarAndMapping(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        flake_route: flakes
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].FlakeRoute; got == nil || got.Name != "flakes" {
		t.Fatalf("flake_route = %+v, want name flakes", got)
	}

	c, err = Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        flake_route: {name: app-flakes, label: flaky}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].FlakeRoute; got == nil || got.Name != "app-flakes" || got.Label != "flaky" {
		t.Fatalf("flake_route = %+v, want name app-flakes label flaky", got)
	}
}

// TestFlakeRouteAbsentStaysNil: no flake_route configured must not
// materialize a route at all — assemble reads this nil to decide whether
// flake summoning is wired at all.
func TestFlakeRouteAbsentStaysNil(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boxes[0].Repos[0].FlakeRoute != nil {
		t.Fatalf("flake_route = %+v, want nil when not configured", c.Boxes[0].Repos[0].FlakeRoute)
	}
}

// TestFlakeRouteEmptyNameDefaultsWithADistinctSuffix: an empty name defaults
// the same way Route's own does, under a suffix that can never collide with
// the repo's primary route.
func TestFlakeRouteEmptyNameDefaultsWithADistinctSuffix(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        flake_route: {label: flaky}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Boxes[0].Repos[0]
	if r.Route.Name != "shop-app" {
		t.Fatalf("route.name = %q, want shop-app", r.Route.Name)
	}
	if r.FlakeRoute.Name != "shop-app-flakes" {
		t.Fatalf("flake_route.name = %q, want shop-app-flakes", r.FlakeRoute.Name)
	}
}

// TestFlakeRouteNameCollisionIsRefused: flake_route's name joins the same
// route-name uniqueness check every other route obeys.
func TestFlakeRouteNameCollisionIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        route: issues
        flake_route: issues
`))
	if err == nil {
		t.Fatal("a flake_route colliding with another route's name was accepted")
	}
}

// TestFlakeRouteSectionsAndOpensValidateTheSameWay: flake_route is the same
// Route type, so it is refused for the same unknown-section and
// unrecognised-kind reasons the primary route is.
func TestFlakeRouteSectionsAndOpensValidateTheSameWay(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        flake_route: {sections: [nope]}
`))
	if err == nil {
		t.Fatal("a flake_route naming an undeclared section was accepted")
	}

	_, err = Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
        flake_route: {opens: nonesuch}
`))
	if err == nil {
		t.Fatal("a flake_route naming an unrecognised kind was accepted")
	}
}
