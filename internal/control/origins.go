package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// FactRef pins one line of a story to the chain record it was read from — an
// auditor's "show your work". Tenant, Scope and Seq are enough to walk back
// to the exact record with Log.Since; Kind and Actor are carried alongside so
// a reader can check the story's claim against the record without a second
// round trip.
type FactRef struct {
	Tenant tenant.ID `json:"tenant"`
	Scope  string    `json:"scope"`
	Seq    uint64    `json:"seq"`
	Kind   string    `json:"kind"`
	Actor  string    `json:"actor"`
	At     time.Time `json:"at"`
}

func factRef(rec evidence.Record) FactRef {
	return FactRef{Tenant: rec.Tenant, Scope: rec.Scope, Seq: rec.Seq, Kind: rec.Kind, Actor: rec.Actor, At: rec.At}
}

// IngressFact is the RecordOpen fact: who delivered the origination, when,
// and what it asked for.
type IngressFact struct {
	FactRef
	Requester string `json:"requester,omitempty"`
	Title     string `json:"title,omitempty"`
	Note      string `json:"note,omitempty"`
}

// PlanFact is one 'planned' record — the decomposition in force, whole.
type PlanFact struct {
	FactRef
	Plan plan.Plan `json:"plan"`
}

// PlanEventFact is a plan-failed, plan-deferred, plan-retried or
// plan-resolved record: a fact about the judgement itself, not the graph it
// produced.
type PlanEventFact struct {
	FactRef
	Reason string `json:"reason,omitempty"`
}

// BuildFact is one 'built' record: the sha an attempt produced.
type BuildFact struct {
	FactRef
	Sha          string `json:"sha"`
	SquashedFrom string `json:"squashed_from,omitempty"`
}

// GateFact is one gate's current verdict for an attempt — either a fresh
// 'gate' judgement or a 'gate-carried' record that rode an earlier judgement
// forward across a rebase. Carried is what tells the two apart: Decision and
// Reason are the ORIGINAL verdict's, resolved from this same chain walk
// rather than a second read, and left empty when that original record is not
// one this walk itself observed.
type GateFact struct {
	FactRef
	Gate     string `json:"gate"`
	Sha      string `json:"sha"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Carried  bool   `json:"carried,omitempty"`
	Reviewed string `json:"reviewed,omitempty"`
	Original string `json:"original,omitempty"`
	PatchID  string `json:"patch_id,omitempty"`
}

// RebaseFact is a 'build-rebased' or 'rebase-refused' record.
type RebaseFact struct {
	FactRef
	// Kind is "build-rebased" or "rebase-refused" — the record's own Kind,
	// repeated here because a caller reading Rebases wants to branch on it
	// without reaching into FactRef.
	Kind   string `json:"kind"`
	Was    string `json:"was,omitempty"`
	From   string `json:"from,omitempty"`
	Onto   string `json:"onto,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// LandFact is one 'land' record: a commit pushed, before and after.
type LandFact struct {
	FactRef
	Repo   string `json:"repo"`
	Branch string `json:"branch,omitempty"`
	Was    string `json:"was,omitempty"`
	Now    string `json:"now,omitempty"`
}

// PRFact is a 'pr-opened' record.
type PRFact struct {
	FactRef
	URL           string `json:"pr_url"`
	Issue         string `json:"issue,omitempty"`
	CompletesPlan bool   `json:"completes_plan"`
	Note          string `json:"note,omitempty"`
}

// LandRefusalFact is a 'land-refused' or 'land-conflict' record.
type LandRefusalFact struct {
	FactRef
	Kind         string `json:"kind"`
	Repo         string `json:"repo,omitempty"`
	Branch       string `json:"branch,omitempty"`
	ExpectedBase string `json:"expected_base,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// CloseFact is an 'issue-closed' record. A unit with no such record has a nil
// Closed on its UnitStory — absent, never inferred from completes_plan.
type CloseFact struct {
	FactRef
	Issue    string `json:"issue,omitempty"`
	Commit   string `json:"commit,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`
	ClosedBy string `json:"closed_by,omitempty"`
}

// Attempt is one build attempt of a unit: the sha it produced and the check
// and review verdicts judged against it (or carried forward onto it by a
// rebase). Nil Check/Review means that gate has not judged this attempt yet,
// never a pending-reads-as-fail assumption.
type Attempt struct {
	N      int        `json:"n"`
	Built  *BuildFact `json:"built,omitempty"`
	Check  *GateFact  `json:"check,omitempty"`
	Review *GateFact  `json:"review,omitempty"`
}

// UnitStory is one plan node's whole account: what it was asked to do, every
// attempt it took, its sessions, its landing, and its close — or the absence
// of one.
type UnitStory struct {
	ItemID       string            `json:"item_id"`
	NodeID       string            `json:"node_id"`
	Summary      string            `json:"summary,omitempty"`
	Sections     []string          `json:"sections,omitempty"`
	DependsOn    []string          `json:"depends_on,omitempty"`
	Attempts     []Attempt         `json:"attempts,omitempty"`
	Rebases      []RebaseFact      `json:"rebases,omitempty"`
	Sessions     []AgentSession    `json:"sessions,omitempty"`
	Landings     []LandFact        `json:"landings,omitempty"`
	PRs          []PRFact          `json:"prs,omitempty"`
	LandRefusals []LandRefusalFact `json:"land_refusals,omitempty"`
	Closed       *CloseFact        `json:"closed,omitempty"`
}

// Totals is a story's cost account, DERIVED strictly from the AgentSession
// rows the story itself lists — never a second fold over control/capacity's
// charge records, which is compacted and would silently undercount (see
// docs/PLAN.md §6, "a count is a projection of the listing, never a second
// reading of it"). Sessions is how many attempts this story lists at all;
// Covered is how many of those carried an agent-result record and so
// contribute to Tokens/CostUSD/WallMS. Sessions > Covered is not an error —
// it is an in-flight or lost attempt whose cost is UNKNOWN, and unknown
// must never be folded in as zero.
type Totals struct {
	Sessions int     `json:"sessions"`
	Covered  int     `json:"covered"`
	Attempts int     `json:"attempts"`
	Tokens   int64   `json:"tokens,omitempty"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
	WallMS   int64   `json:"wall_ms,omitempty"`
}

// OriginStory is one origination's whole audit trail, from the delivery that
// opened it to every unit it produced, its landings and its closes.
type OriginStory struct {
	IngressID  string          `json:"ingress_id"`
	Key        string          `json:"key,omitempty"`
	Route      string          `json:"route,omitempty"`
	Box        string          `json:"box,omitempty"`
	Repo       string          `json:"repo,omitempty"`
	Ingress    IngressFact     `json:"ingress"`
	Plans      []PlanFact      `json:"plans,omitempty"`
	PlanEvents []PlanEventFact `json:"plan_events,omitempty"`
	Sessions   []AgentSession  `json:"sessions,omitempty"`
	Units      []UnitStory     `json:"units,omitempty"`
	Totals     Totals          `json:"totals"`
}

// mentionsPR reports whether any unit's landing opened a PR whose number
// (parsed from pr_url's tail) is n.
func (s OriginStory) mentionsPR(n int) bool {
	for _, u := range s.Units {
		for _, pr := range u.PRs {
			if got, ok := prNumber(pr.URL); ok && got == n {
				return true
			}
		}
	}
	return false
}

// prNumber parses the trailing integer off a PR URL — ".../pull/123" -> 123.
func prNumber(url string) (int, bool) {
	i := strings.LastIndexByte(url, '/')
	if i < 0 || i+1 >= len(url) {
		return 0, false
	}
	n, err := strconv.Atoi(url[i+1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// Origins answers issue #187's door: every origination whose delivery key
// equals key, or extends it with a '-suffix' — the resubmission convention
// (hack/dogfood/submit.py --key-suffix), where each resubmission is its own
// origination, never merged with the one it followed. Newest first.
func (r *Room) Origins(ctx context.Context, key string) ([]OriginStory, error) {
	ids, err := r.matchingOriginations(ctx, key)
	if err != nil {
		return nil, err
	}
	out := make([]OriginStory, 0, len(ids))
	for _, id := range ids {
		st, ok, err := r.originStory(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, st)
		}
	}
	sortStories(out)
	return out, nil
}

// OriginsByPR is a derived reverse lookup: there is no PR-number index
// anywhere in this system (a PR's number lives only in pr-opened and
// issue-closed record bodies), so this walks EVERY indexed origination,
// follows its plans to each unit's landing chain, and matches the number
// parsed from pr_url's tail. It is O(originations × units) by design — this
// is an audit-frequency endpoint, not a hot path, and building a dedicated
// index is a unit of its own, not folded into this one.
func (r *Room) OriginsByPR(ctx context.Context, n int) ([]OriginStory, error) {
	ids, err := r.indexedOriginations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OriginStory, 0)
	for _, id := range ids {
		st, ok, err := r.originStory(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok && st.mentionsPR(n) {
			out = append(out, st)
		}
	}
	sortStories(out)
	return out, nil
}

func sortStories(out []OriginStory) {
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].Ingress.At, out[j].Ingress.At
		if !ai.Equal(aj) {
			return ai.After(aj)
		}
		return out[i].Ingress.Seq > out[j].Ingress.Seq
	})
}

// indexedOriginations lists every origination id on the index/ingress fold,
// in no particular order — callers sort the stories they build from it.
func (r *Room) indexedOriginations(ctx context.Context) ([]string, error) {
	var ids []string
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/ingress", 1) {
		if err != nil {
			return nil, err
		}
		if rec.Kind != "indexed" {
			continue
		}
		ids = append(ids, rec.Subject)
	}
	return ids, nil
}

// matchingOriginations folds the index/ingress scope once, matching an
// origination whose Key equals key or extends it with a '-suffix'.
func (r *Room) matchingOriginations(ctx context.Context, key string) ([]string, error) {
	var ids []string
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "index/ingress", 1) {
		if err != nil {
			return nil, err
		}
		if rec.Kind != "indexed" {
			continue
		}
		var ing ingress.Ingress
		if json.Unmarshal(rec.Body, &ing) != nil {
			continue
		}
		if ing.Key == key || strings.HasPrefix(ing.Key, key+"-") {
			ids = append(ids, rec.Subject)
		}
	}
	return ids, nil
}

// originStory folds one origination's own chain (its RecordOpen, its plans
// and plan events) and every unit its plans named, into the whole story.
// false means id names no origination at all.
func (r *Room) originStory(ctx context.Context, id string) (OriginStory, bool, error) {
	scope := ingress.Scope(id)
	var ing ingress.Ingress
	var openRef FactRef
	found := false
	var plans []PlanFact
	var planEvents []PlanEventFact
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, scope, 1) {
		if err != nil {
			return OriginStory{}, false, err
		}
		switch rec.Kind {
		case ingress.RecordOpen:
			if err := json.Unmarshal(rec.Body, &ing); err != nil {
				return OriginStory{}, false, fmt.Errorf("control: origin story %s: %w", id, err)
			}
			if ing.Opened.IsZero() {
				ing.Opened = rec.At
			}
			openRef = factRef(rec)
			found = true
		case plan.RecordKind:
			var p plan.Plan
			if json.Unmarshal(rec.Body, &p) == nil {
				if p.At.IsZero() {
					p.At = rec.At
				}
				plans = append(plans, PlanFact{FactRef: factRef(rec), Plan: p})
			}
		case plan.FailedKind, plan.DeferredKind, plan.RetriedKind, plan.ResolvedKind:
			planEvents = append(planEvents, PlanEventFact{FactRef: factRef(rec), Reason: string(rec.Body)})
		}
	}
	if !found {
		return OriginStory{}, false, nil
	}

	plannerSessions, err := r.agentSessions(ctx, scope)
	if err != nil {
		return OriginStory{}, false, err
	}

	var units []UnitStory
	for _, pf := range plans {
		for _, n := range pf.Plan.Nodes {
			itemID := pf.Plan.ItemID(n.ID)
			u, err := r.unitStory(ctx, itemID, n)
			if err != nil {
				return OriginStory{}, false, err
			}
			units = append(units, u)
		}
	}

	return OriginStory{
		IngressID: id, Key: ing.Key, Route: ing.Route, Box: ing.Box, Repo: ing.Repo,
		Ingress:    IngressFact{FactRef: openRef, Requester: ing.Requester, Title: ing.Title, Note: ing.Note},
		Plans:      plans,
		PlanEvents: planEvents,
		Sessions:   plannerSessions,
		Units:      units,
		Totals:     storyTotals(plannerSessions, units),
	}, true, nil
}

// gateVerdictBody is a 'gate' record's body, read off [forge.Verdict]'s own
// default JSON encoding — capitalized keys, no tags on the writer's side —
// rather than importing internal/forge for a shape this package only reads a
// subset of.
type gateVerdictBody struct {
	Gate     string
	Decision string
	Reason   string
}

// gateCarriedBody is a 'gate-carried' record's body — see loop.go's
// gateCarriedRecord, the writer's own shape.
type gateCarriedBody struct {
	Gate     string `json:"gate"`
	Reviewed string `json:"reviewed"`
	Original string `json:"original"`
	PatchID  string `json:"patch_id"`
}

// buildRebasedBody is a 'build-rebased' record's body.
type buildRebasedBody struct {
	Was  string `json:"was"`
	From string `json:"from"`
	Onto string `json:"onto"`
}

// rebaseRefusedBody is a 'rebase-refused' record's body.
type rebaseRefusedBody struct {
	Onto   string `json:"onto"`
	Reason string `json:"reason"`
}

// landBody, prOpenedBody, landRefusedBody, landConflictBody and
// issueClosedBody mirror the shapes internal/forge/git writes — see
// git.go's l.record calls and loop.go's recordIssueClosed.
type landBody struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Was    string `json:"was"`
	Now    string `json:"now"`
}

type prOpenedBody struct {
	PRURL         string `json:"pr_url"`
	Issue         string `json:"issue"`
	CompletesPlan bool   `json:"completes_plan"`
	Note          string `json:"note"`
}

type landRefusedBody struct {
	Repo   string `json:"repo"`
	Reason string `json:"reason"`
}

type landConflictBody struct {
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	ExpectedBase string `json:"expected_base"`
}

type issueClosedBody struct {
	Issue    string `json:"issue"`
	Commit   string `json:"commit"`
	PRURL    string `json:"pr_url"`
	ClosedBy string `json:"closed_by"`
}

// unitStory folds one plan node's whole account: its chain of attempts on
// workitem/<itemID>, and its landing chain on changeset/cs-<itemID>.
func (r *Room) unitStory(ctx context.Context, itemID string, node plan.Node) (UnitStory, error) {
	u := UnitStory{
		ItemID: itemID, NodeID: node.ID, Summary: node.Summary,
		Sections: node.Sections, DependsOn: node.DependsOn,
	}

	var attempts []Attempt
	// bySha maps a judged sha to the index of the attempt it currently
	// belongs to — updated on "built" and carried forward across a
	// successful rebase, so a gate verdict on a post-rebase head still
	// attaches to the attempt that produced the content being judged.
	bySha := map[string]int{}
	// verdicts indexes this walk's own 'gate' records by their chain id, so
	// a later 'gate-carried' record can resolve the ORIGINAL decision and
	// reason from records this same pass already observed, rather than a
	// second read of the chain.
	verdicts := map[string]GateFact{}

	attachGate := func(idx int, gf GateFact) {
		if idx < 0 || idx >= len(attempts) {
			return
		}
		switch gf.Gate {
		case "check":
			attempts[idx].Check = &gf
		case "review":
			attempts[idx].Review = &gf
		}
	}

	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, "workitem/"+itemID, 1) {
		if err != nil {
			return UnitStory{}, err
		}
		switch rec.Kind {
		case "built":
			var body struct {
				SquashedFrom string `json:"squashed_from"`
			}
			json.Unmarshal(rec.Body, &body)
			bySha[rec.Subject] = len(attempts)
			attempts = append(attempts, Attempt{
				N:     len(attempts) + 1,
				Built: &BuildFact{FactRef: factRef(rec), Sha: rec.Subject, SquashedFrom: body.SquashedFrom},
			})
		case "build-rebased":
			var body buildRebasedBody
			json.Unmarshal(rec.Body, &body)
			u.Rebases = append(u.Rebases, RebaseFact{
				FactRef: factRef(rec), Kind: rec.Kind, Was: body.Was, From: body.From, Onto: body.Onto,
			})
			if idx, ok := bySha[body.Was]; ok {
				bySha[rec.Subject] = idx
			} else if len(attempts) > 0 {
				bySha[rec.Subject] = len(attempts) - 1
			}
		case "rebase-refused":
			var body rebaseRefusedBody
			json.Unmarshal(rec.Body, &body)
			u.Rebases = append(u.Rebases, RebaseFact{
				FactRef: factRef(rec), Kind: rec.Kind, Onto: body.Onto, Reason: body.Reason,
			})
		case "gate":
			var body gateVerdictBody
			if json.Unmarshal(rec.Body, &body) != nil || (body.Gate != "check" && body.Gate != "review") {
				continue
			}
			gf := GateFact{
				FactRef: factRef(rec), Gate: body.Gate, Sha: rec.Subject,
				Decision: body.Decision, Reason: body.Reason,
			}
			verdicts[rec.ID] = gf
			idx, ok := bySha[rec.Subject]
			if !ok && len(attempts) > 0 {
				idx = len(attempts) - 1
			}
			attachGate(idx, gf)
		case "gate-carried":
			var body gateCarriedBody
			if json.Unmarshal(rec.Body, &body) != nil || (body.Gate != "check" && body.Gate != "review") {
				continue
			}
			orig, ok := verdicts[body.Original]
			gf := GateFact{
				FactRef: factRef(rec), Gate: body.Gate, Sha: rec.Subject,
				Carried: true, Reviewed: body.Reviewed, Original: body.Original, PatchID: body.PatchID,
			}
			if ok {
				gf.Decision, gf.Reason = orig.Decision, orig.Reason
			}
			idx, has := bySha[rec.Subject]
			if !has && len(attempts) > 0 {
				idx = len(attempts) - 1
			}
			attachGate(idx, gf)
		case "issue-closed":
			var body issueClosedBody
			json.Unmarshal(rec.Body, &body)
			u.Closed = &CloseFact{
				FactRef: factRef(rec), Issue: body.Issue, Commit: body.Commit,
				PRURL: body.PRURL, ClosedBy: body.ClosedBy,
			}
		}
	}
	u.Attempts = attempts

	sessions, err := r.agentSessions(ctx, "workitem/"+itemID)
	if err != nil {
		return UnitStory{}, err
	}
	u.Sessions = sessions

	csScope := "changeset/cs-" + itemID
	for rec, err := range r.Store.Log().Since(ctx, r.Tenant, csScope, 1) {
		if err != nil {
			return UnitStory{}, err
		}
		switch rec.Kind {
		case "land":
			var body landBody
			json.Unmarshal(rec.Body, &body)
			u.Landings = append(u.Landings, LandFact{
				FactRef: factRef(rec), Repo: body.Repo, Branch: body.Branch, Was: body.Was, Now: body.Now,
			})
		case "pr-opened":
			var body prOpenedBody
			json.Unmarshal(rec.Body, &body)
			u.PRs = append(u.PRs, PRFact{
				FactRef: factRef(rec), URL: body.PRURL, Issue: body.Issue,
				CompletesPlan: body.CompletesPlan, Note: body.Note,
			})
		case "land-refused":
			var body landRefusedBody
			json.Unmarshal(rec.Body, &body)
			u.LandRefusals = append(u.LandRefusals, LandRefusalFact{
				FactRef: factRef(rec), Kind: rec.Kind, Repo: body.Repo, Reason: body.Reason,
			})
		case "land-conflict":
			var body landConflictBody
			json.Unmarshal(rec.Body, &body)
			u.LandRefusals = append(u.LandRefusals, LandRefusalFact{
				FactRef: factRef(rec), Kind: rec.Kind, Repo: body.Repo, Branch: body.Branch,
				ExpectedBase: body.ExpectedBase,
			})
		}
	}

	return u, nil
}

// storyTotals derives a story's cost account strictly from the AgentSession
// rows the story itself lists — see [Totals].
func storyTotals(planner []AgentSession, units []UnitStory) Totals {
	var t Totals
	fold := func(sessions []AgentSession) {
		t.Sessions += len(sessions)
		for _, s := range sessions {
			if !s.Finished {
				continue
			}
			t.Covered++
			t.Tokens += s.Tokens
			t.CostUSD += s.CostUSD
			t.WallMS += s.WallMS
		}
	}
	fold(planner)
	for _, u := range units {
		fold(u.Sessions)
		t.Attempts += len(u.Attempts)
	}
	return t
}
