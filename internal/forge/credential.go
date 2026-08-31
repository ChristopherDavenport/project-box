package forge

import (
	"context"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// TokenScope is the authority a minted credential should carry.
//
// A forge app installation holds far more than any single run needs, and its
// credentials can be minted **narrower than the installation** — a subset of
// repositories, a subset of permissions, a short life. That is the point of
// leverage: the token a run receives becomes a projection of the authorization
// decision rather than a second, wider grant sitting beside it.
//
// So this is derived from the agent's context set and its role, never chosen by
// the caller. An agent that may read three repositories gets a credential that
// can reach three repositories.
type TokenScope struct {
	Tenant tenant.ID
	// Repos limits the credential to these repositories. Empty is not
	// "everything" — it is refused. A credential with no stated repositories is
	// a mistake, and the failure mode of guessing is an unscoped token.
	Repos []string
	// Permissions are forge-native permission names, at the least level the
	// role requires.
	Permissions map[string]string
	// TTL bounds the credential's life. Forge tokens are short-lived by design;
	// a run longer than one lifetime renews rather than requesting a longer one.
	TTL time.Duration
}

// Credential is a short-lived, scoped forge token.
type Credential struct {
	// Value is the bearer token. It is never written to evidence, never
	// attached to a span, and never logged — only [Credential.Ref] identifies
	// it in a record.
	Value string
	// Ref names this minting in evidence: which run, for which scope, at which
	// time. Every forge action is attributable through it even though the forge
	// itself sees only the app.
	Ref     string
	Expires time.Time
}

// Minter issues scoped forge credentials.
//
// Implementations hold the app's private key, which is the highest-value secret
// in the system: it mints credentials for every installation. It belongs in a
// managed secret store, never in a repository, and rotation is a supported
// operation rather than an incident.
//
// A tenant's installation must never be reachable through another tenant's
// scope. This is tenant non-interference extended to the forge, and it is
// asserted the same way — by trying it.
type Minter interface {
	// Mint issues a credential for exactly the stated scope. It refuses a
	// scope naming no repositories rather than defaulting to the installation's
	// full reach.
	Mint(ctx context.Context, s TokenScope) (Credential, error)
}
