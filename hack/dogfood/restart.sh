#!/bin/sh
# Restart the factory on a rebuilt binary, with the reason on the record.
#
#   ./hack/dogfood/restart.sh "why"
#
# Stops new claims with the reason, waits for whatever is running to settle
# (a killed run produces nothing at all, so this never cuts one short), stops
# the daemon and the room, rebuilds both binaries from this tree through
# run.sh, brings them back, and resumes claims with the same reason. The
# reason is what the chain shows beside the stop and the resume, so say what
# the restart is for.
#
# Processes are found by pid through the socket and the port, never by
# pattern — a pattern matched this script's own shell once and killed it.
set -u
cd "$(dirname "$0")/../.."
reason=${1:?a reason for the restart, e.g. "roles fix deployed"}
export PATH=$HOME/coding/ai/buzz-zero-scaling/node_modules/.bin:$PATH

# One-time migration (issue #73), guarded the same way as run.sh's and run
# here too: this script writes into ../project-box-state below (the log
# mv's, then run.sh's own redirect) before run.sh gets a chance to migrate,
# so on the transition restart run.sh's copy of this guard fires too late —
# the redirect into a directory that does not exist yet kills run.sh before
# its first line runs. `mv` within one filesystem only relinks the
# directory entry, so a still-running old daemon's socket file keeps
# answering at its new path afterward — the sock= line below and the
# API/pid checks that follow it stay correct against the OLD daemon even
# though .factory is already gone by the time they run.
if [ -d .factory ] && [ ! -e ../project-box-state ]; then
  mv .factory ../project-box-state
fi

sock=../project-box-state/boxd.sock
log() { echo "$(date +%H:%M:%S) $*"; }
api() { path=$1; shift; curl -s --unix-socket "$sock" "http://box/v1/$path" -H 'content-type: application/json' "$@"; }
running() { api status | python3 -c 'import json,sys; print(json.load(sys.stdin)["spend"]["running"])' 2>/dev/null || echo "?"; }

if [ -S "$sock" ] && api status >/dev/null 2>&1; then
  api stop -X POST -d "{\"reason\":\"restart: $reason\"}" >/dev/null
  log "claims stopped; waiting for running work to settle"
  while [ "$(running)" != 0 ]; do sleep 10; done
  # By name and argument, never by pattern over every process: a pattern
  # matched this script's own shell once and killed it.
  pid=$(pgrep -x boxd -a | grep -F 'hack/dogfood/box.yaml' | awk '{print $1}' | head -1)
  if [ -n "$pid" ]; then
    log "stopping boxd pid=$pid"
    kill "$pid"
    i=0; while kill -0 "$pid" 2>/dev/null && [ $i -lt 120 ]; do sleep 1; i=$((i+1)); done
    if kill -0 "$pid" 2>/dev/null; then log "boxd still up after 120s; not forcing it"; exit 1; fi
  fi
fi
room=$(lsof -t -iTCP:8477 -sTCP:LISTEN 2>/dev/null || true)
[ -n "$room" ] && { kill $room; log "room pid=$room stopped"; }

mv -f ../project-box-state/boxd.log ../project-box-state/boxd.prev.log 2>/dev/null || true
mv -f ../project-box-state/room.log ../project-box-state/room.prev.log 2>/dev/null || true
setsid nohup ./hack/dogfood/run.sh > ../project-box-state/boxd.log 2>&1 < /dev/null &
log "run.sh started: generate, build, run"
i=0; until [ -S "$sock" ] && ./bin/box -tenant davenport -daemon "unix:$sock" status >/dev/null 2>&1; do
  sleep 1; i=$((i+1)); [ $i -ge 180 ] && { log "boxd did not come up in 180s; see ../project-box-state/boxd.log"; exit 1; }
done
setsid nohup ./hack/dogfood/room.sh > ../project-box-state/room.log 2>&1 < /dev/null &
i=0; until curl -s -o /dev/null http://127.0.0.1:8477/; do sleep 1; i=$((i+1)); [ $i -ge 30 ] && break; done
api resume -X POST -d "{\"reason\":\"restart: $reason — claims back on\"}" >/dev/null
log "up: $(./bin/box -tenant davenport -daemon "unix:$sock" status | head -1)"
log "room: http://127.0.0.1:8477/ ($(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8477/))"
