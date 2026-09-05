package approval

import (
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/authz"
)

func actor(subject string) authz.Actor {
	return authz.Actor{Principal: authz.Principal{Subject: subject}}
}

func TestDistinctForPlan(t *testing.T) {
	requester := actor("requester")

	t.Run("matching planref counts", func(t *testing.T) {
		apps := []Approval{{Subject: "alice", PlanRef: "plan-a"}}
		if n := DistinctForPlan(apps, "plan-a", requester); n != 1 {
			t.Fatalf("got %d, want 1", n)
		}
	})

	t.Run("different planref does not count", func(t *testing.T) {
		apps := []Approval{{Subject: "alice", PlanRef: "plan-b"}}
		if n := DistinctForPlan(apps, "plan-a", requester); n != 0 {
			t.Fatalf("got %d, want 0", n)
		}
	})

	t.Run("self-approval of the right planref does not count", func(t *testing.T) {
		apps := []Approval{{Subject: "requester", PlanRef: "plan-a"}}
		if n := DistinctForPlan(apps, "plan-a", requester); n != 0 {
			t.Fatalf("got %d, want 0", n)
		}
	})

	t.Run("two distinct non-self approvals count as 2", func(t *testing.T) {
		apps := []Approval{
			{Subject: "alice", PlanRef: "plan-a"},
			{Subject: "bob", PlanRef: "plan-a"},
		}
		if n := DistinctForPlan(apps, "plan-a", requester); n != 2 {
			t.Fatalf("got %d, want 2", n)
		}
	})
}
