package boxcfg

import (
	"strings"
	"testing"
)

// TestAttemptsAbsentByDefault: nothing configured means nothing to resolve —
// the loop's own default applies, and boxcfg's job is only to stay out of the
// way (issue #81). hack/dogfood/box.yaml names neither field and must keep
// loading unedited, which this pins.
func TestAttemptsAbsentByDefault(t *testing.T) {
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
	if bx.Attempts != nil {
		t.Fatalf("box attempts = %v, want nil (absent)", bx.Attempts)
	}
	if bx.Repos[0].Attempts != nil {
		t.Fatalf("repo attempts = %v, want nil (absent)", bx.Repos[0].Attempts)
	}
}

// TestAttemptsLoadsAtBothScopes: a repo's override is independent of the
// box's default, and both round-trip as the operator wrote them.
func TestAttemptsLoadsAtBothScopes(t *testing.T) {
	c, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    attempts: 4
    repos:
      - name: app
        remote: /tmp/app.git
        attempts: 1
      - name: infra
        remote: /tmp/infra.git
`))
	if err != nil {
		t.Fatal(err)
	}
	bx := c.Boxes[0]
	if bx.Attempts == nil || *bx.Attempts != 4 {
		t.Fatalf("box attempts = %v, want 4", bx.Attempts)
	}
	app := bx.Repos[0]
	if app.Attempts == nil || *app.Attempts != 1 {
		t.Fatalf("repo app attempts = %v, want 1", app.Attempts)
	}
	if bx.Repos[1].Attempts != nil {
		t.Fatalf("repo infra attempts = %v, want nil (inherits the box's)", bx.Repos[1].Attempts)
	}
}

// TestAttemptsZeroIsRefused: an explicit zero is a budget of nothing, which
// builds nothing — unknown must not read as zero, so this is refused rather
// than silently treated the same as absent.
func TestAttemptsZeroIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, block, want string
	}{
		{"box", "attempts: 0", "boxcfg: box shop: attempts 0"},
		{"repo", "attempts: 0", "boxcfg: box shop repo app: attempts 0"},
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

// TestAttemptsNegativeIsRefused mirrors the zero case: a negative budget is
// no more meaningful than a zero one.
func TestAttemptsNegativeIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    agent: claude-agent-acp
    repos:
      - name: app
        remote: /tmp/app.git
        attempts: -1
`))
	if err == nil || !strings.Contains(err.Error(), "attempts -1") {
		t.Fatalf("negative repo attempts: err = %v", err)
	}
}
