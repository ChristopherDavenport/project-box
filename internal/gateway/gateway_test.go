package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backend fakes the daemon on a unix socket: a /v1 endpoint, the UI shell,
// and an SSE stream.
func backend(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tenant":"acme"}`))
	})
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := range 3 {
			io.WriteString(w, "event: activity\ndata: {\"n\":")
			io.WriteString(w, string(rune('0'+i)))
			io.WriteString(w, "}\n\n")
			fl.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<box-app></box-app>"))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

// TestGateAuthAndPassThrough: the relocated posture — /v1 needs the token
// (header, query, or the cookie a valid query mints), the shell loads
// without it, and everything proxies to the socket.
func TestGateAuthAndPassThrough(t *testing.T) {
	sock := backend(t)
	ts := httptest.NewServer(Gate(sock, "s3cret"))
	defer ts.Close()

	get := func(path string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// The shell is public embedded code; it loads tokenless.
	if res := get("/", nil); res.StatusCode != 200 {
		t.Fatalf("shell: %d", res.StatusCode)
	}
	// /v1 is the data; tokenless and wrong-token are refused at the GATE —
	// the daemon behind the socket never sees them.
	if res := get("/v1/status", nil); res.StatusCode != 401 {
		t.Fatalf("tokenless /v1: %d", res.StatusCode)
	}
	if res := get("/v1/status", map[string]string{"Authorization": "Bearer wrong"}); res.StatusCode != 401 {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	if res := get("/v1/status", map[string]string{"Authorization": "Bearer s3cret"}); res.StatusCode != 200 {
		t.Fatalf("right token: %d", res.StatusCode)
	}
	// A valid ?token= mints the cookie at the gate.
	res := get("/?token=s3cret", nil)
	cookie := ""
	for _, c := range res.Cookies() {
		if c.Name == "box_token" {
			cookie = c.Value
		}
	}
	if cookie != "s3cret" {
		t.Fatalf("cookie not minted: %v", res.Cookies())
	}
	if res := get("/v1/status", map[string]string{"Cookie": "box_token=s3cret"}); res.StatusCode != 200 {
		t.Fatalf("cookie auth: %d", res.StatusCode)
	}
}

// TestGateStreamsSSE: frames arrive as they are sent, not when a buffer
// fills — the live feed's whole point.
func TestGateStreamsSSE(t *testing.T) {
	sock := backend(t)
	ts := httptest.NewServer(Gate(sock, ""))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	frames := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data: ") {
			frames++
		}
		if frames == 3 {
			return
		}
	}
	t.Fatalf("only %d frames arrived through the gate", frames)
}
