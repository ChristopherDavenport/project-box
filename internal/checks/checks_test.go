package checks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/store/sqlite"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

const tn = tenant.ID("acme")

func open(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func statuses(t *testing.T, s *sqlite.Store, repo, sha string) []Status {
	t.Helper()
	var out []Status
	for rec, err := range s.Log().Since(context.Background(), tn, scope(repo), 1) {
		if err != nil {
			t.Fatal(err)
		}
		if rec.Subject != sha {
			continue
		}
		var st Status
		if err := json.Unmarshal(rec.Body, &st); err != nil {
			t.Fatal(err)
		}
		out = append(out, st)
	}
	return out
}

// TestConfirmFlaky: red then green is a flaky check — recorded as such, the
// commit green on the chain, nobody summoned.
func TestConfirmFlaky(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	confirmed, err := Confirm(ctx, s, tn, "app", "sha-1", "optimistic", "tests: fail (timeout)", "runner-1",
		func(context.Context) (bool, string, error) { return true, "", nil })
	if err != nil || confirmed {
		t.Fatalf("a red that does not reproduce must not confirm: %v %v", confirmed, err)
	}
	green, err := IsGreen(ctx, s, tn, "app", "sha-1")
	if err != nil || !green {
		t.Fatalf("flaky-then-green commit should be green: %v %v", green, err)
	}
	sts := statuses(t, s, "app", "sha-1")
	if len(sts) != 2 || sts[0].Green || !sts[1].Green || !sts[1].Flaky {
		t.Fatalf("want observed red then flaky green on the chain: %+v", sts)
	}
}

// TestConfirmRed: red twice is a real red, and both looks are on the chain.
func TestConfirmRed(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	confirmed, err := Confirm(ctx, s, tn, "app", "sha-2", "optimistic", "tests: fail (assert)", "runner-1",
		func(context.Context) (bool, string, error) { return false, "tests: fail (assert again)", nil })
	if err != nil || !confirmed {
		t.Fatalf("a reproducing red must confirm: %v %v", confirmed, err)
	}
	if green, _ := IsGreen(ctx, s, tn, "app", "sha-2"); green {
		t.Fatal("a confirmed red reads green")
	}
	sts := statuses(t, s, "app", "sha-2")
	if len(sts) != 2 || sts[0].Green || sts[1].Green || sts[1].Flaky {
		t.Fatalf("want two red observations on the chain: %+v", sts)
	}
	if !strings.Contains(sts[1].Note, "confirmed red") {
		t.Fatalf("second observation should say it confirms: %+v", sts[1])
	}
}

// TestConfirmInconclusive: the second look could not conclude — the first red
// stands, and the error surfaces instead of a judgement. An unconfirmed red
// is a third state, and it is not a pass.
func TestConfirmInconclusive(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	confirmed, err := Confirm(ctx, s, tn, "app", "sha-3", "optimistic", "tests: fail (timeout)", "runner-1",
		func(context.Context) (bool, string, error) { return false, "", errors.New("runner lost the workspace") })
	if err == nil || confirmed {
		t.Fatalf("an inconclusive rerun must error, not judge: %v %v", confirmed, err)
	}
	if green, _ := IsGreen(ctx, s, tn, "app", "sha-3"); green {
		t.Fatal("an unconfirmed red reads green")
	}
	if sts := statuses(t, s, "app", "sha-3"); len(sts) != 1 || sts[0].Green {
		t.Fatalf("the observed red should stand alone on the chain: %+v", sts)
	}
}
