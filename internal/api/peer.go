package api

import (
	"context"
	"fmt"
	"net"
	"os/user"
	"strconv"
)

// peerCred is the kernel-asserted credential of a unix-socket connection's
// far end. Never built from a header: this package's whole trust model is
// the socket, and a struct a caller could populate by claiming a field would
// just be a header with extra steps.
type peerCred struct {
	uid uint32
	pid int32
	ok  bool
}

type peerCredKey struct{}

// ConnContext wires a connection's peer credential into every request that
// connection carries, for assignment to http.Server.ConnContext. Both of the
// daemon's serve sites (cmd/boxd and internal/daemon) must set it — this is
// the one helper they share rather than each reading SO_PEERCRED its own
// way.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, peerCredKey{}, peerCredOf(c))
}

func peerFromContext(ctx context.Context) peerCred {
	cred, _ := ctx.Value(peerCredKey{}).(peerCred)
	return cred
}

// accountName resolves a uid to the name the system knows it by, falling
// back to "uid:<n>" when the system has no answer — an unresolvable uid is
// still an identity worth recording, never a reason to lose the fact.
func accountName(uid uint32) string {
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil && u.Username != "" {
		return u.Username
	}
	return fmt.Sprintf("uid:%d", uid)
}
