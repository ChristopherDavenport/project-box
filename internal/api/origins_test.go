package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChristopherDavenport/project-box/internal/queue"
)

// TestOriginsOverTheWire is issue #187's door, wired: an origination opened
// and landed through the daemon answers on GET /v1/origins/{key} and its
// /v1/issues/{n} alias with its whole story, and /export hands the same
// story back as a downloadable artifact.
func TestOriginsOverTheWire(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	sock := filepath.Join(t.TempDir(), "boxd.sock")
	ln, err := Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: w.srv.Handler(), ConnContext: ConnContext}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	c, err := Dial("unix:"+sock, "", "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.OpenIngress(ctx, "issues", "issue-187", "wire the origins door", "")
	if err != nil || !res.Opened {
		t.Fatalf("open: %+v %v", res, err)
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.pump.Quiesce(ctx) {
		t.Fatal("quiesce did not settle the pass's dispatched run")
	}
	item, err := c.Item(ctx, res.Item)
	if err != nil || item.State != queue.StateDone {
		t.Fatalf("item over the wire: %+v %v", item, err)
	}

	stories, err := c.Origins(ctx, "issue-187")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 || stories[0].IngressID != res.Ingress.ID {
		t.Fatalf("origins by key: %+v %v", stories, err)
	}
	if len(stories[0].Units) != 1 || stories[0].Units[0].ItemID != item.ID {
		t.Fatalf("origins story units: %+v", stories[0].Units)
	}

	byIssue, err := c.OriginsByIssue(ctx, "187")
	if err != nil || len(byIssue) != 1 || byIssue[0].IngressID != res.Ingress.ID {
		t.Fatalf("origins by issue number: %+v %v", byIssue, err)
	}

	// No PR was opened by this rig's plain git.Lander, so the reverse
	// lookup answers empty rather than erroring.
	byPR, err := c.OriginsByPR(ctx, "999999")
	if err != nil || len(byPR) != 0 {
		t.Fatalf("origins by pr: %+v %v", byPR, err)
	}

	// A key naming nothing at all answers an empty list, not an error.
	none, err := c.Origins(ctx, "issue-nonesuch")
	if err != nil || len(none) != 0 {
		t.Fatalf("origins for an unknown key: %+v %v", none, err)
	}
}

// TestOriginsExportIsDownloadable checks the export route's headers directly
// over HTTP, the way TestProvenanceChainAndExport does for the provenance
// artifact — a client library will not surface a Content-Disposition
// header, so this reads the raw response.
func TestOriginsExportIsDownloadable(t *testing.T) {
	w := rig(t)
	ctx := context.Background()

	hs := httptest.NewServer(w.srv.Handler())
	t.Cleanup(hs.Close)

	c, err := Dial("tcp:"+strings.TrimPrefix(hs.URL, "http://"), "", "chris")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenIngress(ctx, "issues", "issue-188", "export the story", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pump.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(hs.URL + "/v1/origins/issue-188/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("export content-type = %q", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "issue-188") {
		t.Fatalf("export content-disposition = %q, want an attachment naming the key", cd)
	}
}

// TestOriginsByPRRejectsANonNumericNumber: the route only makes sense keyed
// by an integer PR number, and a caller that names one wrong gets told so
// rather than an opaque 500 from a failed parse two layers down.
func TestOriginsByPRRejectsANonNumericNumber(t *testing.T) {
	w := rig(t)
	hs := httptest.NewServer(w.srv.Handler())
	t.Cleanup(hs.Close)

	resp, err := http.Get(hs.URL + "/v1/prs/not-a-number")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
