// Package web embeds the built control room so boxd serves its own UX — one
// binary, one listener, the UI and the API on the same origin.
//
// dist/ is GENERATED, not committed. `go generate ./web` runs the bundle
// (npm ci, then esbuild) and go:embed reads the result at build time; a tree
// that has not generated fails to compile with "pattern all:dist: no matching
// files found", which is loud, and loud is the point — the alternative was an
// empty room served silently.
//
// The bundle was committed for the first month. That kept CI a Go-only
// toolchain until the day a job was added to rebuild it and check the bytes,
// after which the repo paid for both: node in CI and a 250KB minified diff on
// every UI change, unreviewable and in conflict between any two branches that
// touched web/src. Building the daemon now needs node, once, up front; a
// release builds the bundle before the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:generate npm ci --no-audit --no-fund
//go:generate npm run build

//go:embed all:dist
var dist embed.FS

// Dist returns the built UI as a filesystem rooted at the bundle.
func Dist() (fs.FS, error) { return fs.Sub(dist, "dist") }
