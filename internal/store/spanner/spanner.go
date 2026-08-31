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
