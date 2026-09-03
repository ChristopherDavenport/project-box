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
  max_tokens_per_window: 2000000   # the volume brake, in tokens
  max_cost_per_window: 25.00       # the money brake, in dollars — what bills
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

`confine:` puts the agent in a container whose filesystem is the three
directories the run was granted — the workspace, writable, and the
materialized context set and skills, read-only. Nothing else is mounted, so
the mirrors, the store, and every other run's workspace are unreachable by
absence rather than by a rule:

```yaml
    agent: claude-agent-acp
    confine:
      image: box/agent:1          # must carry the agent and git
      env: [ANTHROPIC_API_KEY]    # forwarded BY NAME; never name=value
```

This bounds the filesystem, not the network — an agent that cannot reach a
model API does nothing — and `docker run` killed by its parent leaves the
container behind, so a confined box still wants `max_tokens_per_run`.

`agent:` is an argv, so a wrapper can also be written by hand;
`{{workspace}}`, `{{context}}` and `{{skills}}` become the run's three paths.
A wrapper given only the workspace confines the agent out of its own reading
material — and one that omits `{{skills}}` fails quietly rather than loudly:
the agent starts and simply never has the skills its agentref says it ran.

```yaml
    agent: [sandbox-exec, -D, "WS={{workspace}}", -D, "CTX={{context}}",
            -D, "SK={{skills}}", -f, /etc/box/agent.sb, claude-agent-acp]
```

A run that finishes without reporting what it cost stops new claims until the
window turns over, whether or not a token ceiling is set — measurement is the
precondition for a budget, not a feature of one, and a budget that reads
unknown as zero is not a budget. `allow_unmetered: true` proceeds blind, and
the saying so is on the record.

`build_timeout` bounds one build (default 30m; a negative duration means no
bound). Nothing else does: the lease TTL is renewed while a run holds it, so
without this an agent that hangs runs until the daemon stops.

## What the builders load

A box's `harness:` block is its agentic surface — plugins, skills, MCP — and
all of it is resolved into the bundle before the agent starts, so the
`agentref` answers "what could this agent do" as well as "what was it told".

```yaml
    harness:
      plugins: user            # the machine's enabled set, re-read every run
      plugin_mcp: false        # a plugin's knowledge, never its credentials
      skills:                  # pinned by ref, resolved out of any repo
        - name: go
          repo: https://github.com/you/marketplace.git
          commit: 611d9835ec22d3b5ceba07afafc4b3ddce719e1f
          path: backend/go/skills/go
      mcp:                     # granted deliberately; empty means none
        - name: telemetry
          command: [node, cli.js, mcp]
```

`plugins: user` reads the harness's own enabled set on **every run**, so
enabling a plugin reaches the next build with no reload — and the resolved set
lands in the bundle regardless, because inheriting is not the same as not
knowing. `plugins: none` makes the configuration the only authority.

**Plugin MCP is off by default and `strictMcpConfig` is unconditional.** The
two are not the same lever: the first decides what the plugin loader reads,
the second decides what the session may connect to at all. Without them a
plugin's own `.mcp.json` loads with the plugin, so inheriting a machine's
knowledge means inheriting its credentials — measured on this repository's own
box, that was authenticated Slack, Jira, GitHub, Datadog and Honeycomb servers
in an unattended builder that no box had granted anything.

A skill ref needs a full 40-character commit, refused at load otherwise. The
digest recorded is the **tree** at that path in that commit, not the commit
itself, so revising a neighbouring skill does not churn this one's pin and
revising this one cannot go unnoticed. Skills materialize as a generated
plugin beside the workspace, read-only, never inside the tree — the builder
commits whatever it finds dirty. Adding a skill is an edit and a `kill -HUP`;
moving one forward is a new commit on the same line.

Check the pins in a config without spending a session:

```sh
BOX_HARNESS_LIVE=$PWD/hack/dogfood/box.yaml go test -run TestLiveHarness ./internal/harness/ -v
```

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
./hack/dogfood/run.sh                  # the factory pointed at itself; then:
./hack/dogfood/submit.py               # ...its own open issues, routed in
```

`hack/dogfood` is this repository's own box (issue #7): one repo, one seat,
`max_concurrent: 1`, the gated lane, landing on `refs/heads/factory` and
never on main. It spends real sessions and needs `gh` authenticated — the
forge reaches a private HTTPS remote only through the credential channel
`run.sh` sets up, which the script explains. State lands in `.factory/`,
which is gitignored; the harness is not.

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
