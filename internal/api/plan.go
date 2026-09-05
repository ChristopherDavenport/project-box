package api

import (
	"fmt"
	"html"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/ingress"
	"github.com/ChristopherDavenport/project-box/internal/plan"
)

// The plan surface: what a decomposition decided, rendered and diffed. It is
// read-only on top of the record intake already writes — nothing here plans,
// retries, or resolves anything; that stays on the exception path in api.go.

// PlanView is the plan endpoint's body: the sealed plan itself, the fan-out
// pulled out as its own field (an operator has to see how many units this
// plan calls for before anything else, not derive it by counting nodes), and
// the diagram laid out.
type PlanView struct {
	plan.Plan
	FanOut int    `json:"fan_out"`
	SVG    string `json:"svg"`
}

// PlanSummary is what an ingress's plan listing carries — enough for a diff
// view to find the previous run of the same decomposer without pulling every
// full graph.
type PlanSummary struct {
	Ref        string    `json:"ref"`
	FanOut     int       `json:"fan_out"`
	Decomposer string    `json:"decomposer"`
	Replayable bool      `json:"replayable"`
	At         time.Time `json:"at"`
}

func (s *Server) planByRef(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		httpErr(w, http.StatusNotFound, "no plan store: this daemon does not carry decomposition")
		return
	}
	ref := r.PathValue("ref")
	p, ok, err := s.Plans.ByRef(r.Context(), s.Tenant, ref)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if !ok {
		httpErr(w, http.StatusNotFound, "no plan named "+ref)
		return
	}
	writeJSON(w, PlanView{Plan: p, FanOut: p.FanOut(), SVG: planSVG(p)})
}

// IngressView is one origination's facts and where its judgement stands — the
// answer a judgement's session card links to. GET /v1/items/{id} 404s on an
// ing- id (it only ever calls Queue.Load, and an origination is not a queue
// item — the bug issue #47 names); this is the honest route instead of
// teaching that one endpoint two incompatible response shapes.
type IngressView struct {
	ID        string    `json:"id"`
	Box       string    `json:"box,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	Task      string    `json:"task"`
	Requester string    `json:"requester,omitempty"`
	Opened    time.Time `json:"opened"`
	// Standing is "", planned, failed, retried or resolved — the newest
	// planning event on the origination's chain, empty meaning the judgement
	// is still running. Nil Plans (a daemon carrying no decomposer) leaves it
	// empty rather than guessing.
	Standing string `json:"standing,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func (s *Server) ingress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ing, ok, err := ingress.Load(r.Context(), s.Store.Log(), s.Tenant, id)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if !ok {
		httpErr(w, http.StatusNotFound, "no origination "+id)
		return
	}
	out := IngressView{
		ID: ing.ID, Box: ing.Box, Repo: ing.Repo,
		Task: plan.TaskOf(ing), Requester: ing.Requester, Opened: ing.Opened,
	}
	if s.Plans != nil {
		st, err := s.Plans.Standing(r.Context(), s.Tenant, id)
		if err != nil {
			httpErr(w, 500, err.Error())
			return
		}
		out.Standing, out.Reason = st.State, st.Reason
	}
	writeJSON(w, out)
}

func (s *Server) ingressPlans(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		httpErr(w, http.StatusNotFound, "no plan store: this daemon does not carry decomposition")
		return
	}
	all, err := s.Plans.All(r.Context(), s.Tenant, r.PathValue("id"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	out := make([]PlanSummary, 0, len(all))
	for _, p := range all {
		out = append(out, PlanSummary{
			Ref: p.Ref, FanOut: p.FanOut(), Decomposer: p.Decomposer,
			Replayable: p.Replayable, At: p.At,
		})
	}
	writeJSON(w, out)
}

func (s *Server) planDiff(w http.ResponseWriter, r *http.Request) {
	if s.Plans == nil {
		httpErr(w, http.StatusNotFound, "no plan store: this daemon does not carry decomposition")
		return
	}
	ref, other := r.PathValue("ref"), r.PathValue("other")
	a, ok, err := s.Plans.ByRef(r.Context(), s.Tenant, ref)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if !ok {
		httpErr(w, http.StatusNotFound, "no plan named "+ref)
		return
	}
	b, ok, err := s.Plans.ByRef(r.Context(), s.Tenant, other)
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	if !ok {
		httpErr(w, http.StatusNotFound, "no plan named "+other)
		return
	}
	writeJSON(w, plan.Diff(a, b))
}

// depthOf lays out a node one column deeper than the deepest of its
// dependencies, zero when it has none. A plan on the wire has already been
// through [plan.Plan.Seal], which refuses a graph with a dependency cycle,
// so the recursion here always terminates.
func depthOf(byID map[string]plan.Node, memo map[string]int, id string) int {
	if d, ok := memo[id]; ok {
		return d
	}
	d := 0
	for _, dep := range byID[id].DependsOn {
		if dd := depthOf(byID, memo, dep) + 1; dd > d {
			d = dd
		}
	}
	memo[id] = d
	return d
}

// planSVG lays the graph out by dependency depth and renders it, following
// provenance.go's chainSVG as precedent: coordinates computed here in Go and
// shipped as markup (the settled decision that graph layout is never the
// browser's job), the element aria-hidden because the ordered node list —
// not the picture — is the record.
func planSVG(p plan.Plan) string {
	if len(p.Nodes) == 0 {
		return ""
	}
	nodes := slices.Clone(p.Nodes)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	byID := make(map[string]plan.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	memo := map[string]int{}
	depth := make(map[string]int, len(nodes))
	maxDepth := 0
	for _, n := range nodes {
		d := depthOf(byID, memo, n.ID)
		depth[n.ID] = d
		if d > maxDepth {
			maxDepth = d
		}
	}

	// Columns by depth, rows within a column ordered by ID — deterministic,
	// so the same graph always lays out the same way.
	cols := make([][]string, maxDepth+1)
	for _, n := range nodes {
		cols[depth[n.ID]] = append(cols[depth[n.ID]], n.ID)
	}
	maxRows := 0
	for _, ids := range cols {
		if len(ids) > maxRows {
			maxRows = len(ids)
		}
	}

	const (
		nodeW, nodeH = 168, 56
		colGap       = 56
		rowGap       = 16
		margin       = 12
	)
	type pos struct{ x, y int }
	posOf := make(map[string]pos, len(nodes))
	for d, ids := range cols {
		x := margin + d*(nodeW+colGap)
		for i, id := range ids {
			posOf[id] = pos{x: x, y: margin + i*(nodeH+rowGap)}
		}
	}
	width := margin*2 + (maxDepth+1)*nodeW + maxDepth*colGap
	height := margin*2 + maxRows*nodeH + max0(maxRows-1)*rowGap

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-hidden="true">`,
		width, height, width, height)
	b.WriteString(`<defs><marker id="planarrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0 L10 5 L0 10 z" fill="currentColor"/></marker></defs>`)

	// One edge line per DependsOn pair, drawn before the nodes so the boxes
	// sit on top of the lines rather than the lines crossing the labels.
	for _, n := range nodes {
		to := posOf[n.ID]
		deps := slices.Clone(n.DependsOn)
		slices.Sort(deps)
		for _, dep := range deps {
			from, ok := posOf[dep]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="currentColor" stroke-width="1.5" marker-end="url(#planarrow)"/>`,
				from.x+nodeW, from.y+nodeH/2, to.x, to.y+nodeH/2)
		}
	}
	for _, n := range nodes {
		p := posOf[n.ID]
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="6" fill="none" stroke="currentColor" stroke-width="1.5"/>`,
			p.x, p.y, nodeW, nodeH)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="currentColor" font-size="13" font-weight="600">%s</text>`,
			p.x+nodeW/2, p.y+22, html.EscapeString(clipTo(n.ID, 22)))
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="currentColor" opacity="0.65" font-size="11">%s</text>`,
			p.x+nodeW/2, p.y+40, html.EscapeString(clipTo(n.Task, 26)))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
