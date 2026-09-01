// Command boxgate is a seam over the daemon's socket: it adds a network
// listener and an authentication check to a boxd that has neither, by
// design. The daemon's trust model is its socket's file mode; everything
// network-shaped — this token gate today, OIDC or mTLS or a cloud IAP
// tomorrow — is a layer like this one, composed in front without the daemon
// changing.
//
//	boxgate -socket boxd.sock -listen 127.0.0.1:8437 -token s3cret
//
// The token rides the Authorization header; because a browser's EventSource
// cannot set headers, a valid ?token= mints a cookie and both are accepted.
// All of that lives HERE, not in the daemon — which is the point.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/gateway"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("boxgate " + version.String())
		os.Exit(0)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "boxgate:", err)
		os.Exit(1)
	}
}

func run() error {
	socket := flag.String("socket", "boxd.sock", "the daemon's unix socket")
	listen := flag.String("listen", "", "network address to expose, e.g. 127.0.0.1:8437")
	token := flag.String("token", "", "bearer token callers must present")
	insecureLoopback := flag.Bool("insecure-loopback", false, "allow a tokenless gate on 127.0.0.1 only (local dev)")
	flag.Parse()
	if *listen == "" {
		return errors.New("-listen is required — a gate exists to expose the socket")
	}
	if *token == "" {
		host, _, err := net.SplitHostPort(*listen)
		if err != nil || !*insecureLoopback || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
			return errors.New("a tokenless gate is refused off-loopback — pass -token, or -insecure-loopback with a 127.0.0.1 listen")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h := gateway.Gate(*socket, *token)
	srv := &http.Server{Addr: *listen, Handler: h}
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shctx)
	}()
	slog.Info("boxgate up", "socket", *socket, "listen", *listen, "auth", map[bool]string{true: "token", false: "loopback-only"}[*token != ""])
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
