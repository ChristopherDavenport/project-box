package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

// The provenance chain, presented. GET /v1/provenance/{sha} stays the raw
// audit query; /chain is the same answer assembled — spans verified, gaps
// named, diagram laid out — and /export is that assembly as one
// self-contained file an auditor can be handed without a daemon, a token, or
// a loopback port.
//
// Layout is computed here, in Go, and shipped as markup: the browser renders
// and does not lay out (the decision is recorded in ARCHITECTURE, "Where that
// layout is computed"). One renderer serves the live room and the offline
// artifact, which is what keeps the two views the same view.

// ProvenanceView is the chain endpoint's body: the assembled chain, and the
// diagram already laid out. The ordered list is the representation of record;
// the SVG sits on top of it, aria-hidden, for readers it helps.
type ProvenanceView struct {
	Chain evidence.Chain `json:"chain"`
	SVG   string         `json:"svg"`
}

func (s *Server) provenanceChain(w http.ResponseWriter, r *http.Request) {
	c, err := evidence.Provenance(r.Context(), s.Store.Log(), s.Tenant, r.PathValue("sha"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	writeJSON(w, ProvenanceView{Chain: c, SVG: chainSVG(c)})
}

func (s *Server) provenanceExport(w http.ResponseWriter, r *http.Request) {
	c, err := evidence.Provenance(r.Context(), s.Store.Log(), s.Tenant, r.PathValue("sha"))
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	page, err := exportHTML(c, s.Tenant, time.Now().UTC())
	if err != nil {
		httpErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}

// chainPos orders the diagram the way the published chain reads:
// ingress → work item → run → gates → changeset → landing → checks →
// promotion. Spans and gaps share the scale so a missing link renders in the
// place the chain expects it, never as a shorter chain.
func chainPos(link string) int {
	switch link {
	case "ingress":
		return 0
	case "work item":
		return 10
	case "run":
		return 12
	case "gate":
		return 14
	case "changeset":
		return 20
	case "landing":
		return 22
	case "checks":
		return 30
	case "promotion":
		return 40
	}
	return 50
}

// linkOf names the chain link a scope carries, and the scope's own suffix.
func linkOf(scope string) (link, sub string) {
	prefix, rest, ok := strings.Cut(scope, "/")
	if !ok {
		return scope, ""
	}
	switch prefix {
	case "ingress":
		return "ingress", rest
	case "workitem":
		return "work item", rest
	case "changeset":
		return "changeset", rest
	case "checks":
		return "checks", rest
	case "ladder":
		return "promotion", rest
	}
	return prefix, rest
}

type diagNode struct {
	label, sub string
	pos        int
	missing    bool // a named gap: rendered as the broken link it is
	broken     bool // the span's own chain does not verify
}

// chainSVG lays the chain out and renders it. Coordinates are computed here
// and nowhere else; the element is aria-hidden because the ordered list is
// the primary form — a provenance chain that exists only as an SVG is
// unreadable evidence to a screen reader.
func chainSVG(c evidence.Chain) string {
	var nodes []diagNode
	for _, s := range c.Spans {
		link, sub := linkOf(s.Scope)
		nodes = append(nodes, diagNode{label: link, sub: sub, pos: chainPos(link), broken: s.Verify != ""})
	}
	for _, g := range c.Gaps {
		nodes = append(nodes, diagNode{label: g.Link, sub: "missing", pos: chainPos(g.Link) + 1, missing: true})
	}
	if len(nodes) == 0 {
		return ""
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].pos < nodes[j].pos })

	const (
		nodeW, nodeH = 136, 52
		gapW         = 44
		margin       = 12
	)
	width := margin*2 + len(nodes)*nodeW + (len(nodes)-1)*gapW
	height := margin*2 + nodeH
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-hidden="true">`,
		width, height, width, height)
	b.WriteString(`<defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0 L10 5 L0 10 z" fill="currentColor"/></marker></defs>`)
	for i, n := range nodes {
		x := margin + i*(nodeW+gapW)
		y := margin
		if i > 0 {
			dash := ""
			if n.missing || nodes[i-1].missing {
				dash = ` stroke-dasharray="5 4"`
			}
			fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="currentColor" stroke-width="1.5" marker-end="url(#arrow)"%s/>`,
				x-gapW+2, y+nodeH/2, x-3, y+nodeH/2, dash)
		}
		dash := ""
		if n.missing {
			dash = ` stroke-dasharray="6 4"`
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="6" fill="none" stroke="currentColor" stroke-width="1.5"%s/>`,
			x, y, nodeW, nodeH, dash)
		label := n.label
		if n.missing {
			label = "missing: " + label
		}
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="currentColor" font-size="13" font-weight="600">%s</text>`,
			x+nodeW/2, y+21, html.EscapeString(clipTo(label, 18)))
		sub := n.sub
		if n.broken {
			sub = "does not verify"
		}
		if sub != "" {
			fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="currentColor" opacity="0.65" font-size="11">%s</text>`,
				x+nodeW/2, y+38, html.EscapeString(clipTo(sub, 20)))
		}
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func clipTo(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// shortRef abbreviates a ref the way git does — for filenames, where an
// ellipsis is not a character anyone wants.
func shortRef(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// The export: one file, no live calls, no JavaScript required to render. The
// ordered list is the document; the diagram is decoration on top of it; the
// exact records ride along as a data: download so the hash chains can be
// re-verified offline from the artifact alone.
type exportRecord struct {
	Seq                                uint64
	Kind, At, Actor, OnBehalfOf, Grant string
	Subject, ID, Prev, Body            string
	Names                              bool
}

type exportSpan struct {
	Link, Scope, WORM string
	// Health is the span's verification sentence, precomputed so the
	// template states exactly the claim the walk earned — a whole chain
	// embedded, or a standing scope proven but carried as its links alone.
	Health  string
	Broken  bool
	Records []exportRecord
}

type exportPage struct {
	Subject, Tenant, Version, At string
	Gaps                         []evidence.Gap
	Spans                        []exportSpan
	SVG                          template.HTML
	// JSONHref is the chain as a data: URI — the machine-readable half of the
	// artifact, byte-exact where rendered HTML is not.
	JSONHref template.URL
	JSONName string
}

func exportHTML(c evidence.Chain, t tenant.ID, now time.Time) ([]byte, error) {
	p := exportPage{
		Subject: c.Subject, Tenant: string(t), Version: version.String(),
		At:       now.Format(time.RFC3339),
		Gaps:     c.Gaps,
		SVG:      template.HTML(chainSVG(c)),
		JSONName: "provenance-" + shortRef(c.Subject) + ".json",
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	p.JSONHref = template.URL("data:application/json;base64," + base64.StdEncoding.EncodeToString(raw))
	for _, s := range c.Spans {
		link, _ := linkOf(s.Scope)
		es := exportSpan{Link: link, Scope: s.Scope}
		switch {
		case s.Verify != "":
			es.Broken = true
			es.Health = "CHAIN BROKEN: " + s.Verify
		case s.Whole:
			es.Health = fmt.Sprintf("%d record(s), chain verifies end to end — embedded whole, re-verifiable offline.", s.Total)
		default:
			es.Health = fmt.Sprintf("standing scope: %d record(s) verify end to end; the %d naming this commit are embedded.", s.Total, len(s.Records))
		}
		if s.Compacted != nil {
			lock := "no retention lock (local sink)"
			if !s.Compacted.Export.RetainUntil.IsZero() {
				lock = "retention locked until " + s.Compacted.Export.RetainUntil.Format(time.RFC3339)
			}
			es.WORM = fmt.Sprintf("records %d–%d pruned to WORM at %s (digest %s) — %s",
				s.Compacted.From, s.Compacted.Through, s.Compacted.Export.URI, s.Compacted.Export.Digest, lock)
		}
		named := map[uint64]bool{}
		for _, seq := range s.Names {
			named[seq] = true
		}
		for _, r := range s.Records {
			es.Records = append(es.Records, exportRecord{
				Seq: r.Seq, Kind: r.Kind, At: r.At.Format(time.RFC3339),
				Actor: r.Actor, OnBehalfOf: r.OnBehalfOf, Grant: r.Grant,
				Subject: r.Subject, ID: r.ID, Prev: r.Prev,
				Body:  prettyBody(r.Body),
				Names: named[r.Seq],
			})
		}
		p.Spans = append(p.Spans, es)
	}
	var b strings.Builder
	if err := exportTmpl.Execute(&b, p); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func prettyBody(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(b, &v) == nil {
		if out, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(out)
		}
	}
	return string(b)
}

var exportTmpl = template.Must(template.New("export").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Provenance of {{.Subject}}</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 system-ui, sans-serif; margin: 2rem auto; max-width: 60rem; padding: 0 1rem; }
  code, pre { font: 13px/1.45 ui-monospace, monospace; }
  pre { overflow-x: auto; padding: .5rem; border: 1px solid color-mix(in srgb, currentColor 25%, transparent); border-radius: 6px; }
  h1 code { font-size: .8em; word-break: break-all; }
  .gaps { border: 2px solid currentColor; border-radius: 8px; padding: .75rem 1rem; }
  .gaps h2 { margin: 0 0 .5rem; }
  figure { margin: 1.5rem 0; overflow-x: auto; }
  figcaption { font-size: .85em; opacity: .7; }
  ol.chain { list-style: none; padding: 0; }
  ol.chain > li { border: 1px solid color-mix(in srgb, currentColor 25%, transparent); border-radius: 8px; padding: .75rem 1rem; margin: 0 0 1rem; }
  ol.records { list-style: none; padding: 0; }
  ol.records > li { border-top: 1px solid color-mix(in srgb, currentColor 15%, transparent); padding: .5rem 0; }
  ol.records > li.context { opacity: .65; }
  .verify, .worm { font-size: .9em; }
  .broken { font-weight: 700; }
  .hash { font-size: .8em; opacity: .7; word-break: break-all; }
  .meta { font-size: .9em; }
  footer { margin-top: 2rem; font-size: .85em; opacity: .8; }
</style>
</head>
<body>
<main>
<h1>Provenance of <code>{{.Subject}}</code></h1>
<p class="meta">Tenant {{.Tenant}} · exported {{.At}} · project-box {{.Version}}.
This file is self-contained: it makes no network requests and needs no
JavaScript. The exact records are embedded as
<a href="{{.JSONHref}}" download="{{.JSONName}}">machine-readable JSON</a>,
so each span's hash chain can be re-verified offline from this artifact alone.</p>

{{if .Gaps}}<section class="gaps" role="alert" aria-label="Missing links">
<h2>The chain has {{len .Gaps}} missing link(s)</h2>
<ol>{{range .Gaps}}<li><strong>missing {{.Link}}</strong> — {{.Why}}</li>{{end}}</ol>
</section>{{end}}

{{if not .Spans}}<p>Nothing is recorded about this commit. Either it did not
come through this factory, or it has not landed yet — and for a sampled
change, that absence is itself the finding.</p>{{else}}

<figure aria-hidden="true">{{.SVG}}<figcaption>The chain as a diagram. The
ordered list below is the representation of record.</figcaption></figure>

<h2>The chain</h2>
<ol class="chain">
{{range .Spans}}<li>
  <h3>{{.Link}} — <code>{{.Scope}}</code></h3>
  <p class="verify{{if .Broken}} broken{{end}}">{{.Health}}</p>
  {{if .WORM}}<p class="worm">Crosses into WORM: {{.WORM}}</p>{{end}}
  <ol class="records">
  {{range .Records}}<li{{if not .Names}} class="context"{{end}}>
    <p><strong>#{{.Seq}} {{.Kind}}</strong> — {{.At}}{{if .Names}} · names this commit{{end}}</p>
    <p>actor {{.Actor}}{{if .OnBehalfOf}}, on behalf of {{.OnBehalfOf}}{{else}}, on its own authority{{end}}{{if .Grant}} · grant {{.Grant}}{{end}}{{if .Subject}} · subject <code>{{.Subject}}</code>{{end}}</p>
    {{if .Body}}<details><summary>body</summary><pre>{{.Body}}</pre></details>{{end}}
    <p class="hash">id {{.ID}}{{if .Prev}} ← prev {{.Prev}}{{end}}</p>
  </li>{{end}}
  </ol>
</li>{{end}}
</ol>
{{end}}

<footer>Each record's id is the SHA-256 of its canonical serialization and
each names its predecessor, so removing or reordering anything breaks the
chain — the checks run from these bytes alone, no service required.</footer>
</main>
</body>
</html>
`))
