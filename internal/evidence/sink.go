package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// DirSink writes exports to a local directory as newline-delimited sealed
// records.
//
// It proves the SHAPE and not the property, and that distinction is the whole
// reason it says so here rather than in a deployment note. A retention-locked
// bucket is what an auditor tests; a directory on the daemon's disk is a
// directory the daemon can rewrite, so [Export.RetainUntil] stays zero — a
// sink that offers no lock must report none, because a retention date nobody
// enforces is worse evidence than an absent one.
//
// What it does hold up is everything checkable from bytes: the object is the
// exported run in order, each record's ID recomputable from its own
// serialization and every Prev link checkable, so [VerifySeam] proves the
// pruned side offline with no service running. That is what compaction needs
// from a sink in order to be safe, and it is available locally.
type DirSink struct {
	// Dir is where objects land. Created on first export.
	Dir string
	// Log is the chain the export is read from.
	Log Log
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

// Export writes [from..to] of a scope and returns its receipt.
//
// The run is verified before it is written. An export is the durable half of
// a compaction, so exporting a prefix that does not chain would put the
// tampering beyond reach of the check that would have caught it — the same
// refusal Summarize makes on the hot side, made here first because this side
// happens first.
func (d *DirSink) Export(ctx context.Context, t tenant.ID, scope string, from, to uint64) (Export, error) {
	if d.Dir == "" {
		return Export{}, errors.New("evidence: DirSink needs a directory")
	}
	if d.Log == nil {
		return Export{}, errors.New("evidence: DirSink needs a log to export from")
	}
	if from == 0 || from > to {
		return Export{}, fmt.Errorf("evidence: export of impossible range [%d..%d]", from, to)
	}
	var rs []Record
	for rec, err := range d.Log.Since(ctx, t, scope, from) {
		if err != nil {
			return Export{}, err
		}
		if rec.Seq > to {
			break
		}
		rs = append(rs, rec)
	}
	if len(rs) == 0 {
		return Export{}, fmt.Errorf("evidence: %s/%s has nothing in [%d..%d] to export", t, scope, from, to)
	}
	if first, last := rs[0], rs[len(rs)-1]; first.Seq != from || last.Seq != to {
		return Export{}, fmt.Errorf("evidence: %s/%s holds [%d..%d], asked for [%d..%d]",
			t, scope, first.Seq, last.Seq, from, to)
	}
	if err := VerifyRecords(rs); err != nil {
		return Export{}, fmt.Errorf("evidence: refusing to export a run that does not verify: %w", err)
	}

	body, err := ndjson(rs)
	if err != nil {
		return Export{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	if err := os.MkdirAll(d.Dir, 0o755); err != nil {
		return Export{}, err
	}
	path := filepath.Join(d.Dir, digest+".ndjson")
	// Content-addressed, so an object that already exists holds exactly these
	// bytes and rewriting it would be a no-op at best. Refusing to rewrite is
	// the nearest thing a directory has to write-once: a re-export of the same
	// run is idempotent, and anything that would CHANGE an object is not
	// something this sink will do.
	if err := writeOnce(path, body); err != nil {
		return Export{}, err
	}
	return Export{
		Tenant: t, Scope: scope, From: from, To: to,
		Head:   rs[len(rs)-1].ID,
		URI:    "file://" + path,
		Digest: "sha256:" + digest,
		// Deliberately zero. See the type's doc: no lock, so no claim of one.
		RetainUntil: time.Time{},
		At:          d.now(),
	}, nil
}

// Load reads an exported object back — the offline half of [VerifySeam], and
// the thing an auditor handed a directory actually does with it.
func (d *DirSink) Load(exp Export) ([]Record, error) {
	path := filepath.Join(d.Dir, hashOf(exp.Digest)+".ndjson")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != exp.Digest {
		return nil, fmt.Errorf("evidence: %s hashes to %s, receipt says %s", path, got, exp.Digest)
	}
	var rs []Record
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var r Record
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("evidence: %s: %w", path, err)
		}
		rs = append(rs, r)
	}
	return rs, nil
}

func (d *DirSink) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func ndjson(rs []Record) ([]byte, error) {
	var out []byte
	for _, r := range rs {
		line, err := json.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("evidence: encoding %s/%s seq %d: %w", r.Tenant, r.Scope, r.Seq, err)
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out, nil
}

// writeOnce writes the file if it is absent and otherwise leaves it alone.
// Mode 0444, so the ordinary accident — a process or a person editing an
// exported object — needs an explicit act rather than none.
func writeOnce(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if errors.Is(err, os.ErrExist) {
		return nil // content-addressed: the same name is the same bytes
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// hashOf strips the algorithm prefix from a digest.
func hashOf(digest string) string {
	for i := 0; i < len(digest); i++ {
		if digest[i] == ':' {
			return digest[i+1:]
		}
	}
	return digest
}
