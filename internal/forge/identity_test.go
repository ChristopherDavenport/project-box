package forge

import (
	"errors"
	"slices"
	"testing"
)

// TestAnUnnamedIdentityIsTheBoxs: an installation that says nothing commits
// as the project's own no-reply address, so a commit says a box made it.
func TestAnUnnamedIdentityIsTheBoxs(t *testing.T) {
	if got, want := (Identity{}).String(), "project-box <noreply+pbox@christopherdavenport.tech>"; got != want {
		t.Fatalf("zero identity = %q, want %q", got, want)
	}
	named := Identity{Name: "Acme Factory", Email: "factory@acme.example"}
	if got := named.OrDefault(); got != named {
		t.Fatalf("a named identity resolved to %v, want itself", got)
	}
	want := []string{
		"GIT_AUTHOR_NAME=Acme Factory", "GIT_AUTHOR_EMAIL=factory@acme.example",
		"GIT_COMMITTER_NAME=Acme Factory", "GIT_COMMITTER_EMAIL=factory@acme.example",
	}
	if got := named.Env(); !slices.Equal(got, want) {
		t.Fatalf("env = %q, want %q", got, want)
	}
}

// TestAHalfNamedIdentityIsRefusedNotCompleted: git fills a missing half from
// the host's user and hostname, which is the operator's machine leaking into
// published history.
func TestAHalfNamedIdentityIsRefusedNotCompleted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		id      Identity
		partial bool
		ok      bool
	}{
		{"zero", Identity{}, false, true},
		{"full", Identity{Name: "Acme", Email: "a@acme.example"}, false, true},
		{"name only", Identity{Name: "Acme"}, true, false},
		{"email only", Identity{Email: "a@acme.example"}, true, false},
		{"angle bracket ends the ident", Identity{Name: "Acme <x>", Email: "a@acme.example"}, false, false},
		{"newline forges a trailer", Identity{Name: "Acme\nSigned-off-by: x", Email: "a@acme.example"}, false, false},
		{"git would trim", Identity{Name: " Acme", Email: "a@acme.example"}, false, false},
		{"not an address", Identity{Name: "Acme", Email: "acme"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.id.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("Validate(%+v) = %v, want ok=%v", tc.id, err, tc.ok)
			}
			if got := errors.Is(err, ErrPartialIdentity); got != tc.partial {
				t.Fatalf("errors.Is(%v, ErrPartialIdentity) = %v, want %v", err, got, tc.partial)
			}
		})
	}
}
