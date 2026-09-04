package boxcfg

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/agent"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

const pin = "5f2c8e7d1a3b4c6d8e0f1a2b3c4d5e6f70819293"

func cfgWithKinds(kinds string) string {
	return `
tenant: acme
boxes:
  - name: shop
` + kinds + `
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
`
}

// TestAnInlineDeclarationMayNotGrantAuthority is the governance condition at
// the surface an operator actually types. A capability grant has to pass
// through change control, and a file on the daemon's disk is not that — same
// rule and same test as an inline base layer meaning org-wide safety rules
// nobody reviewed.
func TestAnInlineDeclarationMayNotGrantAuthority(t *testing.T) {
	_, err := Load(writeCfg(t, cfgWithKinds(`    capabilities:
      break_glass: true
    kinds:
      - name: incident
        capabilities:
          break_glass: true
        completion:
          graph: true`)))
	if err == nil {
		t.Fatal("an inline declaration granted break-glass")
	}
	if !errors.Is(err, kind.ErrCapability) {
		t.Fatalf("wrong refusal: %v", err)
	}
	// The same grant, pinned, passes the shape check at load. Its CONTENT is
	// checked at assembly, where the mirror is.
	c, err := Load(writeCfg(t, cfgWithKinds(`    capabilities:
      break_glass: true
    kinds:
      - name: incident
        ref:
          repo: https://example.test/policy.git
          commit: `+pin+`
          path: kinds/incident.yaml`)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Boxes[0].Kinds[0].Ref.Commit != pin {
		t.Fatalf("ref: %+v", c.Boxes[0].Kinds[0].Ref)
	}
}

// TestABoxCeilingBoundsItsKinds. Delegation intersects and never unions, and
// the refusal is at load rather than a silent narrowing: the only way to
// discover which side won would be to have an emergency.
func TestABoxCeilingBoundsItsKinds(t *testing.T) {
	// Nothing declared is nothing held, which is this system's rule
	// everywhere and matters most for the one dangerous authority in it.
	var ceiling kind.Capabilities
	if ceiling.Any() {
		t.Fatal("an unstated ceiling grants something")
	}
	decl := kind.Declaration{
		Name: "incident", Capabilities: kind.Capabilities{BreakGlass: true},
		Completion: kind.Completion{Graph: true},
		Source:     agent.Source{Kind: agent.SourceGit, Repo: "r", Commit: pin, Path: "p", Digest: "d"},
	}
	if err := decl.Validate(ceiling); !errors.Is(err, kind.ErrCapability) {
		t.Fatalf("an empty ceiling admitted a grant: %v", err)
	}
	if err := decl.Validate(kind.Capabilities{BreakGlass: true}); err != nil {
		t.Fatalf("a ceiling that holds it refused: %v", err)
	}
}

func TestARefAndInlineFieldsTogetherIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, cfgWithKinds(`    kinds:
      - name: incident
        ref:
          repo: https://example.test/policy.git
          commit: `+pin+`
          path: kinds/incident.yaml
        completion:
          graph: true`)))
	if err == nil || !strings.Contains(err.Error(), "both a ref and inline fields") {
		t.Fatalf("wrong refusal: %v", err)
	}
}

func TestAPinNeedsAWholeCommit(t *testing.T) {
	_, err := Load(writeCfg(t, cfgWithKinds(`    kinds:
      - name: incident
        ref:
          repo: https://example.test/policy.git
          commit: 5f2c8e7
          path: kinds/incident.yaml`)))
	if err == nil || !strings.Contains(err.Error(), "full 40-character commit") {
		t.Fatalf("wrong refusal: %v", err)
	}
}

func TestAKindDeclaredTwiceIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, cfgWithKinds(`    kinds:
      - name: bump
        decompose:
          decomposer: identity
        completion:
          graph: true
      - name: bump
        decompose:
          decomposer: identity
        completion:
          graph: true`)))
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("wrong refusal: %v", err)
	}
}

// TestAnInlineDeclarationCarriesEverythingButAuthority.
func TestAnInlineDeclarationCarriesEverythingButAuthority(t *testing.T) {
	c, err := Load(writeCfg(t, cfgWithKinds(`    kinds:
      - name: dependency-bump
        escalation:
          severity: low
          objective: 24h
          max_reescalations: 1
          route: platform-rota
        decompose:
          decomposer: sequence
          steps:
            - name: bump
              task: raise the dependency
            - name: verify
              task: check the fleet still builds
        completion:
          graph: true
        envelope:
          class: routine
          endpoints: [claude-opus-5]
          context: [routine]
          redaction: secrets
          payload: text`)))
	if err != nil {
		t.Fatal(err)
	}
	decl, err := c.Boxes[0].Kinds[0].Declaration(c.Boxes[0].Kinds[0].InlineSource("box.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if decl.Escalation.Objective != 24*time.Hour {
		t.Fatalf("objective: %v", decl.Escalation.Objective)
	}
	if decl.Escalation.Severity != queue.SeverityLow || decl.Escalation.Route != "platform-rota" {
		t.Fatalf("escalation: %+v", decl.Escalation)
	}
	if len(decl.Decompose.Steps) != 2 || decl.Decompose.Steps[1].Name != "verify" {
		t.Fatalf("steps: %+v", decl.Decompose.Steps)
	}
	if decl.Envelope == nil || decl.Envelope.Class != "routine" || !decl.Envelope.SendsPayload() {
		t.Fatalf("envelope: %+v", decl.Envelope)
	}
	if decl.Capabilities.Any() {
		t.Fatal("an inline declaration acquired a capability")
	}
	// The digest is over the declaration's own bytes, so editing one field
	// changes what the box is recorded as running under.
	other := c.Boxes[0].Kinds[0]
	other.Escalation.Route = "someone-else"
	if other.InlineSource("box.yaml").Digest == c.Boxes[0].Kinds[0].InlineSource("box.yaml").Digest {
		t.Fatal("two different declarations share a digest")
	}
}

// TestAnUnrecognisedSeverityIsRefusedRatherThanDefaulted. Everywhere else an
// unrecognised severity resolves to medium so that nothing is quietly the
// quiet end; here the operator WROTE something, and a floor that silently
// became medium is a floor nobody set.
func TestAnUnrecognisedSeverityIsRefusedRatherThanDefaulted(t *testing.T) {
	_, err := Load(writeCfg(t, cfgWithKinds(`    kinds:
      - name: urgent
        escalation:
          severity: URGENT
        completion:
          graph: true`)))
	if err == nil || !strings.Contains(err.Error(), "not one of low, medium, high, critical") {
		t.Fatalf("wrong refusal: %v", err)
	}
}
