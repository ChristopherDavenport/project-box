package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	box  string
	from string
	repo boxcfg.Repo
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
				out = append(out, selfDeployTarget{box: bx.Name, from: bx.SelfDeploy.From, repo: r})
				break
			}
		}
	}
	return out
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
	if d.selfDeploy.isRefused(head) {
		return nil // already tried this commit; the refusal is on the chain
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

	if err := d.selfDeployBuild(ctx, t, repo, head); err != nil {
		d.selfDeploy.refuse(head)
		if _, aerr := d.store.Log().Append(ctx, evidence.Record{
			Tenant: d.tenant, Scope: selfDeployScope, Kind: "self-deploy-refused",
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
		Tenant: d.tenant, Scope: selfDeployScope, Kind: "self-deployed",
		Subject: string(head), Actor: d.cfgRunner(), Body: []byte(why),
	}); err != nil {
		slog.Error("self-deploy: recording success", "box", t.box, "err", err)
	}
	if err := d.queue.Resume(ctx, d.tenant, d.cfgRunner(), why); err != nil {
		slog.Error("self-deploy: resume after successful deploy", "box", t.box, "err", err)
	}
	return fmt.Errorf("%w: %s", ErrSelfDeployed, head)
}

// selfDeployBuild checks out the target commit, runs the repo's own check in
// that tree, and — only if it passes — builds the daemon binary and installs
// it over the running executable's path. Any failure here is a refusal:
// the running binary is never touched.
func (d *Daemon) selfDeployBuild(ctx context.Context, t selfDeployTarget, repo forge.Repo, at forge.Commit) error {
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
	// Rename, never a write in place: Linux keeps the old inode open under
	// this process's own mapped pages, so the daemon serving right now is
	// untouched, and the new file is atomically what the next exec sees.
	if err := os.Rename(next, exe); err != nil {
		return fmt.Errorf("installing the new binary: %w", err)
	}
	return nil
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
