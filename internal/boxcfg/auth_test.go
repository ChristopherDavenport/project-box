package boxcfg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAuthAbsentDefaultsToPeer: an absent auth: block must keep every
// committed box.yaml valid, hack/dogfood/box.yaml's included — see
// TestDogfoodBoxIsNarrow's sibling, dogfood_test.go.
func TestAuthAbsentDefaultsToPeer(t *testing.T) {
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
		t.Fatalf("no auth block: %v", err)
	}
	if c.Auth.Method != AuthMethodPeer {
		t.Fatalf("auth.method = %q, want the peer default", c.Auth.Method)
	}
}

// TestAuthUnknownMethodIsRefused: an unknown method name must be caught at
// Load, not discovered the first time a caller tries to authenticate.
func TestAuthUnknownMethodIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
auth: {method: carrier-pigeon}
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
`))
	if err == nil {
		t.Fatal("an unknown auth.method was accepted")
	}
}

// TestAuthTokenWithNoTableIsRefused: a token method with no table
// authenticates nobody — the half-configured case validate refuses rather
// than silently running a listener nothing can ever pass.
func TestAuthTokenWithNoTableIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
auth: {method: token}
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
`))
	if err == nil {
		t.Fatal("auth.method: token with an empty table was accepted")
	}
}

// TestAuthTokensWithoutTokenMethodIsRefused: a token table configured under
// any other method is a typo an operator would otherwise never learn about
// until the day they switched methods and the old table silently applied.
func TestAuthTokensWithoutTokenMethodIsRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
auth:
  method: peer
  tokens:
    - {name: ci, secret_env: BOX_TEST_CI_TOKEN}
boxes:
  - name: shop
    repos:
      - name: app
        remote: /tmp/app.git
        build: "true"
`))
	if err == nil {
		t.Fatal("auth.tokens under method: peer was accepted")
	}
}

// TestAuthTokenNeedsExactlyOneSecretSource: neither leaves a token with no
// secret; both leaves it ambiguous which one wins. Both are refused, the
// same half-configured doctrine as Retention's keep/keep_for pair.
func TestAuthTokenNeedsExactlyOneSecretSource(t *testing.T) {
	for name, body := range map[string]string{
		"neither": `
tenant: acme
auth:
  method: token
  tokens:
    - {name: ci}
boxes:
  - name: shop
    repos:
      - {name: app, remote: /tmp/app.git, build: "true"}
`,
		"both": `
tenant: acme
auth:
  method: token
  tokens:
    - {name: ci, secret_file: ` + filepath.Join(t.TempDir(), "nope") + `, secret_env: BOX_TEST_CI_TOKEN}
boxes:
  - name: shop
    repos:
      - {name: app, remote: /tmp/app.git, build: "true"}
`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, body)); err == nil {
				t.Fatalf("%s secret source was accepted", name)
			}
		})
	}
}

// TestAuthTokenSecretFileIsResolvedAtLoad: the secret is read once, from the
// file, and never left inline in the config an operator can accidentally
// commit or back up.
func TestAuthTokenSecretFileIsResolvedAtLoad(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "ci.token")
	if err := os.WriteFile(secretPath, []byte("s3cret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(writeCfg(t, `
tenant: acme
auth:
  method: token
  tokens:
    - {name: ci, secret_file: `+secretPath+`}
boxes:
  - name: shop
    repos:
      - {name: app, remote: /tmp/app.git, build: "true"}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Auth.Tokens) != 1 || c.Auth.Tokens[0].Secret != "s3cret" {
		t.Fatalf("tokens = %+v, want one resolved secret \"s3cret\" (trimmed)", c.Auth.Tokens)
	}
}

// TestAuthTokenSecretEnvMustBeSet: an unset env var is not an empty secret,
// it is no secret at all — resolving it to "" would let an empty bearer
// value authenticate.
func TestAuthTokenSecretEnvMustBeSet(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
auth:
  method: token
  tokens:
    - {name: ci, secret_env: BOX_TEST_DEFINITELY_UNSET_1234}
boxes:
  - name: shop
    repos:
      - {name: app, remote: /tmp/app.git, build: "true"}
`))
	if err == nil {
		t.Fatal("an unset secret_env was accepted")
	}
}

// TestRouteRequireMustMatchTheBoxsAuthMethod: a route may only require a
// method its own box's listener can actually produce — requiring "token"
// under auth.method: peer (the default) is the half-configured combination
// validate refuses at load rather than at the first delivery.
func TestRouteRequireMustMatchTheBoxsAuthMethod(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, require: [token]}
`))
	if err == nil {
		t.Fatal("route.require: [token] under the peer default was accepted")
	}
}

// TestRouteRequireMatchingTheBoxsAuthMethodIsAccepted is the same check's
// other side: a route requiring exactly the method the box's listener
// authenticates under is ordinary configuration, not a refusal.
func TestRouteRequireMatchingTheBoxsAuthMethodIsAccepted(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "ci.token")
	if err := os.WriteFile(secretPath, []byte("s3cret"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(writeCfg(t, `
tenant: acme
auth:
  method: token
  tokens:
    - {name: ci, secret_file: `+secretPath+`}
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, require: [token]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Boxes[0].Repos[0].Route.Require; len(got) != 1 || got[0] != "token" {
		t.Fatalf("route.require = %v, want [token]", got)
	}
}

// TestRouteRequireUnderAuthNoneIsAlwaysRefused: "none" produces no method
// ever, so ANY route.require under it is unsatisfiable by construction —
// refused at load, not left to fail every delivery forever.
func TestRouteRequireUnderAuthNoneIsAlwaysRefused(t *testing.T) {
	_, err := Load(writeCfg(t, `
tenant: acme
auth: {method: none}
boxes:
  - name: shop
    repos:
      - name: app
        remote: https://github.com/acme/app.git
        build: "true"
        route: {name: issues, require: [peer]}
`))
	if err == nil {
		t.Fatal("route.require under auth.method: none was accepted")
	}
}
