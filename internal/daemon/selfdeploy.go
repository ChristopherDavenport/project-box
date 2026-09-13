package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
	"github.com/ChristopherDavenport/project-box/internal/forge"
	"github.com/ChristopherDavenport/project-box/internal/gate"
	"github.com/ChristopherDavenport/project-box/internal/version"
)

// selfDeployScope is the standing evidence chain for the self-deploy chore —
// a refusal, and a successful deploy, legible on their own rather than
// folded into control/claims, where the stop and resume records already land
// indistinguishable from a human pressing the same buttons.
const selfDeployScope = "control/self-deploy"

// selfDeployRun names the lease and workspace the chore checks out under.
// Fixed, because by the time it is acquired claims are stopped and every run
// has settled — nothing else is ever contending for it.
const selfDeployRun = "self-deploy"

// selfDeployTTL bounds the lease the chore's checkout renews — long enough
// that a real repository's check and a `go build` of the daemon do not race
// the clock.
const selfDeployTTL = 30 * time.Minute

// Kinds recorded on selfDeployScope. Named rather than inlined at each call
// site so [Daemon.selfDeployRefused] and the room's fold (internal/control)
// name the exact same strings a typo in an inline literal could silently
// drift from.
const (
	kindSelfDeployed      = "self-deployed"
	kindSelfDeployRefused = "self-deploy-refused"
)

// ErrSelfDeployed is Run's sentinel: the chore replaced the binary on disk
// and the process needs to re-exec. Run returns it so its own deferred
// httpSrv.Shutdown still runs; the caller (cmd/boxd) is the one that closes
// the store and calls Exec — an Exec called deep inside the tick loop would
// skip that clean shutdown.
var ErrSelfDeployed = errors.New("daemon: self-deployed, re-exec required")

// selfDeployState is the daemon's memory of commits the chore has already
// refused. Without it a bad commit would stop claims, spend a full check,
// refuse, and resume, forever — once every housekeeping pass.
type selfDeployState struct {
	mu      sync.Mutex
	refused map[forge.Commit]bool
}

func (s *selfDeployState) isRefused(c forge.Commit) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused[c]
}

func (s *selfDeployState) refuse(c forge.Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused == nil {
		s.refused = map[forge.Commit]bool{}
	}
	s.refused[c] = true
}

// selfDeployTarget is one box's self_deploy: block, resolved to the repo it
// names — boxcfg.Load has already refused a block naming no repo, an unknown
// repo, or a repo with no check.
type selfDeployTarget struct {
	box      string
	from     string
	repo     boxcfg.Repo
	sections []string
}

// selfDeployTargets resolves every box that has turned the chore on.
func (d *Daemon) selfDeployTargets() []selfDeployTarget {
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()
	var out []selfDeployTarget
	for _, bx := range cfg.Boxes {
		if bx.SelfDeploy == nil {
			continue
		}
		for _, r := range bx.Repos {
			if r.Name == bx.SelfDeploy.Repo {
				out = append(out, selfDeployTarget{
					box: bx.Name, from: bx.SelfDeploy.From, repo: r,
					sections: bx.SelfDeploy.Sections,
				})
				break
			}
		}
	}
	return out
}

// followBranchForAPI answers /v1/status's followed-branch fields: the box's
// self_deploy.from branch and the commit this daemon's own mirror last
// recorded for it. It resolves, never fetches — the status handler shows the
// mirror's own record, not a fresh look at the branch — and returns
// ("", "", nil) when no box has self_deploy configured, which the API reads
// as the fields being absent, not an error.
//
// A daemon with more than one self_deploy box has no single-box answer here
// either, the same way its Box field already picks Boxes[0]: this reports
// the first target [Daemon.selfDeployTargets] finds.
func (d *Daemon) followBranchForAPI(ctx context.Context) (string, string, error) {
	targets := d.selfDeployTargets()
	if len(targets) == 0 {
		return "", "", nil
	}
	t := targets[0]
	head, err := d.mirror.Resolve(ctx, t.repo.ForgeRepo(), "refs/heads/"+t.from)
	if err != nil {
		return "", "", fmt.Errorf("resolving self_deploy branch %s: %w", t.from, err)
	}
	return t.from, string(head), nil
}

// selfDeployChore is one housekeeping pass. It does nothing, silently, in
// the overwhelmingly common case: no box has turned it on, this binary's own
// build commit is not knowable, or no target's branch has moved past it.
//
// It returns [ErrSelfDeployed] exactly once, on the pass that installs a new
// binary — the caller's cue to close the store and re-exec. Every other
// return is nil: a refusal is not a Go error, it is a decision recorded on
// the chain and logged, and an unexpected failure (a fetch that could not
// reach the remote, say) is logged and left for the next pass rather than
// escalated into the tick loop's own error handling.
func (d *Daemon) selfDeployChore(ctx context.Context) error {
	targets := d.selfDeployTargets()
	if len(targets) == 0 {
		return nil
	}
	build := d.build()
	if !build.Known {
		slog.Warn("self-deploy: this binary's own build commit is unknown — declining to compare against a guess")
		return nil
	}
	if build.Dirty {
		slog.Warn("self-deploy: this binary was built from a modified tree — it corresponds to no commit, declining to compare")
		return nil
	}
	for _, t := range targets {
		err := d.selfDeployOne(ctx, t, build)
		if err == nil {
			continue
		}
		if errors.Is(err, ErrSelfDeployed) {
			return err
		}
		slog.Warn("self-deploy", "box", t.box, "err", err)
	}
	return nil
}

// selfDeployOne runs the chore for a single box's target. A nil return means
// nothing happened or a refusal was recorded and logged; a non-nil, non-
// [ErrSelfDeployed] return is an unexpected failure the caller logs and
// moves past.
func (d *Daemon) selfDeployOne(ctx context.Context, t selfDeployTarget, build version.Build) error {
	repo := t.repo.ForgeRepo()
	if err := d.mirror.Fetch(ctx, repo); err != nil {
		return fmt.Errorf("box %s: fetch %s: %w", t.box, repo.Name, err)
	}
	head, err := d.mirror.Resolve(ctx, repo, "refs/heads/"+t.from)
	if err != nil {
		return fmt.Errorf("box %s: resolve %s: %w", t.box, t.from, err)
	}
	if string(head) == build.Commit {
		return nil // the common case: already running what the branch names
	}
	refused, err := d.selfDeployRefused(ctx, head)
	if err != nil {
		return fmt.Errorf("box %s: checking refusal history: %w", t.box, err)
	}
	if refused {
		return nil // already tried this commit; the refusal is on the chain
	}

	if len(t.sections) > 0 {
		changed, err := d.mirror.ChangedPaths(ctx, repo, forge.Commit(build.Commit), head)
		if err != nil {
			// An unanswerable diff must not stall the box behind a docs
			// landing forever — proceed to deploy rather than let "unknown"
			// stand in for "nothing changed".
			slog.Warn("self-deploy: changed paths — proceeding to deploy rather than guessing", "box", t.box, "err", err)
		} else if selfDeploySkippable(changed, t.repo.Sections, t.sections) {
			// The diff is entirely docs/room-shaped. Claims are never
			// stopped for a landing that will be skipped, and the compare
			// is always build.Commit->head, so a skipped landing needs no
			// memory of its own: the next code landing's diff is cumulative
			// and includes it.
			slog.Info("self-deploy: landing touched only sections outside self_deploy.sections — skipping", "box", t.box, "commit", head)
			return nil
		}
	}

	why := fmt.Sprintf("self-deploy: box %s %s -> %s", t.box, short(build.Commit), short(string(head)))
	if err := d.queue.Stop(ctx, d.tenant, d.cfgRunner(), why); err != nil {
		return fmt.Errorf("box %s: stop: %w", t.box, err)
	}

	dctx, cancel := context.WithTimeout(ctx, d.drain())
	clean := d.pump.Quiesce(dctx)
	cancel()
	if !clean {
		// Abandon rather than build on the strength of a drain that did not
		// finish: no run is ever cut short for this. The head is not
		// remembered as refused — it was never attempted — so the next
		// housekeeping pass tries again.
		if rerr := d.queue.Resume(ctx, d.tenant, d.cfgRunner(), why); rerr != nil {
			slog.Error("self-deploy: resume after abandoned quiesce", "box", t.box, "err", rerr)
		}
		slog.Warn("self-deploy: drain window elapsed with work in flight — abandoning this pass", "box", t.box, "commit", head)
		return nil
	}

	if err := d.selfDeployBuild(ctx, t, repo, head, why); err != nil {
		d.selfDeploy.refuse(head)
		if _, aerr := d.store.Log().Append(ctx, evidence.Record{
			Tenant: d.tenant, Scope: selfDeployScope, Kind: kindSelfDeployRefused,
			Subject: string(head), Actor: d.cfgRunner(), Body: []byte(err.Error()),
		}); aerr != nil {
			slog.Error("self-deploy: recording refusal", "box", t.box, "err", aerr)
		}
		if rerr := d.queue.Resume(ctx, d.tenant, d.cfgRunner(), why); rerr != nil {
			slog.Error("self-deploy: resume after refusal", "box", t.box, "err", rerr)
		}
		slog.Warn("self-deploy refused", "box", t.box, "commit", head, "reason", err)
		return nil
	}

	if _, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: selfDeployScope, Kind: kindSelfDeployed,
		Subject: string(head), Actor: d.cfgRunner(), Body: []byte(why),
	}); err != nil {
		slog.Error("self-deploy: recording success", "box", t.box, "err", err)
	}
	if err := d.queue.Resume(ctx, d.tenant, d.cfgRunner(), why); err != nil {
		slog.Error("self-deploy: resume after successful deploy", "box", t.box, "err", err)
	}
	return fmt.Errorf("%w: %s", ErrSelfDeployed, head)
}

// selfDeployRefused reports whether head has already been decided against,
// either in this process's own memory or, durably, on the chain: the
// standing record [Daemon.recordRolledBack] or a refusal in selfDeployOne
// itself already wrote. The chain read matters most exactly when the
// in-memory map cannot: a binary [RecoverFromFailedStart] just restored
// boots with an empty refused map, and without this a bad head that has not
// moved would be retried every housekeeping pass, forever.
func (d *Daemon) selfDeployRefused(ctx context.Context, head forge.Commit) (bool, error) {
	if d.selfDeploy.isRefused(head) {
		return true, nil
	}
	var latest evidence.Record
	for rec, err := range d.store.Log().BySubject(ctx, d.tenant, string(head)) {
		if err != nil {
			return false, err
		}
		if rec.Scope != selfDeployScope {
			continue
		}
		if latest.Seq == 0 || rec.Seq > latest.Seq {
			latest = rec
		}
	}
	refused := latest.Kind == kindSelfDeployRefused || latest.Kind == kindSelfDeployRolledBack
	if refused {
		d.selfDeploy.refuse(head)
	}
	return refused, nil
}

// selfDeployBuild checks out the target commit, runs the repo's own check in
// that tree, and — only if it passes — builds the daemon binary and installs
// it over the running executable's path. Any failure here is a refusal:
// the running binary is never touched.
//
// Before the install rename, it preserves the running executable as
// exe+".prev" and writes a marker naming the head it is about to install —
// [Daemon.resolveSelfDeployMarker] is what tells "I was just self-deployed"
// from an ordinary start, and [RecoverFromFailedStart] is what a process
// that never gets that far falls back to.
func (d *Daemon) selfDeployBuild(ctx context.Context, t selfDeployTarget, repo forge.Repo, at forge.Commit, why string) error {
	leaser := d.store.Leaser(d.cfgRunner())
	lease, err := leaser.Acquire(ctx, forge.LeaseRequest{
		Tenant: d.tenant, Repo: repo.Name, Ref: "run/" + selfDeployRun, Run: selfDeployRun, TTL: selfDeployTTL,
	})
	if err != nil {
		return fmt.Errorf("lease: %w", err)
	}
	defer leaser.Release(ctx, lease)
	ws, err := d.mirror.Checkout(ctx, lease, at)
	if err != nil {
		return fmt.Errorf("checkout: %w", err)
	}
	v, err := (gate.Command{Label: "self-deploy-check", Dir: ws.Dir,
		Commands: [][]string{{"sh", "-c", t.repo.Check}}}).Evaluate(ctx, forge.ChangeSet{})
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if v.Decision != forge.DecisionPass {
		return fmt.Errorf("check failed: %s", firstLine(v.Reason))
	}
	exe, err := d.exePath()
	if err != nil {
		return fmt.Errorf("locating the running executable: %w", err)
	}
	next := exe + ".next"
	build := exec.CommandContext(ctx, "go", "build", "-o", next, "./cmd/boxd")
	build.Dir = ws.Dir
	out, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w: %s", err, firstLine(string(out)))
	}
	// Preserve what is running now and say what is about to replace it —
	// both before the install, so a refusal here (nothing written yet) still
	// leaves the running binary untouched, same as every earlier refusal in
	// this function.
	if err := preserveRunning(exe); err != nil {
		return fmt.Errorf("preserving the running binary: %w", err)
	}
	if err := writeSelfDeployMarker(d.stateRoot(), selfDeployMarker{Head: string(at), Reason: why, At: time.Now()}); err != nil {
		return fmt.Errorf("writing self-deploy marker: %w", err)
	}
	// Rename, never a write in place: Linux keeps the old inode open under
	// this process's own mapped pages, so the daemon serving right now is
	// untouched, and the new file is atomically what the next exec sees.
	if err := os.Rename(next, exe); err != nil {
		return fmt.Errorf("installing the new binary: %w", err)
	}
	return nil
}

// selfDeploySkippable reports whether every one of changed falls inside some
// declared section of the repo whose name is NOT in warrant — i.e. the diff
// is entirely docs/room-shaped and self_deploy.sections says a landing there
// warrants no deploy. A path that matches no declared section counts as
// touching the daemon's tree (go.mod, hack/, an unsectioned cmd) and forces
// a deploy: unknown never reads as zero.
func selfDeploySkippable(changed []string, sections []boxcfg.Section, warrant []string) bool {
	warranted := map[string]bool{}
	for _, n := range warrant {
		warranted[n] = true
	}
	for _, path := range changed {
		name, ok := sectionOfPath(path, sections)
		if !ok || warranted[name] {
			return false
		}
	}
	return true
}

// sectionOfPath answers the first declared section whose Paths cover path,
// matched by prefix at a path-segment boundary: "internal/loop" matches
// "internal/loop" and "internal/loop/x.go", never "internal/loopy".
func sectionOfPath(path string, sections []boxcfg.Section) (string, bool) {
	for _, s := range sections {
		for _, p := range s.Paths {
			if path == p || strings.HasPrefix(path, p+"/") {
				return s.Name, true
			}
		}
	}
	return "", false
}

// Reexec closes the store and calls Exec, completing a self-deploy after Run
// has returned [ErrSelfDeployed] and the caller has finished its own
// shutdown. Exec is nil-safe: a Daemon nobody wired for self-deploy has no
// seam to call, and closing the store is still the right thing to do.
func (d *Daemon) Reexec() error {
	if err := d.Close(); err != nil {
		return err
	}
	if d.Exec == nil {
		return nil
	}
	return d.Exec()
}

// short renders a commit for a log line or a claims-chain reason: distinct
// enough to eyeball, at a fraction of the width.
func short(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
