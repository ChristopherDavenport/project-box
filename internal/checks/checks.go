// Package checks records whether a landed commit is green — the fact the
// environment ladder promotes on.
//
// In the gated lane a landing is green by construction and the record says
// so; in the optimistic lane the post-land checks decide after the fact. The
// promotion predicate reads only this chain: "green" means a check record
// says green, never "nobody has looked".
package checks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Status is one commit's check outcome.
type Status struct {
	Green bool   `json:"green"`
	Lane  string `json:"lane"`
	Note  string `json:"note,omitempty"`
	// Flaky marks a green whose preceding observation of the same commit was
	// red: the re-run passed, so the commit is green, but the check itself
	// misfired — a fact on the chain in its own right, not something an
	// operator has to notice twice.
	Flaky bool `json:"flaky,omitempty"`
}

func scope(repo string) string { return "checks/" + repo }

// Write records a commit's status on the repo's check chain.
func Write(ctx context.Context, s store.Store, t tenant.ID, repo, sha string, st Status, actor string) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.Log().Append(ctx, evidence.Record{
		Tenant: t, Scope: scope(repo), Kind: "check", Subject: sha,
		Actor: actor, Body: body,
	})
	return err
}

// Rerun is Confirm's second look at a red judgement: re-evaluate the checks
// and report whether they now pass and, if not, why. An error means the look
// could not conclude — infrastructure trouble, never a judgement.
type Rerun func(ctx context.Context) (green bool, reason string, err error)

// Confirm settles whether an observed red is real before anyone is summoned
// over it. Both observations go on the chain: red twice is a confirmed red;
// red then green is a flaky check, recorded as such. When the second look
// cannot conclude, the first red stands and its error returns — an
// unconfirmed red is a third state, and it is not a pass.
func Confirm(ctx context.Context, s store.Store, t tenant.ID, repo, sha, lane, reason, actor string, rerun Rerun) (bool, error) {
	if err := Write(ctx, s, t, repo, sha,
		Status{Green: false, Lane: lane, Note: "observed red: " + reason}, actor); err != nil {
		return false, err
	}
	green, second, err := rerun(ctx)
	if err != nil {
		return false, fmt.Errorf("checks: confirming red %s@%s: %w", repo, sha, err)
	}
	if green {
		return false, Write(ctx, s, t, repo, sha,
			Status{Green: true, Lane: lane, Flaky: true, Note: "flaky: red did not reproduce (" + reason + ")"}, actor)
	}
	return true, Write(ctx, s, t, repo, sha,
		Status{Green: false, Lane: lane, Note: "confirmed red: " + second}, actor)
}

// LatestGreen returns the most recently landed commit whose latest check is
// green. Chain order is landing order, so a red landing simply never becomes
// the candidate — nothing rolls back, it just never advances.
func LatestGreen(ctx context.Context, s store.Store, t tenant.ID, repo string) (string, bool, error) {
	latest := map[string]bool{}
	var order []string
	for rec, err := range s.Log().Since(ctx, t, scope(repo), 1) {
		if err != nil {
			return "", false, err
		}
		var st Status
		if json.Unmarshal(rec.Body, &st) != nil {
			continue
		}
		if _, seen := latest[rec.Subject]; !seen {
			order = append(order, rec.Subject)
		}
		latest[rec.Subject] = st.Green
	}
	for i := len(order) - 1; i >= 0; i-- {
		if latest[order[i]] {
			return order[i], true, nil
		}
	}
	return "", false, nil
}

// IsGreen reports a specific commit's latest status. An unchecked commit is
// not green: absence of judgement is not a pass.
func IsGreen(ctx context.Context, s store.Store, t tenant.ID, repo, sha string) (bool, error) {
	green, found := false, false
	for rec, err := range s.Log().Since(ctx, t, scope(repo), 1) {
		if err != nil {
			return false, err
		}
		if rec.Subject != sha {
			continue
		}
		var st Status
		if err := json.Unmarshal(rec.Body, &st); err != nil {
			return false, fmt.Errorf("checks: %s@%s: %w", repo, sha, err)
		}
		green, found = st.Green, true
	}
	return green && found, nil
}
