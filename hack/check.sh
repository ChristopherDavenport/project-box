#!/bin/sh
# The check: what CI runs and what the factory's own gate runs on every
# build, from one file so the two cannot drift (issue #68 — d9b4607 landed
# through the gate and then failed CI at gofmt, because the gate ran fewer
# legs than CI did). Legs run in CI's own order: generate, gofmt, vet,
# build, test -race, govulncheck. go generate runs first because go:embed
# reads the bundle — a tree without it does not compile, by design.
#
# The gate's verdict (internal/gate/command.go: isFailureLine) keeps only
# lines near the head of the transcript shaped like a Go test failure, a
# panic, or a compiler error — a leg whose native failure output matches
# none of that (gofmt's bare file list, govulncheck's report) would fail
# the script but leave the gate's refusal illegible. So every leg that
# fails prints a line starting with "FAIL <name>" before this script exits
# non-zero; that shape is what isFailureLine keeps.
set -eu
cd "$(dirname "$0")/.."

go generate ./web || { echo "FAIL generate"; exit 1; }

# git ls-files, not `gofmt -l .`: a developer checkout carries gitignored
# trees (.factory/ among them) that a filesystem walk descends into but a
# git listing does not. This is the one place this script's spelling departs
# from ci.yml's old spelling — and since ci.yml now calls this script, this
# spelling IS CI's spelling, so the two cannot drift on it either.
unformatted=$(gofmt -l $(git ls-files '*.go'))
if [ -n "$unformatted" ]; then
	echo "FAIL gofmt"
	echo "$unformatted"
	exit 1
fi

go vet ./... || { echo "FAIL vet"; exit 1; }
go build ./... || { echo "FAIL build"; exit 1; }
go test -race ./... || { echo "FAIL test"; exit 1; }

# The Spanner conformance leg above (internal/store/spanner) fails outright
# when CI is set and the emulator is absent, so CI proves nothing silently —
# but a plain `go test` prints nothing about a local skip, and a skipped leg
# is on the record, never silent.
if [ -z "${SPANNER_EMULATOR_HOST:-}" ]; then
	echo "SKIP spanner: SPANNER_EMULATOR_HOST is unset, so the conformance leg above skipped rather than ran; start one with: gcloud emulators spanner start"
fi

go run golang.org/x/vuln/cmd/govulncheck@latest ./... || { echo "FAIL govulncheck"; exit 1; }
