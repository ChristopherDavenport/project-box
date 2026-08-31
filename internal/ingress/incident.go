package ingress

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
)

// CanClose reports whether an ingress may be closed, and what still blocks
// it. An incident needs service restored AND a postmortem landed: an
// incident with a Restored time and no postmortem is still open, which is
// the point — restoration ends the outage, the postmortem ends the incident.
func (i Ingress) CanClose() (bool, string) {
	if i.Kind != KindIncident {
		return true, ""
	}
	if i.Incident == nil {
		return false, "incident ingress carries no incident record"
	}
	if i.Incident.Restored.IsZero() {
		return false, "service not restored"
	}
	if i.Incident.Postmortem == "" {
		return false, "postmortem not landed"
	}
	return true, ""
}

// Preempt takes a lease over a live claim on an incident's authority, and
// records that it did. The store's Preempt is deliberately mechanical; this
// is where the authority question is answered: only an OPEN INCIDENT
// preempts, and a campaign — however urgent it feels — never does.
func Preempt(ctx context.Context, ing Ingress, leaser forge.Leaser, log evidence.Log, actor string, req forge.LeaseRequest) (forge.Lease, error) {
	if ing.Kind != KindIncident {
		return forge.Lease{}, fmt.Errorf("ingress: %s is a %s — only an incident preempts", ing.ID, ing.Kind)
	}
	if !ing.Closed.IsZero() {
		return forge.Lease{}, fmt.Errorf("ingress: incident %s is closed — a closed incident authorizes nothing", ing.ID)
	}
	lease, err := leaser.Preempt(ctx, req)
	if err != nil {
		return forge.Lease{}, err
	}
	body, _ := json.Marshal(map[string]any{"repo": req.Repo, "ref": req.Ref, "token": lease.Token, "incident": ing.ID})
	if _, err := log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: "ingress/" + ing.ID,
		Kind: "preempted", Subject: req.Repo + "@" + req.Ref,
		Actor: actor, Body: body,
	}); err != nil {
		return lease, fmt.Errorf("ingress: preemption held but its record failed: %w", err)
	}
	return lease, nil
}

// BreakGlass authorizes one emergency bypass under an open incident, and is
// the ONLY path that does. An emergency bypass with no incident behind it is
// precisely the finding an audit looks for, so the refusal is the control:
// every invocation inherits a why, a timeline, and a postmortem obligation,
// and "show me every break-glass invocation" resolves to a list of
// incidents.
func BreakGlass(ctx context.Context, ing Ingress, log evidence.Log, actor, what string) error {
	if !ing.MayBreakGlass() {
		return fmt.Errorf("ingress: break-glass refused — %s is not an open incident", ing.ID)
	}
	body, _ := json.Marshal(map[string]string{"what": what, "incident": ing.ID})
	_, err := log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: "ingress/" + ing.ID,
		Kind: "break-glass", Subject: ing.ID,
		Actor: actor, Body: body,
	})
	if err != nil {
		return fmt.Errorf("ingress: break-glass NOT authorized — the invocation could not be recorded: %w", err)
	}
	return nil
}
