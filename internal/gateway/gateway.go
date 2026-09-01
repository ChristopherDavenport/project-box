// Package gateway is the seam-over-the-socket: authentication plus a
// reverse proxy to a daemon that is, by design, socket-only. cmd/boxgate
// serves it on a network address; `box room` serves it tokenless on
// loopback; an OIDC or mTLS gate replaces the token check here without the
// daemon ever knowing.
package gateway

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// Gate builds the proxy handler: authentication, then a straight reverse
// proxy to the socket. Exported so `box room` and tests reuse the exact
// handler the binary serves.
func Gate(socket, token string) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = "boxd"
			r.SetXForwarded()
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
		// SSE needs frames now, not when a buffer fills.
		FlushInterval: 100 * time.Millisecond,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if got == "" {
				if c, err := r.Cookie("box_token"); err == nil {
					got = c.Value
				}
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				// The UI shell may load without auth — it is public embedded
				// code and cannot leak what /v1 refuses to serve it.
				if strings.HasPrefix(r.URL.Path, "/v1/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `{"error":"missing or wrong bearer token"}`)
					return
				}
			} else if q := r.URL.Query().Get("token"); q != "" {
				// Mint the cookie at the gate so EventSource and every later
				// /v1 call are authed with no JS bootstrap race.
				http.SetCookie(w, &http.Cookie{Name: "box_token", Value: q, Path: "/", SameSite: http.SameSiteStrictMode})
			}
		}
		proxy.ServeHTTP(w, r)
	})
}
