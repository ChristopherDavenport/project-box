// Package version is the build's identity — the dockerd lesson that a fleet
// is unoperable when a node cannot say what it is running. Stamped at build
// time:
//
//	go build -ldflags "-X .../internal/version.Version=v0.9.0 -X .../internal/version.Commit=$(git rev-parse --short HEAD)"
//
// Unstamped builds say so honestly rather than guessing.
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version is the release identifier; "dev" when unstamped.
	Version = "dev"
	// Commit is the source commit; empty when unstamped.
	Commit = ""
)

// String renders the identity for --version flags and status payloads.
func String() string {
	if Commit == "" {
		return Version
	}
	return fmt.Sprintf("%s (%s)", Version, Commit)
}

// Build is the commit a running binary was built from, honestly — including
// the case where that is not knowable at all.
//
// Commit and Dirty mean nothing unless Known is true. A build from a modified
// tree does not correspond to any commit, and an unstamped build outside a
// VCS checkout knows nothing about its origin; a caller comparing this
// against a branch head must treat both the same way — refuse rather than
// guess — because unknown never reads as zero.
type Build struct {
	Commit string
	Dirty  bool
	Known  bool
}

// ThisBuild reports the running binary's own build. An explicit -X ldflags
// Commit wins when set. Otherwise it falls back to
// runtime/debug.ReadBuildInfo(), which the Go toolchain embeds automatically
// for any `go build` of a package pattern (not a bare file argument) run
// inside a git checkout: the revision the tree was built from, and whether
// that tree had uncommitted changes.
func ThisBuild() Build {
	if Commit != "" {
		return Build{Commit: Commit, Known: true}
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Build{}
	}
	return buildFrom(info.Settings)
}

func buildFrom(settings []debug.BuildSetting) Build {
	var b Build
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	b.Known = b.Commit != ""
	if !b.Known {
		b.Dirty = false
	}
	return b
}
