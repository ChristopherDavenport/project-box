//go:build !linux

package api

import "net"

// peerCredOf has no SO_PEERCRED to read outside Linux. The daemon still
// listens on a unix socket only, so this reports no peer rather than
// trusting anything the caller sent — mutating calls simply refuse here
// until this is ported, the same answer as a TCP test server gets.
func peerCredOf(net.Conn) peerCred {
	return peerCred{}
}
