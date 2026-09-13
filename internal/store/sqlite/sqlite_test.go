package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
	"github.com/ChristopherDavenport/project-box/internal/store/conformance"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) conformance.Harness {
		var mu sync.Mutex
		offset := time.Duration(0)
		s, err := Open(filepath.Join(t.TempDir(), "box.db"), WithClock(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return time.Now().Add(offset)
		}))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return conformance.Harness{
			Store: s,
			Advance: func(d time.Duration) {
				mu.Lock()
				defer mu.Unlock()
				offset += d
			},
		}
	})
}

// TestAppendOnlyIsStructural goes behind the interface on purpose: the
// append-only property must hold in the database, not merely in this
// package's discipline, so a direct UPDATE and DELETE must both be refused by
// the schema itself.
func TestAppendOnlyIsStructural(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "box.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.Log().Append(ctx, evidence.Record{
		Tenant: tenant.ID("t1"), Scope: "scope", Kind: "test", Subject: "sha", Actor: "a", Body: []byte("x"),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE chain SET body=X'00'`); err == nil {
		t.Fatal("direct UPDATE of the chain must be refused by the schema")
	}
	if _, err := s.w.Exec(`DELETE FROM chain`); err == nil {
		t.Fatal("direct DELETE of the chain must be refused by the schema")
	}
	// Compaction is the one named exception: it passes the gate inside its
	// own transaction — and the gate is closed again the moment it commits,
	// so a stray delete afterwards is refused exactly as before.
	head, err := s.Log().Append(ctx, evidence.Record{
		Tenant: tenant.ID("t1"), Scope: "scope", Kind: "test", Subject: "sha", Actor: "a", Body: []byte("y"),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	var pruned []evidence.Record
	for r, err := range s.Log().Since(ctx, tenant.ID("t1"), "scope", 1) {
		if err != nil {
			t.Fatalf("since: %v", err)
		}
		pruned = append(pruned, r)
	}
	if _, err := s.Log().Compact(ctx, tenant.ID("t1"), "scope", head.Seq, evidence.Export{
		Tenant: tenant.ID("t1"), Scope: "scope",
		From: pruned[0].Seq, To: head.Seq, Head: head.ID,
		URI: "mem://test", Digest: "d",
	}, "compactor"); err != nil {
		t.Fatalf("compact through the gate: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM chain`); err == nil {
		t.Fatal("direct DELETE after a compaction must still be refused — the gate closes with the transaction")
	}
}

// TestOpenUpgradesARunlogTableWithNoSessionColumn is the repository's first
// schema evolution, proven against the shape it must upgrade: a database
// built before the session column existed, with a row already in it. Open
// must add the column in place and go on serving both the old row (reading
// back with an empty session, never an invented one) and new rows appended
// with one.
func TestOpenUpgradesARunlogTableWithNoSessionColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "box.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE runlog (
		tenant TEXT    NOT NULL,
		run    TEXT    NOT NULL,
		n      INTEGER NOT NULL,
		at_ns  INTEGER NOT NULL,
		kind   TEXT    NOT NULL,
		call   TEXT    NOT NULL,
		tool   TEXT    NOT NULL,
		text   TEXT    NOT NULL,
		reason TEXT    NOT NULL,
		PRIMARY KEY (tenant, run, n)
	) STRICT`); err != nil {
		t.Fatalf("create old-shape table: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO runlog (tenant, run, n, at_ns, kind, call, tool, text, reason) VALUES (?,?,?,?,?,?,?,?,?)`,
		"t1", "wi-old", 1, int64(0), "started", "", "", "", ""); err != nil {
		t.Fatalf("insert old-shape row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open must upgrade the table rather than fail: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.Runs().Append(ctx, tenant.ID("t1"), runlog.Frame{Run: "wi-old", Kind: "tool", Session: "agentref-2"}); err != nil {
		t.Fatalf("append after upgrade: %v", err)
	}
	got, err := s.Runs().Frames(ctx, tenant.ID("t1"), "wi-old")
	if err != nil {
		t.Fatalf("frames: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("frames = %d, want 2 (the pre-upgrade row and the new one)", len(got))
	}
	if got[0].Session != "" {
		t.Errorf("pre-upgrade row's session = %q, want empty — an upgrade must not invent one", got[0].Session)
	}
	if got[1].Session != "agentref-2" {
		t.Errorf("post-upgrade row's session = %q, want agentref-2", got[1].Session)
	}
}
