//go:build linux

package api

import (
	"net"
	"syscall"
)

// peerCredOf reads SO_PEERCRED off an accepted unix-socket connection — the
// kernel's own account of who is on the other end. Anything that is not a
// live *net.UnixConn (a TCP test server, a connection the kernel could not
// answer for) reports not-ok rather than guessing at a peer.
func peerCredOf(c net.Conn) peerCred {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return peerCred{}
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}
	}
	var cred *syscall.Ucred
	var gerr error
	if cerr := raw.Control(func(fd uintptr) {
		cred, gerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); cerr != nil {
		return peerCred{}
	}
	if gerr != nil || cred == nil {
		return peerCred{}
	}
	return peerCred{uid: cred.Uid, pid: cred.Pid, ok: true}
}
