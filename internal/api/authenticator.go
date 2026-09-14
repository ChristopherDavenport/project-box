package api

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Method names an Authenticator's own vouching method — the same vocabulary
// [ingress.Source.Method] and [ingress.Route.Require] read and write. Kept
// as plain strings rather than a closed type: a future implementation
// (oidc, and whatever comes after it) is a new value, never a new type the
// rest of this package has to know about.
const (
	MethodPeer  = "peer"
	MethodToken = "token"
	MethodOIDC  = "oidc"
	MethodNone  = "none"
)

// Subject is what an Authenticator answers for one request: who it is, and
// what vouched. Never built from anything the caller wrote in a header —
// that is exactly what the interface exists to establish, the same doctrine
// [peerCred] states for the socket case.
type Subject struct {
	Name   string
	Method string
	// Claims carries the token's own claim set, for a method that has
	// claims to carry (oidc). Nil for peer/token/none, which vouch for a
	// name but were never handed anything beyond it.
	Claims map[string]any
}

// Authenticator resolves who a request acts as. Every implementation must
// refuse rather than guess: a Subject this package cannot vouch for must
// never be returned alongside a nil error.
type Authenticator interface {
	Authenticate(r *http.Request) (Subject, error)
}

// PeerAuthenticator is the local-first default: SO_PEERCRED, the kernel's
// own account of who is on the unix socket's far end. It is also what a nil
// [Server.Auth] resolves to — see [Server.authenticator] — so this type is
// the whole of "today's behavior" mustActor built by hand before this file
// existed.
type PeerAuthenticator struct{}

// Authenticate refuses a connection with no resolvable peer — a TCP test
// server, non-linux, no ucred available — rather than defaulting to any
// name; "local-operator" does not exist here for the same reason it never
// did.
func (PeerAuthenticator) Authenticate(r *http.Request) (Subject, error) {
	cred := peerFromContext(r.Context())
	if !cred.ok {
		return Subject{}, fmt.Errorf("no unix-socket peer was asserted for this connection — mutating calls require one")
	}
	return Subject{Name: accountName(cred.uid), Method: MethodPeer}, nil
}

// TokenAuthenticator is a static name-to-secret table, populated once at
// assembly from box.yaml's resolved (file- or env-sourced) auth.tokens —
// never built from anything inline in the config, and never from anything
// the caller wrote beyond the bearer value itself.
type TokenAuthenticator struct {
	// Tokens maps a principal name to its shared secret.
	Tokens map[string]string
}

// Authenticate constant-time compares the Authorization: Bearer header
// against every configured secret. Absent or malformed headers, and a
// bearer value that matches nothing, are refused with the same reason —
// the code, not the message, is what a caller may depend on.
func (t TokenAuthenticator) Authenticate(r *http.Request) (Subject, error) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return Subject{}, fmt.Errorf("token auth requires an Authorization: Bearer header")
	}
	given := []byte(strings.TrimPrefix(h, prefix))
	for name, secret := range t.Tokens {
		if subtle.ConstantTimeCompare(given, []byte(secret)) == 1 {
			return Subject{Name: name, Method: MethodToken}, nil
		}
	}
	return Subject{}, fmt.Errorf("token not recognized")
}

// jwksCacheTTL is how long OIDCAuthenticator trusts a fetched key set before
// asking the issuer again — long enough that an ordinary request never pays
// a network round trip, short enough that a rotated key set is picked up
// within one operator's coffee break rather than only at a restart.
const jwksCacheTTL = 10 * time.Minute

// OIDCAuthenticator verifies an Authorization: Bearer JWT against a
// configured issuer — stdlib only, matching this repository's no-third-party
// convention: crypto/rsa and crypto/sha256 verify RS256 signatures,
// encoding/base64 (RawURLEncoding) decodes the JOSE segments. Every other
// alg, including "none", is refused by name — see Authenticate.
//
// Keys are fetched lazily, on first use, and cached for jwksCacheTTL rather
// than fetched at construction: the issue that adds this type is explicit
// that the fetch must NEVER happen at boxcfg.Load (which stays
// network-free), and lazy-on-first-request also means a daemon still starts
// when its issuer is briefly unreachable — the first authenticated request
// pays the fetch, not the daemon's own boot.
type OIDCAuthenticator struct {
	// Issuer is the token's required iss claim, and — unless JWKSURL
	// overrides it — where keys are discovered: Issuer +
	// "/.well-known/openid-configuration" is fetched for its jwks_uri.
	Issuer string
	// Audience, when non-empty, is the value the token's aud claim must
	// contain (a string or an array of strings, per the JWT spec). Empty
	// leaves aud unchecked.
	Audience string
	// JWKSURL, when set, is fetched directly instead of discovering it from
	// Issuer's own well-known document.
	JWKSURL string
	// HTTPClient makes the discovery and JWKS requests. Nil defaults to
	// http.DefaultClient; a test substitutes an httptest.Server's client.
	HTTPClient *http.Client

	mu     sync.Mutex
	keys   map[string]*rsa.PublicKey
	keysAt time.Time
}

func (o *OIDCAuthenticator) httpClient() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return http.DefaultClient
}

// jwk is one entry of a JWKS document's "keys" array — only the fields an
// RSA verification key needs; anything else in the document is ignored.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// discoverJWKSURL reads Issuer's own OpenID discovery document for its
// jwks_uri — the standard way an OIDC relying party learns where an issuer
// publishes keys, used whenever JWKSURL was not configured directly.
func (o *OIDCAuthenticator) discoverJWKSURL(ctx context.Context) (string, error) {
	wellKnown := strings.TrimSuffix(o.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return "", fmt.Errorf("oidc: building discovery request: %w", err)
	}
	resp, err := o.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc: GET %s: %w", wellKnown, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oidc: GET %s: %s", wellKnown, resp.Status)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("oidc: decoding discovery document from %s: %w", wellKnown, err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("oidc: discovery document at %s has no jwks_uri", wellKnown)
	}
	return doc.JWKSURI, nil
}

// fetchKeys resolves the JWKS URL (JWKSURL, or discovered from Issuer) and
// parses every RSA key it names into Go's own rsa.PublicKey, keyed by kid.
// A key this cannot parse — wrong kty, an unparseable n or e — is simply
// left out of the map: verification against a kid that never resolves to a
// key is refused the same way a kid absent from the JWKS entirely is.
func (o *OIDCAuthenticator) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	jwksURL := o.JWKSURL
	if jwksURL == "" {
		var err error
		jwksURL, err = o.discoverJWKSURL(ctx)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: building JWKS request: %w", err)
	}
	resp, err := o.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: GET %s: %w", jwksURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: GET %s: %s", jwksURL, resp.Status)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("oidc: decoding JWKS from %s: %w", jwksURL, err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

// rsaPublicKeyFromJWK decodes a JWK's base64url-encoded modulus (n) and
// exponent (e) — RFC 7518 §6.3.1 — into an *rsa.PublicKey.
func rsaPublicKeyFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decoding n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decoding e: %w", err)
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("exponent decoded to zero")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// key answers the RSA public key for kid, fetching (or refreshing) the
// cached key set as needed: a cache older than jwksCacheTTL is refetched
// before lookup, and a kid absent from an otherwise-fresh cache triggers one
// forced refresh — key rotation between two TTL windows must not be
// mistaken for an unknown key id.
func (o *OIDCAuthenticator) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.keys == nil || time.Since(o.keysAt) >= jwksCacheTTL {
		keys, err := o.fetchKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("oidc: fetching keys: %w", err)
		}
		o.keys, o.keysAt = keys, time.Now()
	}
	if k, ok := o.keys[kid]; ok {
		return k, nil
	}
	keys, err := o.fetchKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("oidc: refreshing keys for kid %q: %w", kid, err)
	}
	o.keys, o.keysAt = keys, time.Now()
	if k, ok := o.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("oidc: no key %q in the issuer's JWKS", kid)
}

// audienceContains reports whether an aud claim — a string or a JSON array
// of strings, per RFC 7519 §4.1.3 — contains want.
func audienceContains(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// Authenticate verifies the Authorization: Bearer header as an RS256 JWT:
// signature against the issuer's own published key, iss against Issuer, exp
// (required — a token missing it is refused, never treated as unexpiring),
// nbf when present, and aud when Audience names one. The resulting Subject
// carries the token's sub as principal (required — a token missing it is
// refused, never treated as anonymous) and MethodOIDC, with the claims the
// token carried.
//
// alg is checked before anything else touches the network or a key: "none"
// is refused unconditionally, and any alg other than "RS256" is refused by
// name — this authenticator verifies RS256 only.
func (o *OIDCAuthenticator) Authenticate(r *http.Request) (Subject, error) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return Subject{}, fmt.Errorf("oidc auth requires an Authorization: Bearer header")
	}
	token := strings.TrimPrefix(h, prefix)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Subject{}, fmt.Errorf("oidc: not a JWT — want three dot-separated segments, got %d", len(parts))
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Subject{}, fmt.Errorf("oidc: decoding header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Subject{}, fmt.Errorf("oidc: parsing header: %w", err)
	}
	if header.Alg == "none" {
		return Subject{}, fmt.Errorf("oidc: alg \"none\" is refused unconditionally")
	}
	if header.Alg != "RS256" {
		return Subject{}, fmt.Errorf("oidc: alg %q is not supported — this daemon verifies RS256 only", header.Alg)
	}
	if header.Kid == "" {
		return Subject{}, fmt.Errorf("oidc: token header names no kid")
	}
	key, err := o.key(r.Context(), header.Kid)
	if err != nil {
		return Subject{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Subject{}, fmt.Errorf("oidc: decoding signature: %w", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return Subject{}, fmt.Errorf("oidc: signature verification failed: %w", err)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Subject{}, fmt.Errorf("oidc: decoding claims: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return Subject{}, fmt.Errorf("oidc: parsing claims: %w", err)
	}
	if iss, _ := claims["iss"].(string); iss != o.Issuer {
		return Subject{}, fmt.Errorf("oidc: iss %q does not match the configured issuer %q", iss, o.Issuer)
	}
	expAt, ok := claims["exp"].(float64)
	if !ok {
		return Subject{}, fmt.Errorf("oidc: token has no exp claim — a token this cannot expire-check is refused, never treated as unexpiring")
	}
	now := time.Now()
	if exp := time.Unix(int64(expAt), 0); now.After(exp) {
		return Subject{}, fmt.Errorf("oidc: token expired at %s", exp.UTC())
	}
	if nbfAt, ok := claims["nbf"].(float64); ok {
		if nbf := time.Unix(int64(nbfAt), 0); now.Before(nbf) {
			return Subject{}, fmt.Errorf("oidc: token not valid before %s", nbf.UTC())
		}
	}
	if o.Audience != "" && !audienceContains(claims["aud"], o.Audience) {
		return Subject{}, fmt.Errorf("oidc: aud does not contain the configured audience %q", o.Audience)
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return Subject{}, fmt.Errorf("oidc: token has no sub claim — a token this cannot name a principal for is refused, never treated as anonymous")
	}
	return Subject{Name: sub, Method: MethodOIDC, Claims: claims}, nil
}

// NoneAuthenticator refuses every write. It exists so "no method may write"
// is a configuration a box can name explicitly, rather than only being
// reachable by misconfiguring something else.
type NoneAuthenticator struct{}

// Authenticate always refuses.
func (NoneAuthenticator) Authenticate(r *http.Request) (Subject, error) {
	return Subject{}, fmt.Errorf("this daemon's auth is configured as none — no method may write")
}
