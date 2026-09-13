// Package spanner is the deployed backend. It runs identically against the
// emulator and a real instance — the Go client honours SPANNER_EMULATOR_HOST,
// so only the endpoint differs — which is what lets the conformance suite hold
// it to the same properties as SQLite from the first commit, seven phases
// before an instance is provisioned.
//
// The fencing token is the grant's commit timestamp. Grants over one ref
// serialize on that row, and commit timestamps respect external consistency,
// so a later grant necessarily carries a greater token — no counter to
// maintain, and no clock of ours to trust. Expiry likewise compares against
// the database's clock, never the runner's: a runner with a wrong clock can
// harm only itself.
//
// Every key leads with tenant, so a tenant's rows occupy a contiguous range:
// isolation and locality from the same decision.
package spanner

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	gspanner "cloud.google.com/go/spanner"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/runlog"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// DDL is the schema, exported so tests and provisioning apply exactly what
// this package assumes.
var DDL = []string{
	`CREATE TABLE leases (
		tenant     STRING(63)  NOT NULL,
		repo       STRING(MAX) NOT NULL,
		ref        STRING(MAX) NOT NULL,
		runner     STRING(MAX) NOT NULL,
		run        STRING(MAX) NOT NULL,
		held       BOOL        NOT NULL,
		granted_at TIMESTAMP   NOT NULL OPTIONS (allow_commit_timestamp = true),
		expires_at TIMESTAMP   NOT NULL,
	) PRIMARY KEY (tenant, repo, ref)`,
	`CREATE TABLE chain (
		tenant       STRING(63)  NOT NULL,
		scope        STRING(MAX) NOT NULL,
		seq          INT64       NOT NULL,
		id           STRING(64)  NOT NULL,
		prev         STRING(64)  NOT NULL,
		kind         STRING(MAX) NOT NULL,
		subject      STRING(MAX) NOT NULL,
		actor        STRING(MAX) NOT NULL,
		on_behalf_of STRING(MAX) NOT NULL,
		grant_id     STRING(MAX) NOT NULL,
		body         BYTES(MAX)  NOT NULL,
		at_ns        INT64       NOT NULL,
		trace        STRING(MAX) NOT NULL,
	) PRIMARY KEY (tenant, scope, seq)`,
	`CREATE INDEX chain_by_subject ON chain (tenant, subject, at_ns)`,
	// session is the DDL for a fresh or emulator database only. Altering an
	// existing production Spanner database to add this column is an
	// operator's DDL action against the live instance, out of this
	// package's scope.
	`CREATE TABLE runlog (
		tenant  STRING(63)  NOT NULL,
		run     STRING(MAX) NOT NULL,
		n       INT64       NOT NULL,
		at_ns   INT64       NOT NULL,
		kind    STRING(MAX) NOT NULL,
		call    STRING(MAX) NOT NULL,
		tool    STRING(MAX) NOT NULL,
		text    STRING(MAX) NOT NULL,
		reason  STRING(MAX) NOT NULL,
		session STRING(MAX) NOT NULL,
	) PRIMARY KEY (tenant, run, n)`,
	`CREATE INDEX runlog_by_age ON runlog (tenant, at_ns)`,
}

// Store is a Spanner-backed store.Store.
type Store struct {
	c *gspanner.Client
}

// Open connects to the database named by its full path,
// projects/P/instances/I/databases/D. With SPANNER_EMULATOR_HOST set the
// client routes to the emulator with no credentials; unset, it is production.
func Open(ctx context.Context, database string) (*Store, error) {
	c, err := gspanner.NewClient(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("spanner: open %s: %w", database, err)
	}
	return &Store{c: c}, nil
}

// Close releases the client.
func (s *Store) Close() error {
	s.c.Close()
	return nil
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
		return runlog.Frame{}, errors.New("spanner: a run log frame needs a run")
	}
	if f.At.IsZero() {
		f.At = time.Now()
	}
	f.At = f.At.UTC()
	_, err := r.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		it := txn.Query(ctx, gspanner.Statement{
			SQL:    `SELECT COALESCE(MAX(n), 0) FROM runlog WHERE tenant=@t AND run=@r`,
			Params: map[string]any{"t": string(t), "r": f.Run},
		})
		defer it.Stop()
		row, err := it.Next()
		if err != nil {
			return err
		}
		var n int64
		if err := row.Columns(&n); err != nil {
			return err
		}
		f.N = uint64(n) + 1
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.Insert("runlog",
			[]string{"tenant", "run", "n", "at_ns", "kind", "call", "tool", "text", "reason", "session"},
			[]any{string(t), f.Run, int64(f.N), f.At.UnixNano(), f.Kind, f.Call, f.Tool, f.Text, f.Reason, f.Session},
		)})
	})
	if err != nil {
		return runlog.Frame{}, err
	}
	return f, nil
}

func (r *runs) Frames(ctx context.Context, t tenant.ID, run string) ([]runlog.Frame, error) {
	it := r.s.c.Single().Query(ctx, gspanner.Statement{
		SQL:    `SELECT ` + frameCols + ` FROM runlog WHERE tenant=@t AND run=@r ORDER BY n`,
		Params: map[string]any{"t": string(t), "r": run},
	})
	defer it.Stop()
	var out []runlog.Frame
	for {
		row, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		var f runlog.Frame
		var n, at int64
		if err := row.Columns(&f.Run, &n, &at, &f.Kind, &f.Call, &f.Tool, &f.Text, &f.Reason, &f.Session); err != nil {
			return nil, err
		}
		f.N, f.At = uint64(n), time.Unix(0, at).UTC()
		out = append(out, f)
	}
}

func (r *runs) Prune(ctx context.Context, t tenant.ID, before time.Time) (int64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	var n int64
	_, err := r.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		var err error
		n, err = txn.Update(ctx, gspanner.Statement{
			SQL:    `DELETE FROM runlog WHERE tenant=@t AND at_ns < @b`,
			Params: map[string]any{"t": string(t), "b": before.UnixNano()},
		})
		return err
	})
	return n, err
}

type leaser struct {
	s      *Store
	runner string
}

var leaseCols = []string{"held", "granted_at", "expires_at"}

// dbNow reads the database's clock inside the transaction, so expiry is judged
// by the store and a stalled runner's local clock buys it nothing.
func dbNow(ctx context.Context, txn *gspanner.ReadWriteTransaction) (time.Time, error) {
	it := txn.Query(ctx, gspanner.Statement{SQL: "SELECT CURRENT_TIMESTAMP()"})
	defer it.Stop()
	row, err := it.Next()
	if err != nil {
		return time.Time{}, err
	}
	var now time.Time
	return now, row.Columns(&now)
}

func (l *leaser) Acquire(ctx context.Context, req forge.LeaseRequest) (forge.Lease, error) {
	if err := req.Tenant.Validate(); err != nil {
		return forge.Lease{}, err
	}
	var expires time.Time
	resp, err := l.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		now, err := dbNow(ctx, txn)
		if err != nil {
			return err
		}
		row, err := txn.ReadRow(ctx, "leases", gspanner.Key{string(req.Tenant), req.Repo, req.Ref}, leaseCols)
		switch {
		case gspanner.ErrCode(err) == codes.NotFound:
			// first grant over this ref
		case err != nil:
			return err
		default:
			var held bool
			var granted, exp time.Time
			if err := row.Columns(&held, &granted, &exp); err != nil {
				return err
			}
			if held && now.Before(exp) {
				return forge.ErrHeld
			}
			// Released, or expired with no successor: supersede. The new
			// grant's commit timestamp is necessarily greater than the old.
		}
		expires = now.Add(req.TTL)
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.InsertOrUpdate("leases",
			[]string{"tenant", "repo", "ref", "runner", "run", "held", "granted_at", "expires_at"},
			[]any{string(req.Tenant), req.Repo, req.Ref, l.runner, req.Run, true, gspanner.CommitTimestamp, expires},
		)})
	})
	if err != nil {
		return forge.Lease{}, err
	}
	return forge.Lease{
		Tenant: req.Tenant, Repo: req.Repo, Ref: req.Ref,
		Runner: l.runner, Run: req.Run,
		Token: forge.Token(resp.UnixNano()), Expires: expires,
	}, nil
}

// Inspect reads the outstanding grant. A single-use read-only transaction is
// enough: this is a report, not a decision, and the caller that must act
// acquires. A released row reports no lease; an unsuperseded row past its
// deadline still reports one — see the interface contract.
func (l *leaser) Inspect(ctx context.Context, t tenant.ID, repo, ref string) (forge.Lease, bool, error) {
	if err := t.Validate(); err != nil {
		return forge.Lease{}, false, err
	}
	row, err := l.s.c.Single().ReadRow(ctx, "leases", gspanner.Key{string(t), repo, ref},
		[]string{"runner", "run", "held", "granted_at", "expires_at"})
	if gspanner.ErrCode(err) == codes.NotFound {
		return forge.Lease{}, false, nil
	}
	if err != nil {
		return forge.Lease{}, false, err
	}
	var runner, run string
	var held bool
	var granted, expires time.Time
	if err := row.Columns(&runner, &run, &held, &granted, &expires); err != nil {
		return forge.Lease{}, false, err
	}
	if !held {
		return forge.Lease{}, false, nil
	}
	return forge.Lease{
		Tenant: t, Repo: repo, Ref: ref, Runner: runner, Run: run,
		Token: forge.Token(granted.UnixNano()), Expires: expires,
	}, true, nil
}

// Preempt supersedes whatever claim exists; the new grant's commit timestamp
// is necessarily greater than the displaced one's, so the fence follows from
// the same property Acquire relies on.
func (l *leaser) Preempt(ctx context.Context, req forge.LeaseRequest) (forge.Lease, error) {
	if err := req.Tenant.Validate(); err != nil {
		return forge.Lease{}, err
	}
	var expires time.Time
	resp, err := l.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		now, err := dbNow(ctx, txn)
		if err != nil {
			return err
		}
		expires = now.Add(req.TTL)
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.InsertOrUpdate("leases",
			[]string{"tenant", "repo", "ref", "runner", "run", "held", "granted_at", "expires_at"},
			[]any{string(req.Tenant), req.Repo, req.Ref, l.runner, req.Run, true, gspanner.CommitTimestamp, expires},
		)})
	})
	if err != nil {
		return forge.Lease{}, err
	}
	return forge.Lease{
		Tenant: req.Tenant, Repo: req.Repo, Ref: req.Ref,
		Runner: l.runner, Run: req.Run,
		Token: forge.Token(resp.UnixNano()), Expires: expires,
	}, nil
}

func (l *leaser) Renew(ctx context.Context, lease forge.Lease, ttl time.Duration) (forge.Lease, error) {
	var expires time.Time
	_, err := l.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		now, err := dbNow(ctx, txn)
		if err != nil {
			return err
		}
		row, err := txn.ReadRow(ctx, "leases", gspanner.Key{string(lease.Tenant), lease.Repo, lease.Ref}, leaseCols)
		if gspanner.ErrCode(err) == codes.NotFound {
			return forge.ErrExpired
		}
		if err != nil {
			return err
		}
		var held bool
		var granted, exp time.Time
		if err := row.Columns(&held, &granted, &exp); err != nil {
			return err
		}
		if forge.Token(granted.UnixNano()) != lease.Token {
			return forge.ErrStaleToken
		}
		if !held || !now.Before(exp) {
			return forge.ErrExpired
		}
		expires = now.Add(ttl)
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.Update("leases",
			[]string{"tenant", "repo", "ref", "expires_at"},
			[]any{string(lease.Tenant), lease.Repo, lease.Ref, expires},
		)})
	})
	if err != nil {
		return forge.Lease{}, err
	}
	lease.Expires = expires
	return lease, nil
}

func (l *leaser) Release(ctx context.Context, lease forge.Lease) error {
	_, err := l.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		row, err := txn.ReadRow(ctx, "leases", gspanner.Key{string(lease.Tenant), lease.Repo, lease.Ref}, leaseCols)
		if gspanner.ErrCode(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var held bool
		var granted, exp time.Time
		if err := row.Columns(&held, &granted, &exp); err != nil {
			return err
		}
		// A superseded holder's release matches nothing: the claim it thinks
		// it is dropping is not its claim any more.
		if !held || forge.Token(granted.UnixNano()) != lease.Token {
			return nil
		}
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.Update("leases",
			[]string{"tenant", "repo", "ref", "held"},
			[]any{string(lease.Tenant), lease.Repo, lease.Ref, false},
		)})
	})
	return err
}

type log struct{ s *Store }

func (g *log) Append(ctx context.Context, r evidence.Record) (evidence.Record, error) {
	if err := r.Tenant.Validate(); err != nil {
		return evidence.Record{}, err
	}
	if r.Scope == "" {
		return evidence.Record{}, errors.New("spanner: evidence record needs a scope")
	}
	if r.Kind == evidence.KindCompaction {
		return evidence.Record{}, errors.New("spanner: kind \"compaction\" is reserved — a summary enters the chain only through Compact")
	}
	if r.Body == nil {
		r.Body = []byte{}
	}
	var sealed evidence.Record
	_, err := g.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		rr := r
		if rr.At.IsZero() {
			now, err := dbNow(ctx, txn)
			if err != nil {
				return err
			}
			rr.At = now
		}
		rr.At = rr.At.UTC()
		it := txn.Query(ctx, gspanner.Statement{
			SQL:    `SELECT seq, id FROM chain WHERE tenant=@t AND scope=@s ORDER BY seq DESC LIMIT 1`,
			Params: map[string]any{"t": string(rr.Tenant), "s": rr.Scope},
		})
		var seq int64
		var prev string
		row, err := it.Next()
		if err == nil {
			err = row.Columns(&seq, &prev)
		} else if errors.Is(err, iterator.Done) {
			err = nil
		}
		it.Stop()
		if err != nil {
			return err
		}
		rr.Seq = uint64(seq) + 1
		rr.Prev = prev
		rr, err = evidence.Seal(rr)
		if err != nil {
			return err
		}
		sealed = rr
		return txn.BufferWrite([]*gspanner.Mutation{gspanner.Insert("chain",
			[]string{"tenant", "scope", "seq", "id", "prev", "kind", "subject", "actor", "on_behalf_of", "grant_id", "body", "at_ns", "trace"},
			[]any{string(rr.Tenant), rr.Scope, int64(rr.Seq), rr.ID, rr.Prev, rr.Kind, rr.Subject,
				rr.Actor, rr.OnBehalfOf, rr.Grant, rr.Body, rr.At.UnixNano(), rr.Trace},
		)})
	})
	if err != nil {
		return evidence.Record{}, err
	}
	return sealed, nil
}

const recordCols = `tenant, scope, seq, id, prev, kind, subject, actor, on_behalf_of, grant_id, body, at_ns, trace`

func scanRecord(row *gspanner.Row) (evidence.Record, error) {
	var r evidence.Record
	var tn string
	var seq, atNS int64
	err := row.Columns(&tn, &r.Scope, &seq, &r.ID, &r.Prev, &r.Kind, &r.Subject,
		&r.Actor, &r.OnBehalfOf, &r.Grant, &r.Body, &atNS, &r.Trace)
	if err != nil {
		return evidence.Record{}, err
	}
	r.Tenant = tenant.ID(tn)
	r.Seq = uint64(seq)
	r.At = time.Unix(0, atNS).UTC()
	return r, nil
}

func (g *log) Head(ctx context.Context, t tenant.ID, scope string) (evidence.Record, error) {
	it := g.s.c.Single().Query(ctx, gspanner.Statement{
		SQL:    `SELECT ` + recordCols + ` FROM chain WHERE tenant=@t AND scope=@s ORDER BY seq DESC LIMIT 1`,
		Params: map[string]any{"t": string(t), "s": scope},
	})
	defer it.Stop()
	row, err := it.Next()
	if errors.Is(err, iterator.Done) {
		return evidence.Record{}, nil
	}
	if err != nil {
		return evidence.Record{}, err
	}
	return scanRecord(row)
}

func (g *log) Since(ctx context.Context, t tenant.ID, scope string, seq uint64) iter.Seq2[evidence.Record, error] {
	return g.query(ctx, gspanner.Statement{
		SQL:    `SELECT ` + recordCols + ` FROM chain WHERE tenant=@t AND scope=@s AND seq>=@q ORDER BY seq`,
		Params: map[string]any{"t": string(t), "s": scope, "q": int64(seq)},
	})
}

func (g *log) BySubject(ctx context.Context, t tenant.ID, subject string) iter.Seq2[evidence.Record, error] {
	return g.query(ctx, gspanner.Statement{
		SQL:    `SELECT ` + recordCols + ` FROM chain WHERE tenant=@t AND subject=@u ORDER BY at_ns, scope, seq`,
		Params: map[string]any{"t": string(t), "u": subject},
	})
}

func (g *log) Verify(ctx context.Context, t tenant.ID, scope string) error {
	return evidence.VerifyScope(ctx, g, t, scope)
}

// Compact replaces the scope's prefix with one summary record; see
// [evidence.Log]. The judgement — prefix verification, receipt checks,
// summary construction — is evidence.Summarize's; this method supplies the
// rows, the database's clock, and the prune, in one transaction. The rows
// strictly below the boundary are deleted by key range and the summary takes
// the boundary row's seat by replacing every column, so no key is both
// deleted and written in one commit.
func (g *log) Compact(ctx context.Context, t tenant.ID, scope string, through uint64, exp evidence.Export, actor string) (evidence.Record, error) {
	if err := t.Validate(); err != nil {
		return evidence.Record{}, err
	}
	if scope == "" {
		return evidence.Record{}, errors.New("spanner: compaction needs a scope")
	}
	var out evidence.Record
	_, err := g.s.c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *gspanner.ReadWriteTransaction) error {
		it := txn.Query(ctx, gspanner.Statement{
			SQL:    `SELECT ` + recordCols + ` FROM chain WHERE tenant=@t AND scope=@s AND seq<=@q ORDER BY seq`,
			Params: map[string]any{"t": string(t), "s": scope, "q": int64(through)},
		})
		var rs []evidence.Record
		err := func() error {
			defer it.Stop()
			for {
				row, err := it.Next()
				if errors.Is(err, iterator.Done) {
					return nil
				}
				if err != nil {
					return err
				}
				r, err := scanRecord(row)
				if err != nil {
					return err
				}
				rs = append(rs, r)
			}
		}()
		if err != nil {
			return err
		}
		if n := len(rs); n == 0 || rs[n-1].Seq != through {
			return fmt.Errorf("spanner: %s/%s has no record at seq %d to compact through", t, scope, through)
		}
		sum, err := evidence.Summarize(rs, exp, actor)
		if err != nil {
			return err
		}
		now, err := dbNow(ctx, txn)
		if err != nil {
			return err
		}
		sum.At = now.UTC()
		sum, err = evidence.Seal(sum)
		if err != nil {
			return err
		}
		out = sum
		return txn.BufferWrite([]*gspanner.Mutation{
			gspanner.Delete("chain", gspanner.KeyRange{
				Start: gspanner.Key{string(t), scope, 0},
				End:   gspanner.Key{string(t), scope, int64(through)},
				Kind:  gspanner.ClosedOpen,
			}),
			gspanner.InsertOrUpdate("chain",
				[]string{"tenant", "scope", "seq", "id", "prev", "kind", "subject", "actor", "on_behalf_of", "grant_id", "body", "at_ns", "trace"},
				[]any{string(sum.Tenant), sum.Scope, int64(sum.Seq), sum.ID, sum.Prev, sum.Kind, sum.Subject,
					sum.Actor, sum.OnBehalfOf, sum.Grant, sum.Body, sum.At.UnixNano(), sum.Trace},
			),
		})
	})
	if err != nil {
		return evidence.Record{}, err
	}
	return out, nil
}

func (g *log) query(ctx context.Context, stmt gspanner.Statement) iter.Seq2[evidence.Record, error] {
	return func(yield func(evidence.Record, error) bool) {
		it := g.s.c.Single().Query(ctx, stmt)
		defer it.Stop()
		for {
			row, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return
			}
			if err != nil {
				yield(evidence.Record{}, err)
				return
			}
			r, err := scanRecord(row)
			if !yield(r, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}
