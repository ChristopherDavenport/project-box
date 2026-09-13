// Package sqlite is the local backend: one file, zero services, full
// conformance. It exists so the whole factory model can be proven on a laptop
// before a Spanner instance is provisioned — and so the store seam has two
// implementations from the first commit, keeping either from becoming the
// definition.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, cross-compiles, one static binary

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// schema uses STRICT tables so a type mismatch is an error rather than a
// silently stored value, and enforces append-only on the chain with triggers —
// the property holds in the database, not merely in this package's discipline.
// Lease rows survive release (held=0) so the token counter never resets: a
// token, once superseded, can never come back.
const schema = `
CREATE TABLE IF NOT EXISTS leases (
	tenant     TEXT    NOT NULL,
	repo       TEXT    NOT NULL,
	ref        TEXT    NOT NULL,
	runner     TEXT    NOT NULL,
	run        TEXT    NOT NULL,
	token      INTEGER NOT NULL,
	held       INTEGER NOT NULL,
	expires_ns INTEGER NOT NULL,
	PRIMARY KEY (tenant, repo, ref)
) STRICT, WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS chain (
	tenant       TEXT    NOT NULL,
	scope        TEXT    NOT NULL,
	seq          INTEGER NOT NULL,
	id           TEXT    NOT NULL,
	prev         TEXT    NOT NULL,
	kind         TEXT    NOT NULL,
	subject      TEXT    NOT NULL,
	actor        TEXT    NOT NULL,
	pid          INTEGER NOT NULL DEFAULT 0,
	on_behalf_of TEXT    NOT NULL,
	grant_id     TEXT    NOT NULL,
	body         BLOB    NOT NULL,
	at_ns        INTEGER NOT NULL,
	trace        TEXT    NOT NULL,
	PRIMARY KEY (tenant, scope, seq)
) STRICT;

CREATE INDEX IF NOT EXISTS chain_by_subject ON chain (tenant, subject, at_ns);

CREATE TRIGGER IF NOT EXISTS chain_no_update BEFORE UPDATE ON chain
BEGIN SELECT RAISE(ABORT, 'evidence chain is append-only'); END;

-- The compaction gate: a delete on the chain is admitted only while a row
-- sits here, and only Compact ever seats one — inside the same transaction
-- that verifies the prefix, checks the export receipt, and inserts the
-- summary. Stray deletes still abort, so the append-only property keeps
-- holding in the database, with exactly one named exception. The DROP retires
-- the unconditional trigger this one replaces on databases that predate it.
CREATE TABLE IF NOT EXISTS compaction_gate (
	open INTEGER NOT NULL
) STRICT;

DROP TRIGGER IF EXISTS chain_no_delete;
CREATE TRIGGER IF NOT EXISTS chain_delete_outside_compaction BEFORE DELETE ON chain
WHEN NOT EXISTS (SELECT 1 FROM compaction_gate)
BEGIN SELECT RAISE(ABORT, 'evidence chain is append-only outside compaction'); END;

-- The run log: what an agent did while it ran, one row a frame, in order.
-- Telemetry beside the chain, so no trigger guards it — it is pruned by
-- age, and the chain's append-only property does not extend to it.
CREATE TABLE IF NOT EXISTS runlog (
	tenant  TEXT    NOT NULL,
	run     TEXT    NOT NULL,
	n       INTEGER NOT NULL,
	at_ns   INTEGER NOT NULL,
	kind    TEXT    NOT NULL,
	call    TEXT    NOT NULL,
	tool    TEXT    NOT NULL,
	text    TEXT    NOT NULL,
	reason  TEXT    NOT NULL,
	session TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (tenant, run, n)
) STRICT;

CREATE INDEX IF NOT EXISTS runlog_by_age ON runlog (tenant, at_ns);
`

// Store is a SQLite-backed store.Store.
type Store struct {
	w, r *sql.DB // one writer, many readers: the WAL two-pool pattern
	now  func() time.Time
}

// Option configures Open.
type Option func(*Store)

// WithClock substitutes the store's clock, for expiry tests that should not
// wait in real time. The default is time.Now.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// Open opens (creating if absent) the store at path.
func Open(path string, opts ...Option) (*Store, error) {
	// Pragmas ride the DSN because they are per-connection and database/sql
	// opens connections lazily — an Exec after Open would configure one
	// connection and leave its siblings on defaults.
	base := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	w, err := sql.Open("sqlite", base+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("sqlite: open writer: %w", err)
	}
	w.SetMaxOpenConns(1) // writer-vs-writer contention is resolved here, not by SQLITE_BUSY
	r, err := sql.Open("sqlite", base)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("sqlite: open reader: %w", err)
	}
	s := &Store{w: w, r: r, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	if _, err := w.Exec(schema); err != nil {
		s.Close()
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	if err := addRunlogSessionColumn(w); err != nil {
		s.Close()
		return nil, fmt.Errorf("sqlite: upgrade runlog schema: %w", err)
	}
	if err := addChainPidColumn(w); err != nil {
		s.Close()
		return nil, fmt.Errorf("sqlite: upgrade chain schema: %w", err)
	}
	return s, nil
}

// addRunlogSessionColumn is the repository's first schema evolution.
// CREATE TABLE IF NOT EXISTS above is a no-op against a database that
// already has a runlog table, so a database opened before the session
// column existed would never gain it from schema alone — including the
// live dogfood daemon's. This checks the column's presence with
// pragma table_info and adds it once when absent, so an old database
// upgrades in place rather than needing a separate migration tool.
func addRunlogSessionColumn(w *sql.DB) error {
	rows, err := w.Query(`PRAGMA table_info(runlog)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "session" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if has {
		return nil
	}
	_, err = w.Exec(`ALTER TABLE runlog ADD COLUMN session TEXT NOT NULL DEFAULT ''`)
	return err
}

// addChainPidColumn is the repository's second schema evolution, for the same
// reason as [addRunlogSessionColumn]: CREATE TABLE IF NOT EXISTS is a no-op
// against a chain table that predates pid, including the live dogfood
// daemon's, so this adds the column once when absent rather than requiring a
// migration tool. The default of 0 reads as "not asserted" on every row this
// upgrade touches, which is exactly what those rows are — no old record ever
// asserted a pid.
func addChainPidColumn(w *sql.DB) error {
	rows, err := w.Query(`PRAGMA table_info(chain)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "pid" {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if has {
		return nil
	}
	_, err = w.Exec(`ALTER TABLE chain ADD COLUMN pid INTEGER NOT NULL DEFAULT 0`)
	return err
}

// Close closes both pools.
func (s *Store) Close() error {
	return errors.Join(s.w.Close(), s.r.Close())
}

// Leaser returns the allocator bound to one runner identity.
func (s *Store) Leaser(runner string) forge.Leaser {
	return &leaser{s: s, runner: runner}
}

// Log returns the evidence log.
func (s *Store) Log() evidence.Log { return &log{s: s} }

func (s *Store) Runs() runlog.Log { return &runs{s: s} }

type runs struct{ s *Store }

const frameCols = `run, n, at_ns, kind, call, tool, text, reason, session`

func (r *runs) Append(ctx context.Context, t tenant.ID, f runlog.Frame) (runlog.Frame, error) {
	if err := t.Validate(); err != nil {
		return runlog.Frame{}, err
	}
	if f.Run == "" {
		return runlog.Frame{}, errors.New("sqlite: a run log frame needs a run")
	}
	if f.At.IsZero() {
		f.At = r.s.now()
	}
	f.At = f.At.UTC()
	err := r.s.write(ctx, func(tx *sql.Tx) error {
		var n int64
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(n), 0) FROM runlog WHERE tenant=? AND run=?`,
			string(t), f.Run).Scan(&n)
		if err != nil {
			return err
		}
		f.N = uint64(n) + 1
		_, err = tx.ExecContext(ctx,
			`INSERT INTO runlog (tenant, `+frameCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			string(t), f.Run, int64(f.N), f.At.UnixNano(), f.Kind, f.Call, f.Tool, f.Text, f.Reason, f.Session)
		return err
	})
	if err != nil {
		return runlog.Frame{}, err
	}
	return f, nil
}

func (r *runs) Frames(ctx context.Context, t tenant.ID, run string) ([]runlog.Frame, error) {
	rows, err := r.s.r.QueryContext(ctx,
		`SELECT `+frameCols+` FROM runlog WHERE tenant=? AND run=? ORDER BY n`, string(t), run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runlog.Frame
	for rows.Next() {
		var f runlog.Frame
		var n, at int64
		if err := rows.Scan(&f.Run, &n, &at, &f.Kind, &f.Call, &f.Tool, &f.Text, &f.Reason, &f.Session); err != nil {
			return nil, err
		}
		f.N, f.At = uint64(n), time.Unix(0, at).UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *runs) Prune(ctx context.Context, t tenant.ID, before time.Time) (int64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	var n int64
	err := r.s.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM runlog WHERE tenant=? AND at_ns<?`, string(t), before.UnixNano())
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

type leaser struct {
	s      *Store
	runner string
}

func (l *leaser) Acquire(ctx context.Context, req forge.LeaseRequest) (forge.Lease, error) {
	if err := req.Tenant.Validate(); err != nil {
		return forge.Lease{}, err
	}
	now := l.s.now().UTC()
	expires := now.Add(req.TTL)
	var out forge.Lease
	err := l.s.write(ctx, func(tx *sql.Tx) error {
		var token int64
		var held int64
		var expNS int64
		err := tx.QueryRowContext(ctx,
			`SELECT token, held, expires_ns FROM leases WHERE tenant=? AND repo=? AND ref=?`,
			string(req.Tenant), req.Repo, req.Ref).Scan(&token, &held, &expNS)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			token = 1
			_, err = tx.ExecContext(ctx,
				`INSERT INTO leases (tenant, repo, ref, runner, run, token, held, expires_ns) VALUES (?,?,?,?,?,?,1,?)`,
				string(req.Tenant), req.Repo, req.Ref, l.runner, req.Run, token, expires.UnixNano())
			if err != nil {
				return err
			}
		case err != nil:
			return err
		case held == 1 && now.UnixNano() < expNS:
			return forge.ErrHeld
		default:
			// Released, or expired with no successor yet: supersede. The
			// counter bumps in both paths, so the old token is dead forever.
			token++
			_, err = tx.ExecContext(ctx,
				`UPDATE leases SET token=?, held=1, runner=?, run=?, expires_ns=? WHERE tenant=? AND repo=? AND ref=?`,
				token, l.runner, req.Run, expires.UnixNano(), string(req.Tenant), req.Repo, req.Ref)
			if err != nil {
				return err
			}
		}
		out = forge.Lease{
			Tenant: req.Tenant, Repo: req.Repo, Ref: req.Ref,
			Runner: l.runner, Run: req.Run,
			Token: forge.Token(token), Expires: expires,
		}
		return nil
	})
	return out, err
}

// Inspect reads the outstanding grant. A released row (held=0) reports no
// lease; an unsuperseded row past its deadline still reports one, because the
// caller asked who holds it and not whether it could take it — see the
// interface contract.
func (l *leaser) Inspect(ctx context.Context, t tenant.ID, repo, ref string) (forge.Lease, bool, error) {
	if err := t.Validate(); err != nil {
		return forge.Lease{}, false, err
	}
	var runner, run string
	var token, held, expNS int64
	err := l.s.r.QueryRowContext(ctx,
		`SELECT runner, run, token, held, expires_ns FROM leases WHERE tenant=? AND repo=? AND ref=?`,
		string(t), repo, ref).Scan(&runner, &run, &token, &held, &expNS)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return forge.Lease{}, false, nil
	case err != nil:
		return forge.Lease{}, false, err
	case held == 0:
		return forge.Lease{}, false, nil
	}
	return forge.Lease{
		Tenant: t, Repo: repo, Ref: ref, Runner: runner, Run: run,
		Token: forge.Token(token), Expires: time.Unix(0, expNS).UTC(),
	}, true, nil
}

// Preempt supersedes whatever claim exists; see the interface contract for
// why no authority check lives here.
func (l *leaser) Preempt(ctx context.Context, req forge.LeaseRequest) (forge.Lease, error) {
	if err := req.Tenant.Validate(); err != nil {
		return forge.Lease{}, err
	}
	now := l.s.now().UTC()
	expires := now.Add(req.TTL)
	var out forge.Lease
	err := l.s.write(ctx, func(tx *sql.Tx) error {
		var token int64
		err := tx.QueryRowContext(ctx,
			`SELECT token FROM leases WHERE tenant=? AND repo=? AND ref=?`,
			string(req.Tenant), req.Repo, req.Ref).Scan(&token)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			token = 1
			_, err = tx.ExecContext(ctx,
				`INSERT INTO leases (tenant, repo, ref, runner, run, token, held, expires_ns) VALUES (?,?,?,?,?,?,1,?)`,
				string(req.Tenant), req.Repo, req.Ref, l.runner, req.Run, token, expires.UnixNano())
		case err == nil:
			token++
			_, err = tx.ExecContext(ctx,
				`UPDATE leases SET token=?, held=1, runner=?, run=?, expires_ns=? WHERE tenant=? AND repo=? AND ref=?`,
				token, l.runner, req.Run, expires.UnixNano(), string(req.Tenant), req.Repo, req.Ref)
		}
		if err != nil {
			return err
		}
		out = forge.Lease{
			Tenant: req.Tenant, Repo: req.Repo, Ref: req.Ref,
			Runner: l.runner, Run: req.Run,
			Token: forge.Token(token), Expires: expires,
		}
		return nil
	})
	return out, err
}

func (l *leaser) Renew(ctx context.Context, lease forge.Lease, ttl time.Duration) (forge.Lease, error) {
	now := l.s.now().UTC()
	expires := now.Add(ttl)
	err := l.s.write(ctx, func(tx *sql.Tx) error {
		var token, held, expNS int64
		err := tx.QueryRowContext(ctx,
			`SELECT token, held, expires_ns FROM leases WHERE tenant=? AND repo=? AND ref=?`,
			string(lease.Tenant), lease.Repo, lease.Ref).Scan(&token, &held, &expNS)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return forge.ErrExpired
		case err != nil:
			return err
		case forge.Token(token) != lease.Token:
			return forge.ErrStaleToken
		case held == 0, now.UnixNano() >= expNS:
			return forge.ErrExpired
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE leases SET expires_ns=? WHERE tenant=? AND repo=? AND ref=?`,
			expires.UnixNano(), string(lease.Tenant), lease.Repo, lease.Ref)
		return err
	})
	if err != nil {
		return forge.Lease{}, err
	}
	lease.Expires = expires
	return lease, nil
}

func (l *leaser) Release(ctx context.Context, lease forge.Lease) error {
	return l.s.write(ctx, func(tx *sql.Tx) error {
		// Guarded by token: a superseded holder's release matches nothing and
		// is a no-op by design — the claim it thinks it is dropping is not its
		// claim any more.
		_, err := tx.ExecContext(ctx,
			`UPDATE leases SET held=0 WHERE tenant=? AND repo=? AND ref=? AND token=? AND held=1`,
			string(lease.Tenant), lease.Repo, lease.Ref, int64(lease.Token))
		return err
	})
}

type log struct{ s *Store }

func (g *log) Append(ctx context.Context, r evidence.Record) (evidence.Record, error) {
	if err := r.Tenant.Validate(); err != nil {
		return evidence.Record{}, err
	}
	if r.Scope == "" {
		return evidence.Record{}, errors.New("sqlite: evidence record needs a scope")
	}
	if r.Kind == evidence.KindCompaction {
		return evidence.Record{}, errors.New("sqlite: kind \"compaction\" is reserved — a summary enters the chain only through Compact")
	}
	if r.At.IsZero() {
		r.At = g.s.now()
	}
	r.At = r.At.UTC()
	err := g.s.write(ctx, func(tx *sql.Tx) error {
		var seq int64
		var prev string
		err := tx.QueryRowContext(ctx,
			`SELECT seq, id FROM chain WHERE tenant=? AND scope=? ORDER BY seq DESC LIMIT 1`,
			string(r.Tenant), r.Scope).Scan(&seq, &prev)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r.Seq = uint64(seq) + 1
		r.Prev = prev
		r, err = evidence.Seal(r)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO chain (tenant, scope, seq, id, prev, kind, subject, actor, pid, on_behalf_of, grant_id, body, at_ns, trace)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			string(r.Tenant), r.Scope, int64(r.Seq), r.ID, r.Prev, r.Kind, r.Subject,
			r.Actor, int64(r.Pid), r.OnBehalfOf, r.Grant, r.Body, r.At.UnixNano(), r.Trace)
		return err
	})
	if err != nil {
		return evidence.Record{}, err
	}
	return r, nil
}

const recordCols = `tenant, scope, seq, id, prev, kind, subject, actor, pid, on_behalf_of, grant_id, body, at_ns, trace`

func scanRecord(rows interface{ Scan(...any) error }) (evidence.Record, error) {
	var r evidence.Record
	var tn string
	var seq, atNS, pid int64
	err := rows.Scan(&tn, &r.Scope, &seq, &r.ID, &r.Prev, &r.Kind, &r.Subject,
		&r.Actor, &pid, &r.OnBehalfOf, &r.Grant, &r.Body, &atNS, &r.Trace)
	if err != nil {
		return evidence.Record{}, err
	}
	r.Tenant = tenant.ID(tn)
	r.Seq = uint64(seq)
	r.Pid = int32(pid)
	r.At = time.Unix(0, atNS).UTC()
	return r, nil
}

func (g *log) Head(ctx context.Context, t tenant.ID, scope string) (evidence.Record, error) {
	row := g.s.r.QueryRowContext(ctx,
		`SELECT `+recordCols+` FROM chain WHERE tenant=? AND scope=? ORDER BY seq DESC LIMIT 1`,
		string(t), scope)
	r, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence.Record{}, nil
	}
	return r, err
}

func (g *log) Since(ctx context.Context, t tenant.ID, scope string, seq uint64) iter.Seq2[evidence.Record, error] {
	return g.query(ctx,
		`SELECT `+recordCols+` FROM chain WHERE tenant=? AND scope=? AND seq>=? ORDER BY seq`,
		string(t), scope, int64(seq))
}

func (g *log) BySubject(ctx context.Context, t tenant.ID, subject string) iter.Seq2[evidence.Record, error] {
	return g.query(ctx,
		`SELECT `+recordCols+` FROM chain WHERE tenant=? AND subject=? ORDER BY at_ns, scope, seq`,
		string(t), subject)
}

func (g *log) Verify(ctx context.Context, t tenant.ID, scope string) error {
	return evidence.VerifyScope(ctx, g, t, scope)
}

// Compact replaces the scope's prefix with one summary record; see
// [evidence.Log]. The judgement — prefix verification, receipt checks,
// summary construction — is evidence.Summarize's; this method supplies the
// rows, the clock, and the gated delete, all in one immediate transaction.
func (g *log) Compact(ctx context.Context, t tenant.ID, scope string, through uint64, exp evidence.Export, actor string) (evidence.Record, error) {
	if err := t.Validate(); err != nil {
		return evidence.Record{}, err
	}
	if scope == "" {
		return evidence.Record{}, errors.New("sqlite: compaction needs a scope")
	}
	var out evidence.Record
	err := g.s.write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT `+recordCols+` FROM chain WHERE tenant=? AND scope=? AND seq<=? ORDER BY seq`,
			string(t), scope, int64(through))
		if err != nil {
			return err
		}
		var rs []evidence.Record
		for rows.Next() {
			r, err := scanRecord(rows)
			if err != nil {
				rows.Close()
				return err
			}
			rs = append(rs, r)
		}
		// Closed before the writes below: the writer pool is one connection,
		// and an open cursor would wedge it.
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if n := len(rs); n == 0 || rs[n-1].Seq != through {
			return fmt.Errorf("sqlite: %s/%s has no record at seq %d to compact through", t, scope, through)
		}
		sum, err := evidence.Summarize(rs, exp, actor)
		if err != nil {
			return err
		}
		sum.At = g.s.now().UTC()
		sum, err = evidence.Seal(sum)
		if err != nil {
			return err
		}
		// Seat the gate, prune, clear the gate, take the pruned head's seat —
		// one transaction, so the delete trigger admits exactly this path.
		if _, err := tx.ExecContext(ctx, `INSERT INTO compaction_gate (open) VALUES (1)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM chain WHERE tenant=? AND scope=? AND seq<=?`,
			string(t), scope, int64(through)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_gate`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chain (tenant, scope, seq, id, prev, kind, subject, actor, pid, on_behalf_of, grant_id, body, at_ns, trace)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			string(sum.Tenant), sum.Scope, int64(sum.Seq), sum.ID, sum.Prev, sum.Kind, sum.Subject,
			sum.Actor, int64(sum.Pid), sum.OnBehalfOf, sum.Grant, sum.Body, sum.At.UnixNano(), sum.Trace); err != nil {
			return err
		}
		out = sum
		return nil
	})
	if err != nil {
		return evidence.Record{}, err
	}
	return out, nil
}

func (g *log) query(ctx context.Context, q string, args ...any) iter.Seq2[evidence.Record, error] {
	return func(yield func(evidence.Record, error) bool) {
		rows, err := g.s.r.QueryContext(ctx, q, args...)
		if err != nil {
			yield(evidence.Record{}, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRecord(rows)
			if !yield(r, err) {
				return
			}
			if err != nil {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(evidence.Record{}, err)
		}
	}
}

// write runs fn in an immediate write transaction on the single-connection
// writer pool. Immediate matters: a deferred transaction upgrades its lock at
// the first write and that upgrade is where SQLITE_BUSY bites mid-flight.
func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil) // _txlock=immediate in the writer DSN makes this BEGIN IMMEDIATE
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
