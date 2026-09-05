package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var defines = regexp.MustCompile(`customElements\.define\('(box-[a-z-]+)'`)

// TestTheBundleIsNotStale catches a dist/ generated before the last edit to
// src/. The bundle is no longer committed, so this is not policing git any
// more; it is policing the developer who edited a page and ran `go test`
// without `go generate ./web` in between.
//
// The failure has a shape worth naming. A new page routes fine — @lit-labs
// resolves the path and renders an undefined element, which is an empty box
// rather than an error — so the room looks like it has the view and the view
// is blank. Everything green.
//
// Every source file is scanned, not only pages.js: the shell and the shared
// controls define elements too, and a check that only knew one file would
// have gone quiet the day a second one appeared.
func TestTheBundleIsNotStale(t *testing.T) {
	built, err := fs.ReadFile(dist, "dist/app.js")
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob("src/*.js")
	if err != nil || len(srcs) == 0 {
		t.Fatalf("no sources under src/: %v", err)
	}
	total := 0
	for _, path := range srcs {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range defines.FindAllStringSubmatch(string(src), -1) {
			total++
			if !strings.Contains(string(built), m[1]) {
				t.Errorf("%s defines %s and the committed bundle does not carry it — run `cd web && npm run build`", path, m[1])
			}
		}
	}
	if total == 0 {
		t.Fatal("no custom elements found under src/ — this test has stopped testing anything")
	}
}

// TestTheThemeIsSwitchable pins the mechanism dark mode depends on. jh-core
// ships its light tokens on :root and its dark tokens on .jh-theme-dark; the
// first shell loaded the dark sheet under a media query and never set the
// class, so dark mode was a stylesheet that applied to nothing. The page must
// carry both sheets and put the class on <html> itself.
func TestTheThemeIsSwitchable(t *testing.T) {
	dark, err := fs.ReadFile(dist, "dist/jh-theme-dark.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dark), ".jh-theme-dark {") {
		t.Fatalf("jh-theme-dark.css no longer scopes to .jh-theme-dark — the theme switch in index.html needs revisiting")
	}
	page, err := fs.ReadFile(dist, "dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="/jh-theme-dark.css">`, `classList.toggle('jh-theme-dark'`, `name="color-scheme"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("index.html lacks %q — dark mode would silently apply to nothing", want)
		}
	}
	if strings.Contains(string(page), `media="(prefers-color-scheme: dark)"`) {
		t.Errorf("index.html gates the dark sheet on a media query again; the class is the switch")
	}
}
