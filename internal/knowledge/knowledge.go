// Package knowledge is what the factory believes: scoped, attributed records
// with provenance, invalidated by the code moving rather than by anyone
// remembering to mark them.
//
// Records live on the evidence log, so chaining and attribution come from the
// same properties the audit trail stands on — and knowledge poisoning, the
// highest-leverage attack on a dark factory, faces the same controls: only
// authenticated producers write, every record names its producer and run, and
// retrieval returns nothing outside the requesting agent's granted scopes.
// Least privilege at read time, by construction.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Record is one claim the factory holds.
type Record struct {
	ID     string    `json:"id"`
	Tenant tenant.ID `json:"tenant"`
	// Scope is the knowledge scope this record belongs to — "global",
	// "box/<name>", "repo/<key>" — and the unit retrieval is granted by.
	Scope string `json:"scope"`
	// Claim is the knowledge itself.
	Claim string `json:"claim"`
	// Binding: the code this claim is about. A claim about nothing in
	// particular cannot go stale, which is exactly what is wrong with it —
	// bound records are the useful kind, and the binding is what lets the
	// sweep invalidate them mechanically.
	Repo   string `json:"repo,omitempty"`   // remote, usable for git operations
	Path   string `json:"path,omitempty"`   // narrows the binding to a path
	Commit string `json:"commit,omitempty"` // the commit the claim was true at
	// Provenance.
	Producer string `json:"producer"` // authenticated subject that wrote this
	Run      string `json:"run,omitempty"`
	// Supersedes names an earlier record this one replaces. Corrections are
	// new records, never edits — same rule as evidence, same reason.
	Supersedes string `json:"supersedes,omitempty"`
	At         time.Time
	// Stale is set on retrieval when a sweep has flagged the binding as
	// changed. Stale records are returned and marked rather than hidden: an
	// agent deciding whether old knowledge still applies needs to see it,
	// with its staleness, not a silent gap.
	Stale bool `json:"-"`
}

// Base stores and retrieves a tenant's knowledge.
type Base struct {
	Store  store.Store
	Tenant tenant.ID
}

func chainScope(scope string) string { return "knowledge/" + scope }

// Put records a claim. Producer and scope are required; a bound record needs
// its commit, because "this was true of some version" is not knowledge.
func (b *Base) Put(ctx context.Context, r Record) (Record, error) {
	if r.Producer == "" {
		return Record{}, errors.New("knowledge: record names no producer")
	}
	if r.Scope == "" {
		return Record{}, errors.New("knowledge: record names no scope")
	}
	if r.Repo != "" && r.Commit == "" {
		return Record{}, errors.New("knowledge: a bound record needs the commit it was true at")
	}
	r.Tenant = b.Tenant
	body, err := json.Marshal(r)
	if err != nil {
		return Record{}, err
	}
	rec, err := b.Store.Log().Append(ctx, evidence.Record{
		Tenant: b.Tenant, Scope: chainScope(r.Scope),
		Kind: "knowledge", Subject: bindingSubject(r.Repo, r.Path),
		Actor: r.Producer, Body: body,
	})
	if err != nil {
		return Record{}, err
	}
	r.ID = rec.ID
	r.At = rec.At
	return r, nil
}

// Flag marks a record stale, normally from a sweep. It is itself evidence:
// who or what invalidated the claim, and when, survives.
func (b *Base) Flag(ctx context.Context, scope, recordID, why, actor string) error {
	body, _ := json.Marshal(map[string]string{"record": recordID, "why": why})
	_, err := b.Store.Log().Append(ctx, evidence.Record{
		Tenant: b.Tenant, Scope: chainScope(scope),
		Kind: "knowledge-stale", Subject: recordID,
		Actor: actor, Body: body,
	})
	return err
}

// Query is a retrieval request.
type Query struct {
	// Granted is the requesting agent's granted knowledge scopes, from its
	// bundle's context set. Retrieval returns NOTHING outside them, however
	// the other fields are phrased.
	Granted []string
	// Scopes narrows within the grant; empty means every granted scope.
	Scopes []string
	// Contains keeps records whose claim matches every term,
	// case-insensitively. Keyword matching first; an embedding retriever
	// slots in behind the same Query without changing callers.
	Contains []string
}

// Retrieve folds the granted scopes: superseded records drop out, stale flags
// annotate, and scopes outside the grant contribute nothing — asking for them
// is not an error, it is an empty answer, because which scopes exist is
// itself information.
func (b *Base) Retrieve(ctx context.Context, q Query) ([]Record, error) {
	scopes := q.Scopes
	if len(scopes) == 0 {
		scopes = q.Granted
	}
	var out []Record
	for _, scope := range scopes {
		if !slices.Contains(q.Granted, scope) {
			continue
		}
		records, superseded, stale, err := b.fold(ctx, scope)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			if superseded[r.ID] {
				continue
			}
			r.Stale = stale[r.ID]
			if !matches(r, q.Contains) {
				continue
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func (b *Base) fold(ctx context.Context, scope string) ([]Record, map[string]bool, map[string]bool, error) {
	var records []Record
	superseded := map[string]bool{}
	stale := map[string]bool{}
	for rec, err := range b.Store.Log().Since(ctx, b.Tenant, chainScope(scope), 1) {
		if err != nil {
			return nil, nil, nil, err
		}
		switch rec.Kind {
		case "knowledge":
			var r Record
			if err := json.Unmarshal(rec.Body, &r); err != nil {
				return nil, nil, nil, fmt.Errorf("knowledge: %s seq %d: %w", scope, rec.Seq, err)
			}
			r.ID, r.At = rec.ID, rec.At
			if r.Supersedes != "" {
				superseded[r.Supersedes] = true
			}
			records = append(records, r)
		case "knowledge-stale":
			stale[rec.Subject] = true
		}
	}
	return records, superseded, stale, nil
}

func matches(r Record, terms []string) bool {
	claim := strings.ToLower(r.Claim)
	for _, t := range terms {
		if !strings.Contains(claim, strings.ToLower(t)) {
			return false
		}
	}
	return true
}

func bindingSubject(repo, path string) string {
	if repo == "" {
		return "unbound"
	}
	if path == "" {
		return repo
	}
	return repo + "#" + path
}
