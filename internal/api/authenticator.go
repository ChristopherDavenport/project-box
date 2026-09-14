package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// Method names an Authenticator's own vouching method — the same vocabulary
// [ingress.Source.Method] and [ingress.Route.Require] read and write. Kept
// as plain strings rather than a closed type: a future implementation
// (oidc, and whatever comes after it) is a new value, never a new type the
// rest of this package has to know about.
const (
	MethodPeer  = "peer"
	MethodToken = "token"
	MethodNone  = "none"
)

// Subject is what an Authenticator answers for one request: who it is, and
// what vouched. Never built from anything the caller wrote in a header —
// that is exactly what the interface exists to establish, the same doctrine
// [peerCred] states for the socket case.
type Subject struct {
	Name   string
	Method string
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

// NoneAuthenticator refuses every write. It exists so "no method may write"
// is a configuration a box can name explicitly, rather than only being
// reachable by misconfiguring something else.
type NoneAuthenticator struct{}

// Authenticate always refuses.
func (NoneAuthenticator) Authenticate(r *http.Request) (Subject, error) {
	return Subject{}, fmt.Errorf("this daemon's auth is configured as none — no method may write")
}
