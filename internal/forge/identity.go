package forge

import (
	"errors"
	"fmt"
	"strings"
)

// Identity is who the box is when it writes a commit itself: the committer
// of a squash or a rebase replay, the author of a compensating revert or a
// rendered deploy, the commit made for an agent that left its tree dirty,
// and the Co-Authored-By trailer builders are asked to sign with. A commit
// carrying it says the box made it.
//
// The zero value names nothing and resolves to [DefaultIdentity]; a
// half-named one is refused by [Identity.Validate] rather than completed,
// because git fills a missing half from the host's user and hostname, and
// that is the operator's machine leaking into published history.
type Identity struct {
	Name  string
	Email string
}

// DefaultIdentity is what an installation that names no identity commits as:
// the project's own no-reply address, so a commit made by any box says so
// wherever it lands.
func DefaultIdentity() Identity {
	return Identity{Name: "project-box", Email: "noreply+pbox@christopherdavenport.tech"}
}

// OrDefault returns id, or [DefaultIdentity] when id names nothing.
func (id Identity) OrDefault() Identity {
	if id == (Identity{}) {
		return DefaultIdentity()
	}
	return id
}

// ErrPartialIdentity is a commit identity naming only one of name and email.
var ErrPartialIdentity = errors.New("forge: commit identity names one of name and email but not both")

// Validate accepts the zero value and a fully named identity git can write
// verbatim. It refuses a half-named one and characters git's ident line
// would strip or that would end the trailer they ride in.
func (id Identity) Validate() error {
	if id == (Identity{}) {
		return nil
	}
	if id.Name == "" || id.Email == "" {
		return ErrPartialIdentity
	}
	for _, f := range [...]struct{ name, v string }{{"name", id.Name}, {"email", id.Email}} {
		if strings.ContainsAny(f.v, "<>\n\r") {
			return fmt.Errorf("forge: commit identity %s %q contains '<', '>' or a line break", f.name, f.v)
		}
		if strings.TrimSpace(f.v) != f.v {
			return fmt.Errorf("forge: commit identity %s %q has surrounding whitespace git would trim", f.name, f.v)
		}
	}
	if !strings.Contains(id.Email, "@") {
		return fmt.Errorf("forge: commit identity email %q is not an address", id.Email)
	}
	return nil
}

// String is the resolved identity as git and a trailer spell it:
// "Name <email>".
func (id Identity) String() string {
	r := id.OrDefault()
	return r.Name + " <" + r.Email + ">"
}

// Env is the resolved identity as git's author and committer environment,
// for appending after os.Environ so it wins over anything inherited.
func (id Identity) Env() []string {
	r := id.OrDefault()
	return []string{
		"GIT_AUTHOR_NAME=" + r.Name, "GIT_AUTHOR_EMAIL=" + r.Email,
		"GIT_COMMITTER_NAME=" + r.Name, "GIT_COMMITTER_EMAIL=" + r.Email,
	}
}
