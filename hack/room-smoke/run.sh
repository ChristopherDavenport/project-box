#!/usr/bin/env bash
# The control-room browser smoke, end to end from nothing: two bare remotes,
# boxd on a unix socket, boxgate fronting it with a token, then a real
# chromium driving the UI — dashboard live, submit through the form, the
# landing on the SSE feed, hot reload on the running daemon, the new route
# in the form. Needs: go, git, node (installs playwright + chromium on
# first run).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
SOCK=/tmp/boxd-smoke-$$.sock
PORT=${PORT:-8437}
TOKEN=room-smoke-token
trap 'kill $(cat "$WORK"/*.pid 2>/dev/null) 2>/dev/null || true; rm -f "$SOCK"' EXIT

step() { printf '\n== %s ==\n' "$*"; }

step "remotes"
for R in api web; do
  git init -q --bare "$WORK/$R.git"
  git -C "$WORK/$R.git" symbolic-ref HEAD refs/heads/main
  git clone -q "$WORK/$R.git" "$WORK/$R-seed" 2>/dev/null
  ( cd "$WORK/$R-seed" && echo "$R" > README.md && git add . \
    && GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@i GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@i \
       git commit -qm seed && git push -q origin HEAD:refs/heads/main )
done

step "daemon (single repo) + gate"
BUILD='date +%s%N > out.txt && git add -A && git -c user.name=b -c user.email=b@i commit -m'
cat > "$WORK/box.yaml" <<YAML
tenant: acme
db: $WORK/box.db
root: $WORK/forge
listen: unix:$SOCK
every: 1s
budget: {max_concurrent: 2}
boxes:
  - name: shop
    repos:
      - name: api
        remote: $WORK/api.git
        build: '$BUILD "api change"'
        check: 'test -f out.txt'
YAML
(cd "$ROOT" && go generate ./web)
go build -o "$WORK/boxd" "$ROOT/cmd/boxd"
go build -o "$WORK/boxgate" "$ROOT/cmd/boxgate"
"$WORK/boxd" -config "$WORK/box.yaml" > "$WORK/boxd.log" 2>&1 & echo $! > "$WORK/boxd.pid"
sleep 2
"$WORK/boxgate" -socket "$SOCK" -listen "127.0.0.1:$PORT" -token "$TOKEN" > "$WORK/gate.log" 2>&1 & echo $! > "$WORK/gate.pid"
sleep 1

step "stage the hot-reload target (a second repo)"
cat >> "$WORK/box.yaml" <<YAML
      - name: web
        remote: $WORK/web.git
        build: '$BUILD "web change"'
        check: 'test -f out.txt'
YAML

step "browser"
cd "$(dirname "$0")"
[ -d node_modules/playwright ] || { npm init -y >/dev/null; npm i --no-audit --no-fund playwright >/dev/null; npx playwright install chromium; }
SHOT_DIR=${SHOT_DIR:-$WORK/shots} && mkdir -p "$SHOT_DIR"
BASE="http://127.0.0.1:$PORT" TOKEN="$TOKEN" SHOT_DIR="$SHOT_DIR" node smoke.mjs
echo "screenshots: $SHOT_DIR"
