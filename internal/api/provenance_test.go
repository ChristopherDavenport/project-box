package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestProvenanceChainAndExport drains a real change through the loop and
// reads it back the way an auditor will: the assembled chain over the wire,
// and the export as one file that must stand on its own — no live calls, no
// scripts, and records that re-verify offline from the artifact's own bytes.
func TestProvenanceChainAndExport(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	hs := httptest.NewServer(w.srv.Handler())
	t.Cleanup(hs.Close)
	c, err := Dial("tcp:"+strings.TrimPrefix(hs.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.OpenIngress(ctx, "issues", "issue-16", "ship the chain view", "")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone || item.Head == "" {
		t.Fatalf("item: %+v %v%s", item, err, queueDump(t, c))
	}

	// The chain, assembled: the published order, every span verified, and a
	// complete landing showing no gap at all.
	view, err := c.ProvenanceChain(ctx, item.Head)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Chain.Gaps) != 0 {
		t.Fatalf("a drained landing reported gaps: %+v", view.Chain.Gaps)
	}
	var order []string
	for _, s := range view.Chain.Spans {
		order = append(order, s.Scope)
		if s.Verify != "" {
			t.Errorf("span %s does not verify: %s", s.Scope, s.Verify)
		}
	}
	if len(order) != 4 ||
		!strings.HasPrefix(order[0], "ingress/") ||
		!strings.HasPrefix(order[1], "workitem/") ||
		!strings.HasPrefix(order[2], "changeset/") ||
		!strings.HasPrefix(order[3], "checks/") {
		t.Fatalf("chain order %v, want ingress → work item → changeset → checks", order)
	}
	// The diagram ships laid out: the browser renders, it does not lay out.
	if !strings.Contains(view.SVG, "<svg") || !strings.Contains(view.SVG, `aria-hidden="true"`) {
		t.Fatalf("the diagram must arrive as laid-out markup, hidden from readers of the list: %.80s", view.SVG)
	}

	// The export.
	resp, err := http.Get(hs.URL + "/v1/provenance/" + item.Head + "/export")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	doc := string(page)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("export content type %q", ct)
	}
	if !strings.Contains(doc, item.Head) {
		t.Fatal("the artifact does not name the commit it is about")
	}
	// Self-contained means self-contained: nothing to fetch, nothing to run.
	if strings.Contains(doc, "<script") {
		t.Fatal("the artifact requires JavaScript to render")
	}
	for _, m := range regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(doc, -1) {
		if strings.HasPrefix(m[1], "http") || strings.HasPrefix(m[1], "//") {
			t.Fatalf("the artifact reaches out to %q — an export must make no live calls", m[1])
		}
	}
	// The human-versus-automation distinction is the edge property an auditor
	// probes first: both principals must be on the page.
	if !strings.Contains(doc, "on behalf of") {
		t.Fatal("the artifact does not show the on-behalf-of principal")
	}
	// Verifiable offline: the embedded JSON alone must re-verify, span by
	// span, with the same checks an auditor's own tooling would run.
	m := regexp.MustCompile(`data:application/json;base64,([A-Za-z0-9+/=]+)`).FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("the artifact carries no machine-readable records")
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatal(err)
	}
	var chain evidence.Chain
	if err := json.Unmarshal(raw, &chain); err != nil {
		t.Fatal(err)
	}
	if len(chain.Spans) != 4 || chain.Subject != item.Head {
		t.Fatalf("embedded chain: subject=%s spans=%d", chain.Subject, len(chain.Spans))
	}
	for _, s := range chain.Spans {
		// The per-entity spans embed whole and must re-verify from the
		// artifact's bytes alone; the checks span is a standing scope, so it
		// carries only the links and its verification is the export's claim.
		if strings.HasPrefix(s.Scope, "checks/") {
			if s.Whole {
				t.Fatalf("a standing scope embedded whole: %s", s.Scope)
			}
			continue
		}
		if !s.Whole {
			t.Fatalf("a per-entity span did not embed whole: %s", s.Scope)
		}
		if err := evidence.VerifyRecords(s.Records); err != nil {
			t.Fatalf("embedded span %s does not verify offline: %v", s.Scope, err)
		}
	}
}

// TestProvenanceExportNamesTheGap: a chain with a missing link renders as a
// missing link, never as a shorter chain — in the assembled JSON and on the
// artifact both.
func TestProvenanceExportNamesTheGap(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	// A settle with no landing behind it: the queue writes done at a head no
	// changeset ever landed — precisely the shape silent elision would hide.
	sha := "feedfacefeedfacefeedfacefeedfacefeedface"
	q := &queue.Queue{Store: w.srv.Store, Runner: "d", TTL: time.Hour}
	item, err := q.Submit(ctx, queue.WorkItem{
		ID: "wi-gap", Tenant: tn, Box: "shop", Ingress: "ing-gap", Task: "gap probe",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, claim, err := q.Claim(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Done(ctx, item, claim, forge.Commit(sha)); err != nil {
		t.Fatal(err)
	}

	hs := httptest.NewServer(w.srv.Handler())
	t.Cleanup(hs.Close)
	c, _ := Dial("tcp:"+strings.TrimPrefix(hs.URL, "http://"), "", "chris")
	view, err := c.ProvenanceChain(ctx, sha)
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]bool{}
	for _, g := range view.Chain.Gaps {
		links[g.Link] = true
	}
	// No landing names the sha, nothing gated it, and the named ingress was
	// never opened: three absent links, each named.
	for _, want := range []string{"landing", "gate", "ingress"} {
		if !links[want] {
			t.Fatalf("gap %q not named in %+v", want, view.Chain.Gaps)
		}
	}
	if !strings.Contains(view.SVG, "missing") {
		t.Fatal("the diagram elides the missing link")
	}

	resp, err := http.Get(hs.URL + "/v1/provenance/" + sha + "/export")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "missing landing") {
		t.Fatal("the artifact does not name the missing landing")
	}
}
