// Package web embeds the built control room so boxd serves its own UX — one
// binary, one listener, the UI and the API on the same origin.
//
// dist/ is committed: CI stays a Go toolchain, and the daemon never depends
// on node at runtime. Rebuild with `cd web && npm run build` after changing
// src/.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built UI as a filesystem rooted at the bundle.
func Dist() (fs.FS, error) { return fs.Sub(dist, "dist") }
