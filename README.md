# project-box

A control plane for **dark software factories**: systems built, verified,
and deployed end to end by agents, with no human in the execution loop —
producing evidence an external auditor can test.

- **What and why:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — every
  decision with its reasoning, including the rejected paths.
- **State of the work:** [docs/PLAN.md](docs/PLAN.md) — phases, exit
  criteria, and §10 as the running status log. The commit history is the
  build journal; read it phase by phase.

## Build and test

Go 1.25.13+ (`go.mod` pins it; with `GOTOOLCHAIN=local`, upgrade or run
with `GOTOOLCHAIN=auto`).

```sh
go build ./...
go test -race ./...                    # SQLite conformance always runs
```

Two test legs gate on their environment locally and are REQUIRED in CI —
"skipped, all green" is never a pass here:

```sh
gcloud emulators spanner start &       # then:
SPANNER_EMULATOR_HOST=localhost:9010 go test -race ./internal/store/spanner/
go test -race ./internal/telemetry/    # needs docker (collector control test)
```

Version-stamped builds:

```sh
go build -ldflags "-X github.com/ChristopherDavenport/project-box/internal/version.Version=v0.1.0 \
  -X github.com/ChristopherDavenport/project-box/internal/version.Commit=$(git rev-parse --short HEAD)" ./cmd/...
```

## Run the factory

The daemon is **socket-only by design** (the dockerd posture): its whole
trust model is the unix socket's file mode. Network exposure, tokens, and
OIDC are layers in front — never the daemon's concern.

```sh
boxd -config box.yaml                  # see internal/boxcfg for the schema
box room                               # control room on a loopback port
box context create prod -daemon tcp:host:8437 -token ...   # named endpoints
boxgate -socket boxd.sock -listen 0.0.0.0:8437 -token ...  # the network seam
```

Minimal `box.yaml`:

```yaml
tenant: acme
db: box.db
root: forge
listen: unix:boxd.sock
every: 15s
budget:
  max_concurrent: 3
  max_tokens_per_window: 2000000   # the brake, in the currency of the invoice
  window: 1h
boxes:
  - name: shop
    agent: /path/to/claude-agent-acp   # or per-repo `build:` commands
    seat: mason
    repos:
      - name: api
        remote: git@github.com:you/api.git
        check: go test ./...
```

`agent:` also takes an argv, which is how a confining wrapper goes in front
of the agent — `{{workspace}}` becomes the run's workspace path:

```yaml
    agent: [docker, run, --rm, -i,
            -v, "{{workspace}}:{{workspace}}", -w, "{{workspace}}",
            box/agent:1, claude-agent-acp]
```

Setting `max_tokens_per_window` also arms the unmetered rule: a run that
finishes without reporting what it cost stops new claims until the window
turns over. A budget that reads unknown as zero is not a budget.

Open work and step back — the daemon's ticks do the rest:

```sh
box -tenant acme -daemon unix:boxd.sock submit -route shop-api -key issue-1 -task "fix the flaky test"
box -tenant acme -daemon unix:boxd.sock provenance <sha>    # the audit query
```

Reload config on the running daemon: edit `box.yaml`, then `kill -HUP` or
`POST /v1/reload`. Executors, routes, budget, and interval swap hot;
tenant/db/root/listen are the daemon's identity and refuse to change.

## End-to-end proofs (rerunnable)

```sh
./hack/reconcile-e2e.sh                # kind + Gitea + Argo CD: land → reconcile → running workload
./hack/room-smoke/run.sh               # browser drives the control room incl. hot reload (needs node)
go run ./hack/deployloop -remote ...   # the deploy loop against any env repo
go run ./hack/continuity -agent ...    # the seat-continuity experiment (spends real sessions)
BOX_ACP_LIVE=/path/to/claude-agent-acp go test -run TestLive ./internal/agent/acp/  # one real agent run
```

## Web control room

`web/` (Lit + @lit-labs/router + Jack Henry Design System). `dist/` is
committed and embedded into boxd — CI stays a Go toolchain. Rebuild after
changing `web/src/`:

```sh
cd web && npm install && npm run build
```

## Resuming work

`docs/PLAN.md` §10 says exactly where things stand and what is next; the
phase commits carry the rationale. The live-verification scripts above
recreate every environment the tests assume from nothing.
