#!/bin/sh
# Start the factory, pointed at this repository (issue #7).
#
# From nothing: builds the binaries, makes the state directory, and runs the
# daemon on its socket. Everything it writes lands in .factory/, which is
# gitignored — the harness is versioned, its state is not.
#
# Two environment facts this needs, both of them findings rather than taste:
#
#  1. internal/forge/git/git.go runs every git child with
#     GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_SYSTEM=/dev/null for
#     hermeticity. That also discards the only place a credential helper can
#     be configured, so the forge cannot reach a private HTTPS remote on ANY
#     machine. GIT_CONFIG_COUNT is the one config channel that survives it.
#     The seam is still missing (recorded in a96320e); this is the workaround.
#
#  2. `gh auth git-credential` is the helper because it needs no token at
#     rest — it answers from the gh session already on this machine.
#
set -eu
cd "$(dirname "$0")/../.."

mkdir -p .factory
go build -o bin/boxd ./cmd/boxd
go build -o bin/box ./cmd/box

# api.Listen clears the socket path before binding, and it cannot tell a
# stale socket from a live one — so a second daemon started here would take
# the socket from the first and leave it running but unreachable. The store's
# leases and fencing would keep the WORK correct; the control room would just
# stop answering. Cheaper to refuse.
if [ -S .factory/boxd.sock ] &&
   ./bin/box -tenant davenport -daemon unix:.factory/boxd.sock status >/dev/null 2>&1; then
  echo "a daemon is already answering on .factory/boxd.sock — refusing to take its socket" >&2
  exit 1
fi

export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0=credential.helper
export GIT_CONFIG_VALUE_0='!gh auth git-credential'

exec ./bin/boxd -config hack/dogfood/box.yaml
