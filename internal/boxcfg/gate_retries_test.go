package boxcfg

import (
	"strings"
	"testing"
)

// TestGateRetriesAbsentByDefault: nothing configured means nothing to
// resolve — the loop's own default applies, and boxcfg's job is only to
// stay out of the way (issue #83). hack/dogfood/box.yaml names neither
// field and must keep loading unedited, which this pins.
func TestGateRetriesAbsentByDefault(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
`))
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if bx.GateRetries != nil {
		t.Fatalf("box gate_retries = %v, want nil (absent)", bx.GateRetries)
	}
	if bx.Repos[0].GateRetries != nil {
		t.Fatalf("repo gate_retries = %v, want nil (absent)", bx.Repos[0].GateRetries)
	}
}

// TestGateRetriesLoadsAtBothScopes: a repo's override is independent of the
// box's default, and both round-trip as the operator wrote them.
func TestGateRetriesLoadsAtBothScopes(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    gate_retries: 4
    repos:
      - name: app
        remote: /tmp/app.git
        gate_retries: 1
      - name: infra
        remote: /tmp/infra.git
`))
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if bx.GateRetries == nil || *bx.GateRetries != 4 {
		t.Fatalf("box gate_retries = %v, want 4", bx.GateRetries)
	}
	app := bx.Repos[0]
	if app.GateRetries == nil || *app.GateRetries != 1 {
		t.Fatalf("repo app gate_retries = %v, want 1", app.GateRetries)
	}
	if bx.Repos[1].GateRetries != nil {
		t.Fatalf("repo infra gate_retries = %v, want nil (inherits the box's)", bx.Repos[1].GateRetries)
	}
}

// TestGateRetriesZeroIsRefused: an explicit zero is a bound of nothing,
// which asks nothing — unknown must not read as zero, so this is refused
// rather than silently treated the same as absent.
func TestGateRetriesZeroIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, block, want string
	}{
		{"box", "gate_retries: 0", "boxcfg: box shop: gate_retries 0"},
		{"repo", "gate_retries: 0", "boxcfg: box shop repo app: gate_retries 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			if tc.name == "box" {
				body = `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    ` + tc.block + `
    repos:
      - name: app
        remote: /tmp/app.git
`
			} else {
				body = `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        ` + tc.block + `
`
			}
			_, err := Load(writeCfg(t, body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

// TestGateRetriesNegativeIsRefused mirrors the zero case: a negative bound
// is no more meaningful than a zero one.
func TestGateRetriesNegativeIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        gate_retries: -1
`))
	if err == nil || !strings.Contains(err.Error(), "gate_retries -1") {
		t.Fatalf("negative repo gate_retries: err = %v", err)
	}
}
