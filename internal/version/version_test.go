package version

import (
	"runtime/debug"
	"testing"
)

// TestExplicitCommitWinsOverBuildInfo: an -X ldflags value is trusted as-is,
// and reads as clean — the toolchain only stamps one at a tagged, committed
// build.
func TestExplicitCommitWinsOverBuildInfo(t *testing.T) {
	old := Commit
	Commit = "deadbeef"
	t.Cleanup(func() { Commit = old })

	b := ThisBuild()
	if !b.Known || b.Commit != "deadbeef" || b.Dirty {
		t.Fatalf("ThisBuild() = %+v, want {Commit: deadbeef, Dirty: false, Known: true}", b)
	}
}

// TestBuildFromReadsVCSSettings: the fallback reads exactly the two keys the
// toolchain embeds, and nothing else confuses it.
func TestBuildFromReadsVCSSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
		want     Build
	}{
		{
			name: "clean",
			settings: []debug.BuildSetting{
				{Key: "vcs", Value: "git"},
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: Build{Commit: "abc123", Dirty: false, Known: true},
		},
		{
			name: "dirty",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: Build{Commit: "abc123", Dirty: true, Known: true},
		},
		{
			name:     "unstamped",
			settings: []debug.BuildSetting{{Key: "-compiler", Value: "gc"}},
			want:     Build{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildFrom(tc.settings); got != tc.want {
				t.Fatalf("buildFrom(%v) = %+v, want %+v", tc.settings, got, tc.want)
			}
		})
	}
}

// TestThisBuildDelegatesToRuntimeBuildInfo: with no ldflags override,
// ThisBuild() reports exactly what this test binary's own build info says —
// `go build` of a package pattern stamps VCS settings by default, `go test`
// does not unless asked, and either way ThisBuild() must agree with
// buildFrom applied to the real settings rather than diverge from it.
func TestThisBuildDelegatesToRuntimeBuildInfo(t *testing.T) {
	old := Commit
	Commit = ""
	t.Cleanup(func() { Commit = old })

	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info available for this test binary")
	}
	want := buildFrom(info.Settings)
	if got := ThisBuild(); got != want {
		t.Fatalf("ThisBuild() = %+v, want %+v (from this binary's own build info)", got, want)
	}
}
