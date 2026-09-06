package daemon

import (
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/agent/acp"
)

// TestCredentialDerivationsAgree: acp.Usage.Credential and
// capacity.Spend.Credential apply the same rule — window wins over price,
// neither is unknown — from two different structs on either side of the
// seam SpendOf crosses. This package is the one place both are in scope, and
// is why the rule is duplicated at all rather than importing internal/capacity
// into internal/agent/acp: a test here is what keeps the duplication from
// drifting instead of a shared type collapsing the boundary.
func TestCredentialDerivationsAgree(t *testing.T) {
	cases := []struct {
		name string
		u    acp.Usage
		want string // string so both capacity.CredentialKind and acp.Credential compare
	}{
		{
			name: "plan",
			u:    acp.Usage{Reported: true, CostUSD: 1.23, RateLimit: &acp.RateLimit{Status: "allowed", Window: "five_hour"}},
			want: "plan",
		},
		{
			name: "key",
			u:    acp.Usage{Reported: true, CostUSD: 4.56},
			want: "key",
		},
		{
			name: "unknown",
			u:    acp.Usage{Reported: true, Tokens: 100},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fromUsage := string(c.u.Credential())
			fromSpend := string(SpendOf(c.u).Credential())
			if fromUsage != c.want {
				t.Fatalf("acp.Usage.Credential() = %q, want %q", fromUsage, c.want)
			}
			if fromSpend != c.want {
				t.Fatalf("capacity.Spend.Credential() = %q, want %q", fromSpend, c.want)
			}
			if fromUsage != fromSpend {
				t.Fatalf("derivations disagree: acp says %q, capacity says %q", fromUsage, fromSpend)
			}
		})
	}
}
