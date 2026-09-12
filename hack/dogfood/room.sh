#!/bin/sh
# The control room: fronts the daemon's socket on a loopback port and prints
# the URL. Tokenless on 127.0.0.1 by design — the socket's file mode is the
# trust model, and anything past loopback is boxgate's job.
set -eu
cd "$(dirname "$0")/../.."
exec ./bin/box -tenant davenport room -socket ../project-box-state/boxd.sock -port 8477
