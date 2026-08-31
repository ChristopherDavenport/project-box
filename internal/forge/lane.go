package forge

import "fmt"

// Lane is a landing discipline, chosen per repository role and branch by Box
// policy. The posture (decided 2026-08-31): we are not afraid of a red
// integration branch — we are afraid of a red production. The environment
// ladder protects production, so the integration branch may trade ceremony
// for throughput.
type Lane string

const (
	// LaneGated is gates-then-land: nothing reaches the branch unjudged.
	// The default, and the only lane environments and infrastructure may use.
	LaneGated Lane = "gated"
	// LaneOptimistic is land-then-check: the change lands immediately after
	// the lease and the expected-old-value check, gates run as post-land
	// checks, and a red check opens repair work rather than rolling back.
	// Fencing is identical to the gated lane — two agents in one working
	// tree is corruption, not a throughput trade.
	LaneOptimistic Lane = "optimistic"
)

// ValidateLane refuses the combinations the architecture forbids: env and
// infra repositories never run optimistic, because their landings ARE
// deployments — a red env repo is a red production, which is the one red we
// are afraid of.
func ValidateLane(lane Lane, role Role) error {
	if lane == LaneOptimistic && (role == RoleEnv || role == RoleInfra) {
		return fmt.Errorf("forge: %s repositories never run the optimistic lane — their landings are deployments", role)
	}
	switch lane {
	case "", LaneGated, LaneOptimistic:
		return nil
	default:
		return fmt.Errorf("forge: unknown lane %q", lane)
	}
}
