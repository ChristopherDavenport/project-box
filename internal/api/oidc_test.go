package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// jwksTestServer stands in for an OIDC issuer: its own
// /.well-known/openid-configuration names its /jwks endpoint, which serves
// the single RSA key minted alongside it — everything OIDCAuthenticator
// needs to discover and verify against, with no real issuer involved.
func jwksTestServer(t *testing.T) (ts *httptest.Server, priv *rsa.PrivateKey, kid string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid = "test-key"
	mux := http.NewServeMux()
	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"jwks_uri": ts.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(bigEndianBytes(priv.PublicKey.E)),
			}},
		})
	})
	return ts, priv, kid
}

// bigEndianBytes is the JWK encoding of a small int exponent (RFC 7518
// §6.3.1), e.g. 65537 -> {0x01, 0x00, 0x01}.
func bigEndianBytes(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	var b []byte
	for e > 0 {
		b = append([]byte{byte(e & 0xff)}, b...)
		e >>= 8
	}
	return b
}

// mintRS256 signs claims into a compact JWT, RS256, the header naming kid —
// exactly the shape OIDCAuthenticator.Authenticate parses.
func mintRS256(t *testing.T, priv *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	return mintWithAlg(t, priv, kid, "RS256", claims)
}

func mintWithAlg(t *testing.T, priv *rsa.PrivateKey, kid, alg string, claims map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(map[string]string{"alg": alg, "kid": kid})
	if err != nil {
		t.Fatal(err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	seg := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	signingInput := seg(headerJSON) + "." + seg(claimsJSON)
	if priv == nil {
		return signingInput + "."
	}
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + seg(sig)
}

// TestOIDCVerifiableBearerRecordsIssuerSubjectAndMethod is the issue's own
// example: a bearer that verifies against the issuer's own published key
// records the token's sub and "oidc" on the opened ingress's Source — the
// same shape TestOpenIngressOverATokenRecordsTheTokensNameAsSource proves
// for the token method.
func TestOIDCVerifiableBearerRecordsIssuerSubjectAndMethod(t *testing.T) {
	iss, priv, kid := jwksTestServer(t)
	token := mintRS256(t, priv, kid, map[string]any{
		"iss": iss.URL, "sub": "alice@example.com", "exp": time.Now().Add(time.Hour).Unix(),
	})

	w := rig(t)
	w.srv.Auth = &OIDCAuthenticator{Issuer: iss.URL, HTTPClient: iss.Client()}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), token, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.OpenIngress(context.Background(), "issues", "oidc-key", "task text", "", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if res.Ingress.Source.Subject != "alice@example.com" || res.Ingress.Source.Method != MethodOIDC {
		t.Fatalf("source = %+v, want subject alice@example.com method oidc", res.Ingress.Source)
	}
}

// TestOIDCExpiredTokenIsRefusedWithReason: an expired token is refused, and
// the refusal names why — never treated as unexpiring, the doctrine
// [OIDCAuthenticator.Authenticate]'s exp handling documents.
func TestOIDCExpiredTokenIsRefusedWithReason(t *testing.T) {
	iss, priv, kid := jwksTestServer(t)
	token := mintRS256(t, priv, kid, map[string]any{
		"iss": iss.URL, "sub": "alice@example.com", "exp": time.Now().Add(-time.Hour).Unix(),
	})
	auth := &OIDCAuthenticator{Issuer: iss.URL, HTTPClient: iss.Client()}
	req := httptest.NewRequest("POST", "/v1/ingress", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err := auth.Authenticate(req)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want a refusal naming expired", err)
	}
}

// TestOIDCWrongKeySignatureIsRefused: a token whose kid names a real,
// published key but whose signature was produced by a DIFFERENT private key
// is refused — the JWKS entry vouches for the key, not the caller's say-so.
func TestOIDCWrongKeySignatureIsRefused(t *testing.T) {
	iss, _, kid := jwksTestServer(t)
	wrongPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := mintRS256(t, wrongPriv, kid, map[string]any{
		"iss": iss.URL, "sub": "eve", "exp": time.Now().Add(time.Hour).Unix(),
	})
	auth := &OIDCAuthenticator{Issuer: iss.URL, HTTPClient: iss.Client()}
	req := httptest.NewRequest("POST", "/v1/ingress", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := auth.Authenticate(req); err == nil {
		t.Fatal("a wrong-key signature was accepted")
	}
}

// TestARouteRequiringOIDCRefusesAPeerDelivery is issue #188's fourth test,
// now that oidc exists to require: the same doctrine
// TestARouteRequiringTokenRefusesAPeerDelivery proves for token, on the
// method wire-auth's own interface names in its Method doc as the reason
// this seam exists at all.
func TestARouteRequiringOIDCRefusesAPeerDelivery(t *testing.T) {
	w := rig(t)
	w.srv.SetRoutes(map[string]ingress.Route{"issues": {
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "daemon",
		Require: []string{MethodOIDC},
	}})
	c := dialUnix(t, w, "")

	_, err := c.OpenIngress(context.Background(), "issues", "gated-key", "task text", "", "")
	if err == nil {
		t.Fatal("a peer delivery was accepted by a route requiring oidc")
	}
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusForbidden || !strings.Contains(apiErr.Message, MethodOIDC) {
		t.Fatalf("err = %v, want a 403 naming oidc", err)
	}
}

// TestOIDCAlgIsCheckedBeforeEverythingElse: "none" is refused
// unconditionally, and any alg this authenticator does not support is
// refused by name — both before a key is ever fetched, so neither needs a
// working JWKS server to prove.
func TestOIDCAlgIsCheckedBeforeEverythingElse(t *testing.T) {
	claims := map[string]any{"iss": "https://issuer.example", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix()}
	for _, tc := range []struct {
		name, alg, wantSubstr string
	}{
		{"none", "none", "none"},
		{"unsupported", "HS256", "HS256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := mintWithAlg(t, nil, "whatever", tc.alg, claims)
			auth := &OIDCAuthenticator{Issuer: "https://issuer.example"}
			req := httptest.NewRequest("POST", "/v1/ingress", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			_, err := auth.Authenticate(req)
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.wantSubstr)
			}
		})
	}
}

// TestOIDCMissingRequiredClaimsAreNeverDefaulted: a token missing exp is
// never treated as unexpiring, and a token missing sub is never treated as
// anonymous — both refused outright rather than read as a permissive zero
// value.
func TestOIDCMissingRequiredClaimsAreNeverDefaulted(t *testing.T) {
	iss, priv, kid := jwksTestServer(t)
	for name, claims := range map[string]map[string]any{
		"no exp": {"iss": iss.URL, "sub": "alice"},
		"no sub": {"iss": iss.URL, "exp": time.Now().Add(time.Hour).Unix()},
	} {
		t.Run(name, func(t *testing.T) {
			token := mintRS256(t, priv, kid, claims)
			auth := &OIDCAuthenticator{Issuer: iss.URL, HTTPClient: iss.Client()}
			req := httptest.NewRequest("POST", "/v1/ingress", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if _, err := auth.Authenticate(req); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// TestOIDCAuthenticateCarriesClaims: the resulting Subject carries the
// token's own claim set, not just sub — a consumer beyond this package's own
// tests is exactly what that field exists for.
func TestOIDCAuthenticateCarriesClaims(t *testing.T) {
	iss, priv, kid := jwksTestServer(t)
	token := mintRS256(t, priv, kid, map[string]any{
		"iss": iss.URL, "sub": "alice@example.com", "exp": time.Now().Add(time.Hour).Unix(),
		"email": "alice@example.com",
	})
	auth := &OIDCAuthenticator{Issuer: iss.URL, HTTPClient: iss.Client()}
	req := httptest.NewRequest("POST", "/v1/ingress", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	subj, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("a verifiable bearer was refused: %v", err)
	}
	if subj.Name != "alice@example.com" || subj.Method != MethodOIDC {
		t.Fatalf("subject = %+v, want name alice@example.com method oidc", subj)
	}
	if subj.Claims["email"] != "alice@example.com" {
		t.Fatalf("claims = %+v, want email carried through", subj.Claims)
	}
}
