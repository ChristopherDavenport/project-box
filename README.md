# project-box

A control plane for **dark software factories**: systems built, verified,
and deployed end to end by agents, with no human in the execution loop —
producing evidence an external auditor can test.

- **What and why:** [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — every
  decision with its reasoning, including the rejected paths.
- **State of the work:** [docs/PLAN.md](docs/PLAN.md) — phases, exit
  criteria, and §10 as the running status log. The commit history is the
  build journal; read it phase by phase.

**Status: experimental.** One operator builds this, and it builds itself
(`hack/dogfood`). The configuration schema, the API, and the store layout
change without notice or migration, and nothing here is supported for
production use. Issues are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md)
for how an issue becomes work and [SECURITY.md](SECURITY.md) for how to
report a vulnerability.

## Build and test

Go 1.26.11+ (`go.mod` pins it; with `GOTOOLCHAIN=local`, upgrade or run
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
  credential: key                  # this box runs on an API key, not a subscription
  window: 1h
retention:                         # absent, nothing is ever compacted
  keep: 5000                       # newest records kept hot, per standing scope
  keep_for: 72h                    # and nothing younger is pruned; both bind
boxes:
  - name: shop
    agent: /path/to/claude-agent-acp   # or per-repo `build:` commands
    seat: mason
    model: sonnet                      # by the value the agent lists; empty = its default
    roles:
      planner: {model: opus[1m]}       # a role's tier wins over the box's for that role
    kinds:
      - name: issue
        decompose: {decomposer: planner}   # judge every issue into units first
        completion: {graph: true}
    repos:
      - name: api
        remote: git@github.com:you/api.git
        check: go test ./...
        sections:                      # where work is expressed; see below
          - name: handlers
            purpose: the HTTP surface
            paths: [internal/http]
            check: go test ./internal/http/...
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

`window_reserve` is the brake in the currency that actually binds on a
subscription: every run reports the account's own window — status, which
window, utilization, reset time — and above this share the meter refuses new
starts until it resets, so the factory yields to the human on the same
account instead of finishing their window. It reads the last report, so it
binds only as tightly as runs report. `hack/dogfood/window.py` asks the
harness the same question for a few hundred tokens, before a round starts.

`credential` names which account this box is on: `plan` for a subscription,
whose brake is `window_reserve`, or `key` for a metered API key, whose brake
is `max_cost_per_window`. It is required the moment either brake is set —
absent, the box has not said which account it is braking, so neither brake
means what it claims, and the config refuses to load rather than guess. A
run that reports the OTHER kind than the one named — a key turning up in the
daemon's environment on a box that named `plan`, or a subscription run on a
box that named `key` — is charged (spend is spend) and then stops the box:
the record it writes carries the kind named and the kind reported, never a
credential value.

`build_timeout` bounds one build (default 30m; a negative duration means no
bound). Nothing else does: the lease TTL is renewed while a run holds it, so
without this an agent that hangs runs until the daemon stops.

`commit_identity: {name, email}` is who the box is when it writes a commit
itself: the committer of a squash or replay, the author of a revert or a
rendered deploy, and the `Co-Authored-By` trailer builders sign with.
Absent, it is `project-box <noreply+pbox@christopherdavenport.tech>`, so a
commit says a box made it. Name both fields or neither; changing it needs
a restart.

## Where work is expressed

A repository's `sections:` are its named regions — paths, a purpose the
planner reads, the check a builder runs while iterating there, and optionally
a tier. They are a property of the software, not a person or a role. A unit
of work names the sections its paths fall in: two units in disjoint sections
run at once, two in the same section take turns, and a unit that names none
is the whole repository and waits for every section. The loop holds a
section as a lease before it builds, so fan-out is safe by construction
rather than by hoping two agents' edits rebase cleanly; a conflicting replay
is rebuilt by an agent, and the lease spends a tick to save a session. A
repository that declares no sections has one region, itself, exactly as
before.

`roles:` sets a tier per role — what the planner judges on, what the builder
expresses on — winning over the box's `model:` for that role's runs. A
section's tier wins over the role's, and a plan node's over the section's:
the cheapest model the unit's difficulty allows, chosen as close to the unit
as anyone was willing to say.

A kind declared with `decompose: {decomposer: planner}` is judged into units
before it is work: an agent under the planner role reads a read-only tree at
the branch head, is shown the sections, and answers with a graph — each unit's
task, sections, dependencies and tier. The plan is recorded on the
origination's chain with the model that judged, the units become items that
carry where and on what, and the planner's handoff notes wake its next plan
from its own seat. It is not a coordinator: one run, one agentref, one record,
and it is opt-in per kind because a judgement on the default intake path would
put an agent on the critical path of admitting work. A planner that fails
parks the origination itself in the exception queue — there is no item yet —
and `retry` and `resolve` reach it from the room like any item.

## What the builders load

A box's `harness:` block is its agentic surface — plugins, skills, MCP — and
all of it is resolved into the bundle before the agent starts, so the
`agentref` answers "what could this agent do" as well as "what was it told".

`model:` and `session:` are the session the builders run, in the protocol's
own terms: the model by the value the agent lists for its model option, and
every other option — effort, a fast mode, a sub-agent — by the id the agent
gives it. Both are applied through `session/set_config_option` once the
session opens and asserted back from the agent's answer; a value the agent
does not list, or answers with something else, fails the run before a prompt
is sent. Left empty, the agent's own default runs — for claude-agent-acp,
whatever the machine's settings say, which is a choice nobody made. Either
way the `agent-session` record on the item's chain says what the agent
answered: the model in force, the mode, every option with the values it
offered, and every command (skills among them) it made available. The
`agent-run` record says what was asked, as a digest; this says what was
delivered, in the clear.

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
./hack/dogfood/submit.py               # ...its own open issues labelled `box`, routed in
```

`hack/dogfood` is this repository's own box (issue #7), and it is the
running instance that builds this repository, not a template to run as-is:
the gated lane, landing on main through pull requests the factory opens,
spending real sessions on the operator's own subscription. It needs `gh`
authenticated — the forge reaches the HTTPS remote only through the
credential channel `run.sh` sets up, which the script explains. It takes
only open issues labelled `box`: anyone can open an issue on a public
repository, only a collaborator can label one, and every issue taken
becomes a builder's prompt with a push token behind it. State lands in
`../project-box-state/`, beside the checkout rather than inside it — a
workspace under the checkout would advertise the checkout (issue #73).

`hack/dogfood/install.sh` is how the box survives the host (issue #113):
run once, it generates and enables `systemd --user` units — WSL2 with
systemd enabled, or any Linux user session bus — for the daemon and the
control room, so a machine reboot or a crash-restart comes back on its own
rather than needing someone to re-run `run.sh` by hand. `restart.sh` is the
same lifecycle through those units, keeping the chain's stop/resume record
intact around a `systemctl --user restart`.

## Web control room

`web/` (Lit + @lit-labs/router + Jack Henry Design System). The bundle is
generated, not committed, and go:embed reads it at build time — so building
the daemon needs node once, up front, and a tree that has not generated does
not compile:

```sh
go generate ./web          # npm ci + esbuild → web/dist (gitignored)
go build ./cmd/boxd
```

`hack/check.sh` is the check CI and the factory's own gate both run: generate,
build, test.

The room is themed with jh-core's tokens: the light set lives on `:root`, the
dark set on a `.jh-theme-dark` class that `index.html` puts on `<html>` before
first paint. The header's System / Light / Dark control chooses, the choice is
remembered per browser, and System follows `prefers-color-scheme` live.

Every verb the API offers is on the page it belongs to — stop and resume
claims on the strip under the header, retry and resolve on each exception and
on the item, reload configuration on the dashboard — and each asks for the
reason the record needs before it acts.

## Resuming work

`docs/PLAN.md` §10 says exactly where things stand and what is next; the
phase commits carry the rationale. The live-verification scripts above
recreate every environment the tests assume from nothing.

## License

MIT; see [LICENSE](LICENSE). The control room inlines and bundles
Apache-2.0 and BSD-3-Clause web components, whose notices are in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
