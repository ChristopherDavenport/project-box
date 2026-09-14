package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/api"
	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
)

// TestAuthenticatorFromConfig: each of boxcfg's three known methods resolves
// to the [api.Authenticator] issue #188 names for it — the seam
// [Daemon.assemble]'s caller (New, Reload) wires onto [api.Server.Auth]
// without either package needing to know the other's types beyond this
// function.
func TestAuthenticatorFromConfig(t *testing.T) {
	if _, ok := authenticatorFromConfig(boxcfg.Auth{Method: boxcfg.AuthMethodPeer}).(api.PeerAuthenticator); !ok {
		t.Fatal("method: peer did not resolve to a PeerAuthenticator")
	}
	if _, ok := authenticatorFromConfig(boxcfg.Auth{}).(api.PeerAuthenticator); !ok {
		t.Fatal("an absent method did not resolve to a PeerAuthenticator")
	}
	if _, ok := authenticatorFromConfig(boxcfg.Auth{Method: boxcfg.AuthMethodNone}).(api.NoneAuthenticator); !ok {
		t.Fatal("method: none did not resolve to a NoneAuthenticator")
	}
	auth := authenticatorFromConfig(boxcfg.Auth{Method: boxcfg.AuthMethodToken,
		Tokens: []boxcfg.AuthToken{{Name: "ci", Secret: "s3cret"}}})
	tok, ok := auth.(api.TokenAuthenticator)
	if !ok {
		t.Fatal("method: token did not resolve to a TokenAuthenticator")
	}
	if tok.Tokens["ci"] != "s3cret" {
		t.Fatalf("tokens = %+v, want ci -> s3cret carried through from boxcfg's resolved secret", tok.Tokens)
	}

	oidcAuth := authenticatorFromConfig(boxcfg.Auth{Method: boxcfg.AuthMethodOIDC,
		OIDC: boxcfg.OIDCAuth{Issuer: "https://issuer.example", Audience: "box-api"}})
	oidc, ok := oidcAuth.(*api.OIDCAuthenticator)
	if !ok {
		t.Fatal("method: oidc did not resolve to an OIDCAuthenticator")
	}
	if oidc.Issuer != "https://issuer.example" || oidc.Audience != "box-api" {
		t.Fatalf("oidc = %+v, want issuer and audience carried through from boxcfg", oidc)
	}
}

// TestAssembleThreadsRouteRequireOntoTheIngressRoute: a repo's route.require
// — enforced at the wire, per [ingress.Route.Require]'s own doc — must
// actually reach the assembled [ingress.Route], or the config would parse
// clean and enforce nothing.
func TestAssembleThreadsRouteRequireOntoTheIngressRoute(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "box.yaml")
	secretPath := filepath.Join(dir, "ci.token")
	if err := os.WriteFile(secretPath, []byte("s3cret"), 0o644); err != nil {
		t.Fatal(err)
	}
	appRem := bare(t, "app")
	cfg := fmt.Sprintf(`
tenant: acme
runner: boxd
db: %s
root: %s
listen: unix:/tmp/never-used.sock
auth:
  method: token
  tokens:
    - {name: ci, secret_file: %s}
boxes:
  - name: shop
    repos:
      - name: app
        remote: %s
        build: '%s'
        route: {name: app-issues, require: [token]}
`, filepath.Join(dir, "box.db"), filepath.Join(dir, "forge"), secretPath, appRem, buildCmd)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, routes, _, _, _, _, err := d.assemble(context.Background(), d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := routes["app-issues"]
	if !ok {
		t.Fatal("route app-issues was not assembled")
	}
	if len(r.Require) != 1 || r.Require[0] != "token" {
		t.Fatalf("route.require = %v, want [token] threaded from boxcfg", r.Require)
	}

	if _, ok := d.API.Auth.(api.TokenAuthenticator); !ok {
		t.Fatalf("d.API.Auth = %T, want a TokenAuthenticator wired from auth.method: token", d.API.Auth)
	}
}
