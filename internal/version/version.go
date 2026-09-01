// Package version is the build's identity — the dockerd lesson that a fleet
// is unoperable when a node cannot say what it is running. Stamped at build
// time:
//
//	go build -ldflags "-X .../internal/version.Version=v0.9.0 -X .../internal/version.Commit=$(git rev-parse --short HEAD)"
//
// Unstamped builds say so honestly rather than guessing.
package version

import "fmt"

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
