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
	"strconv"
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

	mu       sync.Mutex
	repos    map[string]forge.Repo    // name -> repo, established by Fetch
	leased   map[string]forge.Lease   // workspace dir -> lease, for Prune
	held     map[string]bool          // workspace dir -> exempt from Prune, for Adopt
	fetching map[string]*sync.RWMutex // repo name -> lock excluding mirror writers from mirror readers
}

// NewMirror returns the mirror set for one tenant. The handle is tenant-bound
// on purpose — there is no unscoped constructor to forget.
func NewMirror(root string, tn tenant.ID, leaser forge.Leaser) (*Mirror, error) {
	if err := tn.Validate(); err != nil {
		return nil, err
	}
	// The root must be absolute: mirror-relative git commands (the lander's
	// fetch-from-workspace) resolve paths from the MIRROR's directory, so a
	// relative root points somewhere that exists only by coincidence. Found
	// by the first live daemon smoke, hidden by every unit test's absolute
	// TempDir.
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Mirror{
		root: root, tenant: tn, leaser: leaser,
		repos: map[string]forge.Repo{}, leased: map[string]forge.Lease{},
		held: map[string]bool{}, fetching: map[string]*sync.RWMutex{},
	}, nil
}

func (m *Mirror) mirrorDir(repo string) string {
	return filepath.Join(m.root, "mirrors", string(m.tenant), repo+".git")
}

func (m *Mirror) workDir(run, repo string) string {
	return filepath.Join(m.root, "work", string(m.tenant), run, repo)
}

// fetchLock returns the lock excluding one repo's mirror writers from its
// mirror readers.
func (m *Mirror) fetchLock(repo string) *sync.RWMutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk, ok := m.fetching[repo]
	if !ok {
		lk = &sync.RWMutex{}
		m.fetching[repo] = lk
	}
	return lk
}

// Fetch clones the mirror on first sight and updates it after, remembering
// the repo so Checkout and the Lander can resolve it from a name.
//
// Updates of one mirror exclude every reader of it. `git remote update`
// racing itself in the same directory fails to lock the refs the other run
// just moved — "cannot lock ref 'refs/heads/main': is at X but expected Y" —
// and the loser reports infrastructure trouble for what is only two runs
// sharing a directory; a concurrent reader (Resolve, Checkout, ChangedPaths,
// MaterializeContext) fares no better, since `remote update` can move or
// prune a ref mid-read. This used to be free: the branch lease was held
// across the whole run, so one run touched the mirror at a time. Narrowing
// that lease made the sharing visible, and the lock is where it belongs — a
// mirror root is one daemon's own working area, so the contention is within
// this process.
//
// Fetch is not the mirror's only writer: the Lander's push collects a
// member's head with `git fetch <workspace>` run in the mirror directory,
// and its compensate mints a revert-forward commit with `rev-parse` and
// `commit-tree` against the same directory — both mutate it exactly as an
// update does, and both take this same lock's write side for that
// subprocess alone.
//
// The lock is a RWMutex for exactly this reason: every mirror-dir writer
// holds the write side for its own subprocess, and readers hold the read
// side only for the duration of theirs — never across a whole build or
// agent session, and never across a network call (a push or a fetch from a
// real remote) once the mirror-dir subprocess it depends on has finished.
// Serializing readers behind writers for longer than that is the regression
// #18 removed (holding the branch lease, and so the mirror, across a whole
// run); this lock must not bring it back.
func (m *Mirror) Fetch(ctx context.Context, repo forge.Repo) error {
	lk := m.fetchLock(repo.Name)
	lk.Lock()
	defer lk.Unlock()
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
//
// It holds the mirror's read lock for the rev-parse alone, so a Fetch moving
// refs concurrently cannot hand this call a ref mid-move.
func (m *Mirror) Resolve(ctx context.Context, repo forge.Repo, ref string) (forge.Commit, error) {
	lk := m.fetchLock(repo.Name)
	lk.RLock()
	out, err := run(ctx, m.mirrorDir(repo.Name), "rev-parse", ref+"^{commit}")
	lk.RUnlock()
	if err != nil {
		return "", fmt.Errorf("git: resolve %s in %s: %w", ref, repo.Name, err)
	}
	return forge.Commit(strings.TrimSpace(out)), nil
}

// ChangedPaths reports the repo-relative paths that differ between two
// commits, via `git diff --name-only`. It errors — never returning an empty
// list — when either commit is unknown to the mirror or git fails: an
// unanswered "what changed" must not be mistaken for "nothing changed" (this
// repo's unknown-never-reads-as-zero rule). It does not fetch: it reads what
// the mirror already has, and callers fetch first.
func (m *Mirror) ChangedPaths(ctx context.Context, repo forge.Repo, from, to forge.Commit) ([]string, error) {
	lk := m.fetchLock(repo.Name)
	lk.RLock()
	out, err := run(ctx, m.mirrorDir(repo.Name), "diff", "--name-only", string(from), string(to))
	lk.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("git: changed paths %s..%s in %s: %w", from, to, repo.Name, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
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
	// A re-claimed run re-materializes: a leftover tree from an earlier
	// attempt of the SAME run (a promotion retried after its approval
	// arrived, say) is stale, and the renewed lease above is the authority
	// that says so. Its context set goes with it — under a confining wrapper
	// that directory is mounted, so material the new attempt was not granted
	// would still be material it can read.
	if err := os.RemoveAll(dir); err != nil {
		return forge.Workspace{}, err
	}
	if err := RemoveContext(forge.ContextDir(dir)); err != nil {
		return forge.Workspace{}, err
	}
	if err := RemoveContext(forge.SkillsDir(dir)); err != nil {
		return forge.Workspace{}, err
	}
	// The clone reads the mirror directory; the read lock covers only this
	// subprocess, not the checkout below, which touches only the new
	// workspace.
	lk := m.fetchLock(l.Repo)
	lk.RLock()
	_, err := run(ctx, "", "clone", "--no-checkout", m.mirrorDir(l.Repo), dir)
	lk.RUnlock()
	if err != nil {
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

// Adopt re-attaches to the workspace an earlier attempt of the same run left
// behind, at the commit that attempt built, instead of materializing a new
// one. It reports false when there is no such tree to take — a different
// runner, a reclaimed disk, a head that moved — and the caller must then build
// again rather than assume.
//
// Checkout destroys a leftover tree on purpose, and that is the right default;
// this is the one case where keeping it is right, so it is a separate call.
// Staying is then a decision at the call site rather than a silent change of
// meaning in Checkout.
func (m *Mirror) Adopt(ctx context.Context, l forge.Lease, at, base forge.Commit) (forge.Workspace, bool, error) {
	if l.Tenant != m.tenant {
		return forge.Workspace{}, false, fmt.Errorf("git: lease for tenant %q presented to tenant %q mirror", l.Tenant, m.tenant)
	}
	// The same fencing rule Checkout applies: the leaser is the only authority
	// on whether the claim survived, and an adopted tree is no less a tree.
	if _, err := m.leaser.Renew(ctx, l, time.Until(l.Expires)); err != nil {
		return forge.Workspace{}, false, fmt.Errorf("git: adopt refused: %w", err)
	}
	dir := m.workDir(l.Run, l.Repo)
	if _, err := os.Stat(dir); err != nil {
		return forge.Workspace{}, false, nil
	}
	// A tree that does not resolve, or resolves elsewhere, is not the one the
	// caller means. Rebuilding is always sound, so every doubt reads as false
	// rather than as an error the caller has to interpret.
	head, err := run(ctx, dir, "rev-parse", "HEAD")
	if err != nil || forge.Commit(strings.TrimSpace(head)) != at {
		return forge.Workspace{}, false, nil
	}
	m.mu.Lock()
	m.leased[dir] = l
	m.mu.Unlock()
	return forge.Workspace{Repo: l.Repo, Run: l.Run, Dir: dir, Base: base, Lease: l}, true, nil
}

// HeldWorkspace names the tree a held run left behind, without leasing it,
// renewing anything, or touching a byte of it. Reports false when there is no
// such tree.
//
// It exists for checks that must change nothing. Acquiring a lease is itself
// a recorded act — grant and record commit together, by design — so a caller
// that only wants to ASK a question cannot go through Checkout or Adopt
// without leaving a trail of having done something.
func (m *Mirror) HeldWorkspace(run, repo string, base forge.Commit) (forge.Workspace, bool) {
	dir := m.workDir(run, repo)
	if _, err := os.Stat(dir); err != nil {
		return forge.Workspace{}, false
	}
	return forge.Workspace{Repo: repo, Run: run, Dir: dir, Base: base}, true
}

// Hold exempts a run's workspace from Prune while the run is between leases.
// A run waiting on a gate releases its lease and reacquires it on the next
// tick; in that gap the tree's lease no longer answers, and Prune would
// reclaim exactly the tree Adopt is there to reuse.
func (m *Mirror) Hold(run, repo string) {
	m.mu.Lock()
	m.held[m.workDir(run, repo)] = true
	m.mu.Unlock()
}

// Discard lifts a hold and returns the tree to Prune's care. Every settle
// calls it: a held workspace nobody will come back for is a disk leak with no
// clock on it.
func (m *Mirror) Discard(run, repo string) {
	m.mu.Lock()
	delete(m.held, m.workDir(run, repo))
	m.mu.Unlock()
}

// Prune removes workspaces whose leases no longer answer. It is advisory disk
// reclamation — the authoritative fencing stays at land — so it asks the
// leaser rather than trusting any local clock.
func (m *Mirror) Prune(ctx context.Context) error {
	m.mu.Lock()
	tracked := make(map[string]forge.Lease, len(m.leased))
	for d, l := range m.leased {
		if m.held[d] {
			continue // a live resume point, between leases by design
		}
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
			// The context set is a sibling, so RemoveAll above never saw it —
			// and it is the half that holds whole repositories. The skill
			// plugin is a sibling for the same reason and sealed the same way.
			if err := RemoveContext(forge.ContextDir(dir)); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := RemoveContext(forge.SkillsDir(dir)); err != nil {
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
//
// Collecting the head is a mirror write — it fetches new objects and refs
// into the mirror directory, the same directory Resolve/Checkout/Adopt/
// ChangedPaths/MaterializeContext read — so it holds the mirror's write lock
// for that subprocess alone. The push itself is a network call against the
// remote, not the mirror directory, and stays outside the lock: holding it
// across the push would serialize landing behind every reader for no reason
// tied to the mirror.
func (l *Lander) push(ctx context.Context, cs forge.ChangeSet, mem forge.Member) error {
	repo, _ := l.m.Repo(mem.Repo)
	mirror := l.m.mirrorDir(mem.Repo)
	ws := l.m.workDir(cs.Run, mem.Repo)
	lk := l.m.fetchLock(mem.Repo)
	lk.Lock()
	_, err := run(ctx, mirror, "fetch", ws, string(mem.Head))
	lk.Unlock()
	if err != nil {
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
		// rev-parse and commit-tree both read and write the mirror's object
		// store directly, so both hold the write lock — the same one push
		// takes — for exactly that pair of subprocesses. The push below is
		// the network call and stays outside it.
		lk := l.m.fetchLock(p.Repo)
		lk.Lock()
		tree, err := run(ctx, mirror, "rev-parse", string(p.Was)+"^{tree}")
		if err != nil {
			lk.Unlock()
			return out, err
		}
		msg := fmt.Sprintf("Revert forward: undo %s of changeset %s\n\nMember %s failed to land; every member must be independently safe,\nso the landed members are restored to their pre-changeset trees.", p.Now, cs.ID, "later")
		rev, err := run(ctx, mirror, "commit-tree", strings.TrimSpace(tree), "-p", string(p.Now), "-m", msg)
		lk.Unlock()
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

// RevParse resolves a rev in a working tree — the one plumbing call builders
// outside this package need, exported so they need not shell git themselves.
func RevParse(ctx context.Context, dir, rev string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", rev)
	return strings.TrimSpace(out), err
}

// Dirty reports whether a working tree has uncommitted changes.
func Dirty(ctx context.Context, dir string) (bool, error) {
	out, err := run(ctx, dir, "status", "--porcelain")
	return strings.TrimSpace(out) != "", err
}

// CommitMessage returns rev's full commit message (subject and body,
// trailers included) — what the loop reads before its gates run to refuse a
// commit that still carries the ACP adapter's placeholder shape (#130). An
// unknown rev is an error, never an empty message: unknown never reads as
// zero.
func CommitMessage(ctx context.Context, dir, rev string) (string, error) {
	out, err := run(ctx, dir, "show", "-s", "--format=%B", rev)
	return out, err
}

// ErrRebaseConflict reports that the workspace's commits do not replay onto
// the new base without a judgement call. The rebase is aborted and the tree
// left where it was: a conflict resolution nobody reviewed is not something
// to land, so the caller builds again instead.
var ErrRebaseConflict = errors.New("git: rebase conflicted")

// Rebase replays the workspace's commits from oldBase onto newBase, leaving
// the tree detached at the new head.
//
// This is what makes a narrow branch lease affordable. Two units that build
// concurrently from one base cannot both land on it, and the loser would
// otherwise have to rebuild — a whole second agent session to produce the
// change it is already holding. Replaying costs a git operation and a gate
// run. Without it, narrowing the lease would trade tokens for wall-clock.
func Rebase(ctx context.Context, dir string, newBase, oldBase forge.Commit) (forge.Commit, error) {
	// The workspace's origin is the mirror, which Fetch has already advanced;
	// this brings the new base's objects into the tree.
	if _, err := run(ctx, dir, "fetch", "origin"); err != nil {
		return "", fmt.Errorf("git: fetch before rebase: %w", err)
	}
	if _, err := run(ctx, dir, "rebase", "--onto", string(newBase), string(oldBase)); err != nil {
		if _, aerr := run(ctx, dir, "rebase", "--abort"); aerr != nil {
			return "", fmt.Errorf("%w, and the abort failed too — the tree is not landable: %v", ErrRebaseConflict, aerr)
		}
		return "", fmt.Errorf("%w: %v", ErrRebaseConflict, err)
	}
	out, err := run(ctx, dir, "rev-parse", "HEAD")
	return forge.Commit(strings.TrimSpace(out)), err
}

// CommitAll stages and commits everything in the tree.
func CommitAll(ctx context.Context, dir, message string) error {
	if _, err := run(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	_, err := run(ctx, dir, "commit", "-m", message)
	return err
}

// ChangedPaths lists the repo-relative paths that base..head touches, via
// `git diff --name-only`. An empty diff yields an empty list; an error is an
// error — a caller must never treat a failed listing as "no paths changed",
// per this repo's rule that unknown never reads as zero.
func ChangedPaths(ctx context.Context, dir string, base, head forge.Commit) ([]string, error) {
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")

	diff := exec.CommandContext(ctx, "git", "diff", "--name-only", fmt.Sprintf("%s..%s", base, head))
	diff.Dir = dir
	diff.Env = env
	var diffOut, diffErr bytes.Buffer
	diff.Stdout, diff.Stderr = &diffOut, &diffErr
	if err := diff.Run(); err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..%s: %w: %s", base, head, err, strings.TrimSpace(diffErr.String()))
	}

	var paths []string
	for _, line := range strings.Split(diffOut.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// PatchID fingerprints the content of the diff between base and head — not
// the commits that produced it. Replaying the identical patch onto a moved
// base yields the same id, which is what lets a rebase stage recognize a
// gate's verdict as still applying instead of re-running it.
//
// git patch-id emits nothing for an empty diff, and this returns "" in that
// case. Per this repo's rule that unknown never reads as zero, callers must
// treat an empty (or otherwise unproduced) patch-id as never equal to
// another, including another empty one — two diffs that both failed to
// yield an id are not thereby the same diff.
func PatchID(ctx context.Context, dir string, base, head forge.Commit) (string, error) {
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")

	diff := exec.CommandContext(ctx, "git", "diff", fmt.Sprintf("%s..%s", base, head))
	diff.Dir = dir
	diff.Env = env
	var diffOut, diffErr bytes.Buffer
	diff.Stdout, diff.Stderr = &diffOut, &diffErr
	if err := diff.Run(); err != nil {
		return "", fmt.Errorf("git diff %s..%s: %w: %s", base, head, err, strings.TrimSpace(diffErr.String()))
	}

	id := exec.CommandContext(ctx, "git", "patch-id", "--stable")
	id.Dir = dir
	id.Env = env
	id.Stdin = &diffOut
	var idOut, idErr bytes.Buffer
	id.Stdout, id.Stderr = &idOut, &idErr
	if err := id.Run(); err != nil {
		return "", fmt.Errorf("git patch-id: %w: %s", err, strings.TrimSpace(idErr.String()))
	}

	fields := strings.Fields(idOut.String())
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// Commits lists the commits reachable from to and not from — from..to, in
// git log's newest-first order — each line a short sha and the commit
// subject. This is what a park note owes when a rebase-bound retry keeps a
// commit: the landings that moved the branch, between the kept commit's old
// base and the head it would have rebased onto, are exactly this range.
//
// An empty range (from == to, or a to already contained in from) yields an
// empty slice and no error. A sha the repository does not have is an error,
// never a silently empty list: unknown never reads as zero.
func Commits(ctx context.Context, dir string, from, to forge.Commit) ([]string, error) {
	out, err := run(ctx, dir, "log", "--format=%h %s", fmt.Sprintf("%s..%s", from, to))
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

// Squash collapses base..head to a single commit carrying head's tree and
// message verbatim, then moves the workspace's detached HEAD to it. A unit's
// several build attempts are one change, and the commit graph should say so
// in one commit rather than in the shape of how many tries it took.
//
// base..head of one commit or fewer is returned as head, sha-identical: the
// loop compares kept candidates and patch-ids by sha, and rewriting a commit
// that was already singular would break resume and gate-carry for no reason.
// An unknown base or head is an error, never head or an empty commit —
// unknown never reads as zero.
func Squash(ctx context.Context, dir string, base, head forge.Commit) (forge.Commit, error) {
	out, err := run(ctx, dir, "rev-list", "--count", fmt.Sprintf("%s..%s", base, head))
	if err != nil {
		return "", fmt.Errorf("git: rev-list --count %s..%s: %w", base, head, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return "", fmt.Errorf("git: parse rev-list --count output %q: %w", out, err)
	}
	if count <= 1 {
		return head, nil
	}

	tree, err := run(ctx, dir, "rev-parse", fmt.Sprintf("%s^{tree}", head))
	if err != nil {
		return "", fmt.Errorf("git: rev-parse %s^{tree}: %w", head, err)
	}
	tree = strings.TrimSpace(tree)

	message, err := CommitMessage(ctx, dir, string(head))
	if err != nil {
		return "", fmt.Errorf("git: read message to squash: %w", err)
	}

	identity, err := run(ctx, dir, "show", "-s", "--format=%an%x00%ae%x00%ad", "--date=raw", string(head))
	if err != nil {
		return "", fmt.Errorf("git: read author identity to squash: %w", err)
	}
	fields := strings.SplitN(strings.TrimRight(identity, "\n"), "\x00", 3)
	if len(fields) != 3 {
		return "", fmt.Errorf("git: unexpected author identity for %s: %q", head, identity)
	}
	name, email, date := fields[0], fields[1], fields[2]

	cmd := exec.CommandContext(ctx, "git", "commit-tree", tree, "-p", string(base))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email, "GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=project-box", "GIT_COMMITTER_EMAIL=box@invalid",
	)
	cmd.Stdin = strings.NewReader(message)
	var commitOut, commitErr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &commitOut, &commitErr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git commit-tree: %w: %s", err, strings.TrimSpace(commitErr.String()))
	}
	squashed := forge.Commit(strings.TrimSpace(commitOut.String()))

	if _, err := run(ctx, dir, "reset", "--soft", string(squashed)); err != nil {
		return "", fmt.Errorf("git: move HEAD to squashed commit: %w", err)
	}
	return squashed, nil
}
