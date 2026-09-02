package web

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

var defines = regexp.MustCompile(`customElements\.define\('(box-[a-z-]+)'`)

// TestTheBundleIsNotStale binds the two halves of a decision this repository
// has already paid for: dist/ is committed and go:embed-served so CI stays a
// Go toolchain, which means nothing in CI ever runs esbuild and a source
// change with no rebuild ships as a UI that silently lacks it.
//
// The failure has a shape worth naming. A new page routes fine — @lit-labs
// resolves the path and renders an undefined element, which is an empty box
// rather than an error — so the room looks like it has the view and the view
// is blank. Everything green.
func TestTheBundleIsNotStale(t *testing.T) {
	src, err := os.ReadFile("src/pages.js")
	if err != nil {
		t.Fatal(err)
	}
	built, err := fs.ReadFile(dist, "dist/app.js")
	if err != nil {
		t.Fatal(err)
	}
	found := defines.FindAllStringSubmatch(string(src), -1)
	if len(found) == 0 {
		t.Fatal("no custom elements found in src/pages.js — this test has stopped testing anything")
	}
	for _, m := range found {
		if !strings.Contains(string(built), m[1]) {
			t.Errorf("src defines %s and the committed bundle does not carry it — run `cd web && npm run build`", m[1])
		}
	}
}
