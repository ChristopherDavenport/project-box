package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/kind"
)

// TestPeerAuthenticatorRefusesWithNoPeer: the type mustActor's nil-Auth path
// resolves to must answer exactly the way mustActor always did — refused,
// never a guessed name — when the connection carries no kernel-asserted
// peer.
func TestPeerAuthenticatorRefusesWithNoPeer(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/stop", nil)
	_, err := PeerAuthenticator{}.Authenticate(req)
	if err == nil || !strings.Contains(err.Error(), "peer") {
		t.Fatalf("err = %v, want a refusal naming the missing peer", err)
	}
}

// TestTokenAuthenticatorAcceptsAndRejects: a matching bearer resolves to the
// token's own name and method "token"; a wrong or absent bearer is refused,
// never defaulted to any table entry.
func TestTokenAuthenticatorAcceptsAndRejects(t *testing.T) {
	auth := TokenAuthenticator{Tokens: map[string]string{"ci": "s3cret", "partner": "other-secret"}}

	req := httptest.NewRequest("POST", "/v1/ingress", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	subj, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("a matching token was refused: %v", err)
	}
	if subj.Name != "ci" || subj.Method != MethodToken {
		t.Fatalf("subject = %+v, want name ci method token", subj)
	}

	wrong := httptest.NewRequest("POST", "/v1/ingress", nil)
	wrong.Header.Set("Authorization", "Bearer nope")
	if _, err := auth.Authenticate(wrong); err == nil {
		t.Fatal("a wrong token was accepted")
	}

	absent := httptest.NewRequest("POST", "/v1/ingress", nil)
	if _, err := auth.Authenticate(absent); err == nil {
		t.Fatal("a request with no Authorization header was accepted")
	}
}

// TestNoneAuthenticatorRefusesEverything: "none" is a configuration a box
// can name explicitly, and it must mean exactly what it says — no request,
// however it arrives, resolves to a subject.
func TestNoneAuthenticatorRefusesEverything(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/stop", nil)
	req.Header.Set("Authorization", "Bearer anything")
	if _, err := (NoneAuthenticator{}).Authenticate(req); err == nil {
		t.Fatal("none accepted a request")
	}
}

// TestOpenIngressOverAPeerConnectionRecordsThePeerAsSource: with no [Server.Auth]
// configured (nil, the default), a delivery over the real unix socket
// records the wire's own peer as the delivery's Source — the test process's
// own account name, method "peer" — never a header, never absent.
func TestOpenIngressOverAPeerConnectionRecordsThePeerAsSource(t *testing.T) {
	w := rig(t)
	c := dialUnix(t, w, "")

	res, err := c.OpenIngress(context.Background(), "issues", "peer-key", "task text", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	wantSubject := accountName(uint32(os.Getuid()))
	if res.Ingress.Source.Subject != wantSubject || res.Ingress.Source.Method != MethodPeer {
		t.Fatalf("source = %+v, want subject %q method %q", res.Ingress.Source, wantSubject, MethodPeer)
	}
}

// TestOpenIngressOverATokenRecordsTheTokensNameAsSource: a "token"-authenticated
// write records the token's own configured name and method "token" —
// exactly what a static-token box needs to answer "who delivered this".
func TestOpenIngressOverATokenRecordsTheTokensNameAsSource(t *testing.T) {
	w := rig(t)
	w.srv.Auth = TokenAuthenticator{Tokens: map[string]string{"ci": "s3cret"}}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "s3cret", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.OpenIngress(context.Background(), "issues", "token-key", "task text", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if res.Ingress.Source.Subject != "ci" || res.Ingress.Source.Method != MethodToken {
		t.Fatalf("source = %+v, want subject ci method token", res.Ingress.Source)
	}
}

// TestStopWithAWrongTokenIsRefused: mustActor's hard-refusal path under a
// "token" auth config — a wrong bearer must refuse the mutation outright,
// the same doctrine [TestMutationOverTCPIsRefusedForWantOfAPeer] proves for
// the peer default.
func TestStopWithAWrongTokenIsRefused(t *testing.T) {
	w := rig(t)
	w.srv.Auth = TokenAuthenticator{Tokens: map[string]string{"ci": "s3cret"}}
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	c, err := Dial("tcp:"+strings.TrimPrefix(ts.URL, "http://"), "wrong-token", "")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Stop(context.Background(), "investigating")
	if err == nil {
		t.Fatal("a wrong token was accepted")
	}
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403", err)
	}
}

// TestARouteRequiringTokenRefusesAPeerDelivery is issue #188's own example:
// a route configured to require "token" refuses a delivery that
// authenticated as the socket's peer — even though the peer itself is
// perfectly legitimate — and the refusal lands on the route's own standing
// chain (the pollScope precedent, internal/ingress/poll.go), not just in
// the one response the caller saw.
func TestARouteRequiringTokenRefusesAPeerDelivery(t *testing.T) {
	w := rig(t)
	w.srv.SetRoutes(map[string]ingress.Route{"issues": {
		Name: "issues", Kind: ingress.RouteIssue, Opens: kind.SeedIssue, Box: "shop", Requester: "daemon",
		Require: []string{MethodToken},
	}})
	c := dialUnix(t, w, "")

	_, err := c.OpenIngress(context.Background(), "issues", "gated-key", "task text", "")
	if err == nil {
		t.Fatal("a peer delivery was accepted by a route requiring token")
	}
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusForbidden || !strings.Contains(apiErr.Message, MethodToken) {
		t.Fatalf("err = %v, want a 403 naming token", err)
	}

	found := false
	for rec, err := range w.srv.Store.Log().Since(context.Background(), tn, "route/issues", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == "ingress-refused" {
			found = true
			if !strings.Contains(string(rec.Body), MethodToken) {
				t.Fatalf("refusal body = %s, want it to name the required method", rec.Body)
			}
		}
	}
	if !found {
		t.Fatal("no ingress-refused record on the route's own chain")
	}
}

// TestDeclaredChannelIsRecordedAsAClaim: an operator-declared channel rides
// straight onto the delivery's Source; an undeclared one stays empty rather
// than defaulting to "hand", and an unknown value is refused outright. The
// Go client has no Channel parameter, so this drives the raw wire directly,
// same as [TestMutationOverTCPIsRefusedForWantOfAPeer].
func TestDeclaredChannelIsRecordedAsAClaim(t *testing.T) {
	w := rig(t)
	ts := httptest.NewServer(w.srv.Handler())
	t.Cleanup(ts.Close)

	post := func(t *testing.T, body string) *http.Response {
		t.Helper()
		resp, err := http.Post(ts.URL+"/v1/ingress", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	// Absent: never defaulted to "hand".
	resp := post(t, `{"route":"issues","key":"no-channel","payload":"task"}`)
	var res OpenIngressResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if res.Ingress.Source.Channel != "" {
		t.Fatalf("channel = %q, want empty when the caller declared none", res.Ingress.Source.Channel)
	}

	// Declared "hand": recorded verbatim as the caller's claim.
	resp = post(t, `{"route":"issues","key":"hand-channel","payload":"task","channel":"hand"}`)
	res = OpenIngressResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Ingress.Source.Channel != "hand" {
		t.Fatalf("channel = %q, want \"hand\"", res.Ingress.Source.Channel)
	}

	// An unknown value is refused outright.
	resp = post(t, `{"route":"issues","key":"bad-channel","payload":"task","channel":"carrier-pigeon"}`)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an undeclared channel value: %s", resp.StatusCode, body)
	}
}
