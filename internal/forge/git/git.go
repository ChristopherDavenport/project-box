// Package git implements the forge's Mirror and Lander over the real git
// binary. Shelling out is deliberate: the failure modes worth testing are
// git's own (a rejected force-with-lease, a missing ref), and a reimplemented
// object store would fail differently from the tool every remote actually
// speaks.
//
// Layout under one root, tenant first as everywhere:
//
//	<root>/mirrors/<tenant>/<repo>.git   bare mirror, shared by runs
//	<root>/work/<tenant>/<run>/<repo>    one workspace per (run, repo)
//
// Mirrors are keyed by tenant even when two tenants name the same remote: a
// mirror is fetched with one tenant's authority, and sharing its objects with
// another tenant would leak exactly the content that authority protected.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/tenant"
)

// Mirror is a tenant-bound implementation of [forge.Mirror].
type Mirror struct {
	root   string
	tenant tenant.ID
	leaser forge.Leaser

	mu     sync.Mutex
	repos  map[string]forge.Repo  // name -> repo, established by Fetch
	leased map[string]forge.Lease // workspace dir -> lease, for Prune
}

// NewMirror returns the mirror set for one tenant. The handle is tenant-bound
// on purpose — there is no unscoped constructor to forget.
func NewMirror(root string, tn tenant.ID, leaser forge.Leaser) (*Mirror, error) {
	if err := tn.Validate(); err != nil {
		return nil, err
	}
	return &Mirror{
		root: root, tenant: tn, leaser: leaser,
		repos: map[string]forge.Repo{}, leased: map[string]forge.Lease{},
	}, nil
}

func (m *Mirror) mirrorDir(repo string) string {
	return filepath.Join(m.root, "mirrors", string(m.tenant), repo+".git")
}

func (m *Mirror) workDir(run, repo string) string {
	return filepath.Join(m.root, "work", string(m.tenant), run, repo)
}

// Fetch clones the mirror on first sight and updates it after, remembering
// the repo so Checkout and the Lander can resolve it from a name.
func (m *Mirror) Fetch(ctx context.Context, repo forge.Repo) error {
	dir := m.mirrorDir(repo.Name)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		if _, err := run(ctx, "", "clone", "--mirror", repo.Remote, dir); err != nil {
			return fmt.Errorf("git: mirror %s: %w", repo.Name, err)
		}
	} else if _, err := run(ctx, dir, "remote", "update", "--prune"); err != nil {
		return fmt.Errorf("git: update mirror %s: %w", repo.Name, err)
	}
	m.mu.Lock()
	m.repos[repo.Name] = repo
	m.mu.Unlock()
	return nil
}

// Repo returns what Fetch established for a name.
func (m *Mirror) Repo(name string) (forge.Repo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.repos[name]
	return r, ok
}

// Resolve turns a ref into the commit it names right now in the mirror.
func (m *Mirror) Resolve(ctx context.Context, repo forge.Repo, ref string) (forge.Commit, error) {
	out, err := run(ctx, m.mirrorDir(repo.Name), "rev-parse", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("git: resolve %s in %s: %w", ref, repo.Name, err)
	}
	return forge.Commit(strings.TrimSpace(out)), nil
}

// Checkout materializes a workspace at a commit. It presents the lease's
// token by renewing it — the leaser is the only authority on whether the
// claim survived, so a runner that stalled past its deadline is refused a
// tree here rather than discovering the problem at land.
func (m *Mirror) Checkout(ctx context.Context, l forge.Lease, at forge.Commit) (forge.Workspace, error) {
	if l.Tenant != m.tenant {
		return forge.Workspace{}, fmt.Errorf("git: lease for tenant %q presented to tenant %q mirror", l.Tenant, m.tenant)
	}
	if _, err := m.leaser.Renew(ctx, l, time.Until(l.Expires)); err != nil {
		return forge.Workspace{}, fmt.Errorf("git: checkout refused: %w", err)
	}
	dir := m.workDir(l.Run, l.Repo)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return forge.Workspace{}, err
	}
	if _, err := run(ctx, "", "clone", "--no-checkout", m.mirrorDir(l.Repo), dir); err != nil {
		return forge.Workspace{}, fmt.Errorf("git: materialize workspace: %w", err)
	}
	if _, err := run(ctx, dir, "checkout", "--detach", string(at)); err != nil {
		os.RemoveAll(dir)
		return forge.Workspace{}, fmt.Errorf("git: checkout %s: %w", at, err)
	}
	m.mu.Lock()
	m.leased[dir] = l
	m.mu.Unlock()
	return forge.Workspace{Repo: l.Repo, Run: l.Run, Dir: dir, Base: at, Lease: l}, nil
}

// Prune removes workspaces whose leases no longer answer. It is advisory disk
// reclamation — the authoritative fencing stays at land — so it asks the
// leaser rather than trusting any local clock.
func (m *Mirror) Prune(ctx context.Context) error {
	m.mu.Lock()
	tracked := make(map[string]forge.Lease, len(m.leased))
	for d, l := range m.leased {
		tracked[d] = l
	}
	m.mu.Unlock()
	var errs []error
	for dir, l := range tracked {
		_, err := m.leaser.Renew(ctx, l, time.Until(l.Expires))
		if errors.Is(err, forge.ErrStaleToken) || errors.Is(err, forge.ErrExpired) {
			if err := os.RemoveAll(dir); err != nil {
				errs = append(errs, err)
				continue
			}
			m.mu.Lock()
			delete(m.leased, dir)
			m.mu.Unlock()
		}
	}
	return errors.Join(errs...)
}

// Lander lands ChangeSets through this mirror set.
type Lander struct {
	m   *Mirror
	log evidence.Log
}

// NewLander wires a lander to one tenant's mirrors and the evidence log.
func NewLander(m *Mirror, log evidence.Log) *Lander {
	return &Lander{m: m, log: log}
}

// Land pushes a gated ChangeSet. Phase 1 scope: a single member; the ordered
// multi-member walk with revert-forward compensation is Phase 4, and landing
// two members without compensation would be worse than refusing.
//
// Two checks guard the push, in order:
//
//  1. The fencing check. The member's lease is presented to the leaser by
//     renewal — this is the moment a stalled or preempted runner's superseded
//     token is refused, and it is authoritative because the leaser, not the
//     runner, judges it.
//  2. The content check. The push presents the expected old value
//     (--force-with-lease=<branch>:<base>), so a concurrent mover of the
//     branch is detected rather than overwritten. Git's per-ref CAS is the
//     last line of defence under the lease, not a substitute for it.
//
// Both the refusal and the landing are evidence, keyed by the head commit —
// the provenance chain's first requirement is that the audit query works.
func (l *Lander) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if len(cs.Members) != 1 {
		return forge.Landing{}, fmt.Errorf("git: phase 1 lander lands exactly one member, got %d", len(cs.Members))
	}
	mem := cs.Members[0]
	repo, ok := l.m.Repo(mem.Repo)
	if !ok {
		return forge.Landing{}, fmt.Errorf("git: repo %q not established by Fetch", mem.Repo)
	}

	if _, err := l.m.leaser.Renew(ctx, mem.Lease, time.Until(mem.Lease.Expires)); err != nil {
		l.record(ctx, cs, "land-refused", string(mem.Head), fmt.Sprintf(`{"reason":%q}`, err.Error()))
		return forge.Landing{Failed: mem.Repo}, fmt.Errorf("git: fencing refused land of %s: %w", mem.Repo, err)
	}

	mirror := l.m.mirrorDir(mem.Repo)
	ws := l.m.workDir(cs.Run, mem.Repo)
	if _, err := run(ctx, mirror, "fetch", ws, string(mem.Head)); err != nil {
		return forge.Landing{}, fmt.Errorf("git: collect %s from workspace: %w", mem.Head, err)
	}
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", mem.Branch, mem.Base)
	if _, err := run(ctx, mirror, "push", repo.Remote, string(mem.Head)+":refs/heads/"+mem.Branch, lease); err != nil {
		l.record(ctx, cs, "land-conflict", string(mem.Head), fmt.Sprintf(`{"branch":%q,"expected_base":%q}`, mem.Branch, mem.Base))
		return forge.Landing{Failed: mem.Repo}, fmt.Errorf("git: push refused — branch %s moved from expected %s: %w", mem.Branch, mem.Base, err)
	}

	now := time.Now().UTC()
	landing := forge.Landing{
		ChangeSet: cs.ID,
		Landed: []forge.Placement{{
			Repo: mem.Repo, Branch: mem.Branch, Was: mem.Base, Now: mem.Head, At: now,
		}},
		Finished: now,
	}
	l.record(ctx, cs, "land", string(mem.Head),
		fmt.Sprintf(`{"repo":%q,"branch":%q,"was":%q,"now":%q}`, mem.Repo, mem.Branch, mem.Base, mem.Head))
	return landing, nil
}

func (l *Lander) record(ctx context.Context, cs forge.ChangeSet, kind, subject, body string) {
	_, err := l.log.Append(ctx, evidence.Record{
		Tenant: cs.Tenant, Scope: "changeset/" + cs.ID,
		Kind: kind, Subject: subject,
		Actor: cs.Members[0].Lease.Runner, Body: []byte(body),
		Trace: "",
	})
	if err != nil {
		// Evidence failure must not be silent, but a landing that happened,
		// happened: surfacing belongs to the caller's error path, and losing
		// the record is precisely what Verify's chain walk will show.
		fmt.Fprintf(os.Stderr, "git: evidence append failed: %v\n", err)
	}
}

// run executes git with a hermetic environment: no user or system config, so
// tests and runners behave identically regardless of whose machine this is.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=project-box", "GIT_AUTHOR_EMAIL=box@invalid",
		"GIT_COMMITTER_NAME=project-box", "GIT_COMMITTER_EMAIL=box@invalid",
	)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}
