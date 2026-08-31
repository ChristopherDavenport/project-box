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

// Land pushes a gated ChangeSet: every member fenced up front, then landed in
// the ChangeSet's dependency order, with revert-forward compensation when a
// later member fails.
//
// Three moments matter:
//
//  1. The fencing check, for every member BEFORE anything moves. A stalled or
//     preempted runner's superseded token is refused here, authoritatively,
//     because the leaser judges it — and checking all members first means a
//     dead lease on member three cannot strand members one and two landed.
//  2. The content check, per push. Each push presents the expected old value
//     (--force-with-lease=<branch>:<base>), so a branch moved out-of-band is
//     detected, never overwritten. Git's per-ref CAS is the last line of
//     defence under the lease, not a substitute for it.
//  3. Compensation. There is no atomic commit across remotes, so a failure at
//     member k reverts the k-1 that landed — forward, as new commits whose
//     trees restore the base, in reverse landing order. Published history is
//     never rewritten. A compensation push that itself fails is an incident:
//     the Landing says exactly what is half-done.
//
// Every placement, refusal, and compensation is evidence keyed by commit.
func (l *Lander) Land(ctx context.Context, cs forge.ChangeSet) (forge.Landing, error) {
	if len(cs.Members) == 0 {
		return forge.Landing{}, errors.New("git: empty ChangeSet")
	}
	for _, mem := range cs.Members {
		if _, ok := l.m.Repo(mem.Repo); !ok {
			return forge.Landing{}, fmt.Errorf("git: repo %q not established by Fetch", mem.Repo)
		}
	}
	// Fence every member before anything moves.
	for _, mem := range cs.Members {
		if _, err := l.m.leaser.Renew(ctx, mem.Lease, time.Until(mem.Lease.Expires)); err != nil {
			l.record(ctx, cs, "land-refused", string(mem.Head), fmt.Sprintf(`{"repo":%q,"reason":%q}`, mem.Repo, err.Error()))
			return forge.Landing{Failed: mem.Repo}, fmt.Errorf("git: fencing refused land of %s: %w", mem.Repo, err)
		}
	}

	landing := forge.Landing{ChangeSet: cs.ID}
	for _, mem := range cs.Members {
		if err := l.push(ctx, cs, mem); err != nil {
			landing.Failed = mem.Repo
			l.record(ctx, cs, "land-conflict", string(mem.Head), fmt.Sprintf(`{"repo":%q,"branch":%q,"expected_base":%q}`, mem.Repo, mem.Branch, mem.Base))
			comp, cerr := l.compensate(ctx, cs, landing.Landed)
			landing.Compensated = comp
			landing.Finished = time.Now().UTC()
			if cerr != nil {
				return landing, fmt.Errorf("git: land of %s failed (%v) AND compensation failed: %w — landing is half-done", mem.Repo, err, cerr)
			}
			return landing, fmt.Errorf("git: land of %s refused, %d earlier member(s) reverted forward: %w", mem.Repo, len(comp), err)
		}
		now := time.Now().UTC()
		landing.Landed = append(landing.Landed, forge.Placement{
			Repo: mem.Repo, Branch: mem.Branch, Was: mem.Base, Now: mem.Head, At: now,
		})
		l.record(ctx, cs, "land", string(mem.Head),
			fmt.Sprintf(`{"repo":%q,"branch":%q,"was":%q,"now":%q}`, mem.Repo, mem.Branch, mem.Base, mem.Head))
	}
	landing.Finished = time.Now().UTC()
	return landing, nil
}

// push collects the member's head from its workspace and lands it with the
// expected old value presented.
func (l *Lander) push(ctx context.Context, cs forge.ChangeSet, mem forge.Member) error {
	repo, _ := l.m.Repo(mem.Repo)
	mirror := l.m.mirrorDir(mem.Repo)
	ws := l.m.workDir(cs.Run, mem.Repo)
	if _, err := run(ctx, mirror, "fetch", ws, string(mem.Head)); err != nil {
		return fmt.Errorf("collect %s from workspace: %w", mem.Head, err)
	}
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", mem.Branch, mem.Base)
	if _, err := run(ctx, mirror, "push", repo.Remote, string(mem.Head)+":refs/heads/"+mem.Branch, lease); err != nil {
		return fmt.Errorf("push refused — branch %s moved from expected %s: %w", mem.Branch, mem.Base, err)
	}
	return nil
}

// compensate reverts landed placements forward, in reverse landing order: for
// each, a new commit whose tree is the placement's Was, with the placement's
// Now as parent. History is never rewritten — the change and its undoing both
// stay on the branch, which is what an audit of the failure needs.
func (l *Lander) compensate(ctx context.Context, cs forge.ChangeSet, landed []forge.Placement) ([]forge.Placement, error) {
	var out []forge.Placement
	for i := len(landed) - 1; i >= 0; i-- {
		p := landed[i]
		repo, _ := l.m.Repo(p.Repo)
		mirror := l.m.mirrorDir(p.Repo)
		tree, err := run(ctx, mirror, "rev-parse", string(p.Was)+"^{tree}")
		if err != nil {
			return out, err
		}
		msg := fmt.Sprintf("Revert forward: undo %s of changeset %s\n\nMember %s failed to land; every member must be independently safe,\nso the landed members are restored to their pre-changeset trees.", p.Now, cs.ID, "later")
		rev, err := run(ctx, mirror, "commit-tree", strings.TrimSpace(tree), "-p", string(p.Now), "-m", msg)
		if err != nil {
			return out, err
		}
		revC := forge.Commit(strings.TrimSpace(rev))
		leaseArg := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", p.Branch, p.Now)
		if _, err := run(ctx, mirror, "push", repo.Remote, string(revC)+":refs/heads/"+p.Branch, leaseArg); err != nil {
			return out, fmt.Errorf("compensation push for %s: %w", p.Repo, err)
		}
		now := time.Now().UTC()
		out = append(out, forge.Placement{Repo: p.Repo, Branch: p.Branch, Was: p.Now, Now: revC, At: now})
		l.record(ctx, cs, "land-compensated", string(revC),
			fmt.Sprintf(`{"repo":%q,"branch":%q,"undoes":%q}`, p.Repo, p.Branch, p.Now))
	}
	return out, nil
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
