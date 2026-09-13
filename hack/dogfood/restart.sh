#!/bin/sh
# Restart the factory on a rebuilt binary, with the reason on the record.
#
#   ./hack/dogfood/restart.sh "why"
#
# Stops new claims with the reason, waits for whatever is running to settle
# (a killed run produces nothing at all, so this never cuts one short), asks
# systemd to restart the daemon and room units (install.sh's), waits for the
# socket to answer again, and resumes claims with the same reason. The
# reason is what the chain shows beside the stop and the resume, so say what
# the restart is for.
#
# Process supervision now belongs to systemd --user (hack/dogfood/install.sh):
# this script no longer discovers a pid, forks with nohup/setsid, or rotates
# log files.
#   - Discovery: systemd finds "the daemon" by the unit's cgroup, not a
#     process-list pattern. The old doctrine here was "never by pattern — a
#     pattern matched this script's own shell once and killed it"; that
#     hazard cannot recur once nothing here greps ps.
#   - Logs: journald owns them now. `journalctl --user -u
#     project-box-dogfood-boxd.service -f`, not boxd.log/boxd.prev.log.
#   - The one-time .factory migration guard duplicated here (issue #73) is
#     gone too: it existed only because this script used to write log files
#     into ../project-box-state before run.sh's own copy of the guard could
#     fire. Nothing here writes there anymore, so run.sh's guard is the only
#     one that needs to exist.
#
# Refuses rather than falling back to the old pid-hunting path when the
# units are not installed — two divergent restart paths is exactly what
# this rewrite removes.
#
# This is for restarts an operator asks for (a landed change outside
# self_deploy's sections, an edited box.yaml, a manual bounce) — box.yaml's
# self_deploy chore re-execs the running process in place (syscall.Exec,
# same pid) and never goes through systemd or this script at all.
set -eu
cd "$(dirname "$0")/../.."

reason=${1:?a reason for the restart, e.g. "roles fix deployed"}

BOXD_UNIT=project-box-dogfood-boxd.service
ROOM_UNIT=project-box-dogfood-room.service

for u in "$BOXD_UNIT" "$ROOM_UNIT"; do
  if ! systemctl --user cat "$u" >/dev/null 2>&1; then
    echo "restart.sh: $u is not installed — run hack/dogfood/install.sh first" >&2
    exit 1
  fi
done

sock=../project-box-state/boxd.sock
log() { echo "$(date +%H:%M:%S) $*"; }
api() { path=$1; shift; curl -s --unix-socket "$sock" "http://box/v1/$path" -H 'content-type: application/json' "$@"; }
running() { api status | python3 -c 'import json,sys; print(json.load(sys.stdin)["spend"]["running"])' 2>/dev/null || echo "?"; }

if [ -S "$sock" ] && api status >/dev/null 2>&1; then
  api stop -X POST -d "{\"reason\":\"restart: $reason\"}" >/dev/null
  log "claims stopped; waiting for running work to settle"
  while [ "$(running)" != 0 ]; do sleep 10; done
fi

log "restarting $BOXD_UNIT and $ROOM_UNIT"
systemctl --user restart "$BOXD_UNIT" "$ROOM_UNIT"

i=0; until [ -S "$sock" ] && ./bin/box -tenant davenport -daemon "unix:$sock" status >/dev/null 2>&1; do
  sleep 1; i=$((i+1))
  [ $i -ge 180 ] && { log "boxd did not come up in 180s; see: journalctl --user -u $BOXD_UNIT -n100"; exit 1; }
done
api resume -X POST -d "{\"reason\":\"restart: $reason — claims back on\"}" >/dev/null
log "up: $(./bin/box -tenant davenport -daemon "unix:$sock" status | head -1)"
log "room: http://127.0.0.1:8477/ ($(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8477/))"
