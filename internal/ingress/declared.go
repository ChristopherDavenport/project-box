package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/kind"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// CanClose reports whether an origination may be closed, and what still blocks
// it.
//
// The rule is the declaration's, which is the whole point of the collapse: an
// incident needing service restored AND a postmortem landed used to be a
// sentence in a doc comment enforced by a Go function that named the kind. Now
// [kind.Completion] says it, and this reads it — so an operator who wants a
// customer escalation to require a written response before it closes declares
// that fact rather than filing a pull request against this file.
//
// graphDone is whether every work item the origination's plan produced has
// settled done. It is a parameter rather than something read here because the
// queue lives on the other side of this package: an origination knows what it
// requires and the queue knows what happened, and joining them here would put
// a queue read on the path of every completion check.
func (i Ingress) CanClose(d kind.Declaration, graphDone bool) (bool, string) {
	if d.Completion.Graph && !graphDone {
		return false, "the work this origination planned has not all settled done"
	}
	var missing []string
	for _, f := range d.Completion.Facts {
		if _, ok := i.Fact(f); !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return false, "not asserted: " + strings.Join(missing, ", ")
	}
	return true, ""
}

// Assert records one completion fact about an origination.
//
// A recorded act, never a field somebody set. "Service was restored at 04:12"
// is a claim by a named party at a named time, and the difference between that
// and a mutated struct is the difference between evidence and a database row —
// the fact an auditor samples is who said it and when, not merely that the
// value is now present.
func Assert(ctx context.Context, log evidence.Log, ing Ingress, actor, name, value string) error {
	if name == "" {
		return fmt.Errorf("ingress: an unnamed fact asserts nothing about %s", ing.ID)
	}
	if value == "" {
		return fmt.Errorf("ingress: fact %q of %s has no value — an empty assertion reads as absence to every completion check", name, ing.ID)
	}
	body, err := json.Marshal(map[string]string{"name": name, "value": value})
	if err != nil {
		return err
	}
	if _, err := log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: Scope(ing.ID),
		Kind: RecordFact, Subject: ing.ID,
		Actor: actor, OnBehalfOf: ing.Requester, Body: body,
	}); err != nil {
		return fmt.Errorf("ingress: asserting %s of %s: %w", name, ing.ID, err)
	}
	return nil
}

// Load folds an origination out of its chain.
//
// A FOLD and not a head read. The chain carries the origination, the facts
// asserted about it, its plans, and every authority it exercised, so the
// newest record is usually not the origination — and the three readers that
// unmarshalled it anyway failed in two different ways.
//
// A break-glass body carries `incident` as a string where the struct had a
// pointer, so it failed to unmarshal at all: the router refused every
// redelivery of that key afterwards, and the pump's intake aborted its whole
// pass, for that tenant, permanently. The room read into a narrow anonymous
// struct instead, which succeeded, and its queue rows silently lost their
// route and their key.
//
// The type error was ACCIDENT rather than protection — it depended on one key
// in one body colliding with a field of a different type. A plan record
// collides with nothing and unmarshals cleanly into a zeroed Ingress that
// every caller would treat as real, which is what this fold exists to stop.
// Same mistake the work item's chain made and the same fix: select on the
// record kind, and let a writer of anything else be irrelevant to the fold.
func Load(ctx context.Context, log evidence.Log, t tenant.ID, id string) (Ingress, bool, error) {
	var ing Ingress
	found := false
	for rec, err := range log.Since(ctx, t, Scope(id), 1) {
		if err != nil {
			return Ingress{}, false, err
		}
		switch rec.Kind {
		case RecordOpen:
			if err := json.Unmarshal(rec.Body, &ing); err != nil {
				return Ingress{}, false, fmt.Errorf("ingress: %s: %w", id, err)
			}
			if ing.Opened.IsZero() {
				ing.Opened = rec.At
			}
			found = true
		case RecordFact:
			var f struct{ Name, Value string }
			if err := json.Unmarshal(rec.Body, &f); err != nil {
				return Ingress{}, false, fmt.Errorf("ingress: %s fact: %w", id, err)
			}
			if ing.Facts == nil {
				ing.Facts = map[string]string{}
			}
			ing.Facts[f.Name] = f.Value
		}
	}
	return ing, found, nil
}

// Preempt takes a lease over a live claim on an origination's authority, and
// records that it did. The store's Preempt is deliberately mechanical; this is
// where the authority question is answered — and the answer now comes from the
// kind's declaration rather than from a string compare, so "what may preempt"
// is a reviewable change instead of a compile-time fact.
func Preempt(ctx context.Context, ing Ingress, d kind.Declaration, leaser forge.Leaser, log evidence.Log, actor string, req forge.LeaseRequest) (forge.Lease, error) {
	if !ing.MayPreempt(d) {
		if !d.Capabilities.Preempt {
			return forge.Lease{}, fmt.Errorf("%w: %s is a %s, and that declaration does not carry preempt (it carries %s)",
				kind.ErrCapability, ing.ID, ing.Kind, d.Capabilities)
		}
		return forge.Lease{}, fmt.Errorf("%w: %s is closed — a closed origination authorizes nothing", kind.ErrCapability, ing.ID)
	}
	lease, err := leaser.Preempt(ctx, req)
	if err != nil {
		return forge.Lease{}, err
	}
	body, _ := json.Marshal(map[string]any{"repo": req.Repo, "ref": req.Ref, "token": lease.Token, "ingress": ing.ID, "kind": ing.Kind})
	if _, err := log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: Scope(ing.ID),
		Kind: "preempted", Subject: req.Repo + "@" + req.Ref,
		Actor: actor, Body: body,
	}); err != nil {
		return lease, fmt.Errorf("ingress: preemption held but its record failed: %w", err)
	}
	return lease, nil
}

// BreakGlass authorizes one emergency bypass under an open origination whose
// declaration grants it, and is the ONLY path that does. An emergency bypass
// with nothing behind it is precisely the finding an audit looks for, so the
// refusal is the control: every invocation inherits a why, a timeline, and a
// completion obligation.
func BreakGlass(ctx context.Context, ing Ingress, d kind.Declaration, log evidence.Log, actor, what string) error {
	if !ing.MayBreakGlass(d) {
		return fmt.Errorf("%w: break-glass refused — %s is a %s, %s",
			kind.ErrCapability, ing.ID, ing.Kind, refusalReason(ing, d))
	}
	body, _ := json.Marshal(map[string]string{"what": what, "ingress": ing.ID, "kind": string(ing.Kind)})
	_, err := log.Append(ctx, evidence.Record{
		Tenant: ing.Tenant, Scope: Scope(ing.ID),
		Kind: "break-glass", Subject: ing.ID,
		Actor: actor, Body: body,
	})
	if err != nil {
		return fmt.Errorf("ingress: break-glass NOT authorized — the invocation could not be recorded: %w", err)
	}
	return nil
}

// refusalReason says which of the two conditions failed, because "refused" on
// its own sends an operator to read the wrong file: a closed incident and a
// kind that was never granted the authority are different problems with
// different fixes.
func refusalReason(ing Ingress, d kind.Declaration) string {
	if !d.Capabilities.BreakGlass {
		return "and that declaration does not carry break_glass (it carries " + d.Capabilities.String() + ")"
	}
	return "and it is closed"
}
