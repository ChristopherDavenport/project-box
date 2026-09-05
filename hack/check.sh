#!/bin/sh
# The check: what CI runs and what the factory's own gate runs on every build,
# from one file so the two cannot drift. The bundle is generated first because
# go:embed reads it — a tree without it does not compile, by design.
set -eu
cd "$(dirname "$0")/.."
go generate ./web
go build ./...
go test ./...
