package ingress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/store"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// StoreRouter is the store-backed [Router]. It is tenant-bound: the tenant a
// route opens work in is fixed at construction, never derived from the
// delivery — an identifier that must be resolved before it is trusted is one
// an attacker can supply.
type StoreRouter struct {
	Store  store.Store
	Tenant tenant.ID
	Runner string
}

// Open admits one delivery. The ingress ID is derived from the delivery key,
// so idempotency is structural: the same occurrence can only ever name one
// ingress, however many times it is delivered. A short lease over the key
// serializes concurrent deliveries of the same occurrence; the loser re-reads
// and finds the winner's record.
func (s *StoreRouter) Open(ctx context.Context, r Route, d Delivery) (Ingress, bool, error) {
	if r.Requester == "" {
		return Ingress{}, false, errors.New("ingress: route names no requester — work with no principal behind it has no authorization to inherit")
	}
	if r.Box == "" {
		return Ingress{}, false, errors.New("ingress: route names no box")
	}
	if d.Key == "" {
		return Ingress{}, false, errors.New("ingress: delivery carries no idempotency key")
	}

	id := "ing-" + hashKey(r.Name, d.Key)
	if ing, ok, err := s.find(ctx, id); err != nil || ok {
		return ing, false, err
	}

	leaser := s.Store.Leaser(s.Runner)
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: s.Tenant, Repo: "_ingress", Ref: d.Key, Run: id, TTL: 30 * time.Second,
	})
	if err != nil {
		if errors.Is(err, forge.ErrHeld) {
			// A concurrent delivery of the same occurrence is admitting it.
			if ing, ok, ferr := s.find(ctx, id); ferr == nil && ok {
				return ing, false, nil
			}
			return Ingress{}, false, fmt.Errorf("ingress: delivery %s is being admitted concurrently", d.Key)
		}
		return Ingress{}, false, err
	}
	defer leaser.Release(ctx, lease)
	// Re-check under the lease: we may have lost the earlier race.
	if ing, ok, err := s.find(ctx, id); err != nil || ok {
		return ing, false, err
	}

	ing := Ingress{ID: id, Kind: r.Opens, Tenant: s.Tenant, Box: r.Box, Requester: r.Requester,
		Repo: r.Repo, Route: r.Name, Key: d.Key, Depth: d.Depth, Prior: d.Prior, Note: string(d.Payload),
		Title: d.Title}
	body, err := json.Marshal(ing)
	if err != nil {
		return Ingress{}, false, err
	}
	rec, err := s.Store.Log().Append(ctx, evidence.Record{
		Tenant: s.Tenant, Scope: Scope(id),
		Kind: RecordOpen, Subject: "delivery/" + d.Key,
		Actor: r.Requester, Body: body,
	})
	if err != nil {
		return Ingress{}, false, err
	}
	ing.Opened = rec.At
	// The ingress index is what standing operation folds: intake creates an
	// item for every ingress that lacks one, which is the whole "step up
	// ingress and step back" contract.
	if _, err := s.Store.Log().Append(ctx, evidence.Record{
		Tenant: s.Tenant, Scope: "index/ingress", Kind: "indexed", Subject: id,
		Actor: r.Requester, Body: body,
	}); err != nil {
		return Ingress{}, false, err
	}
	return ing, true, nil
}

// find reads an origination back through the one fold, rather than through a
// second reader of the same chain. Two readers is how the head-read bug
// survived: the router's copy and the pump's copy each unmarshalled whatever
// record happened to be newest, and both were wrong in the same way at the
// same time, which reads as agreement.
func (s *StoreRouter) find(ctx context.Context, id string) (Ingress, bool, error) {
	return Load(ctx, s.Store.Log(), s.Tenant, id)
}

func hashKey(route, key string) string {
	sum := sha256.Sum256([]byte(route + "\x00" + key))
	return hex.EncodeToString(sum[:6])
}
