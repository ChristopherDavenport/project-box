package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ChristopherDavenport/project-box/internal/boxcfg"
	"github.com/ChristopherDavenport/project-box/internal/evidence"
)

// selfDeployMarkerFile is the marker's fixed name, one level under the
// daemon's state root — never beside the executable, which a self-deploy
// itself replaces and which tells a process nothing about which tenant or
// store it belongs to.
const selfDeployMarkerFile = "self-deploy.json"

// kindSelfDeployRolledBack marks a head that was installed but never came
// up: [Daemon.resolveSelfDeployMarker] records it against the head that
// failed, not the binary that is running again.
const kindSelfDeployRolledBack = "self-deploy-rolled-back"

// selfDeployExceptionPrefix marks an index/exceptions record as one of
// these rollbacks rather than an ingress origination or a work item. See
// [control.Room.Exceptions]'s matching branch, which this string's value is
// duplicated into rather than imported — control cannot depend on daemon
// without a cycle, and the two packages already share "index/exceptions"
// and "control/self-deploy" the same informal way.
const selfDeployExceptionPrefix = "self-deploy/"

// selfDeployMarker is what one process leaves for its successor: the head it
// just installed, and enough context to explain a rollback if that head
// never comes up. Reason starts as the build/check context of the deploy
// that wrote it, and [RecoverFromFailedStart] overwrites it with the actual
// startup failure when there is one to report — a rollback exception should
// say why the box went down, not why the commit once looked good.
type selfDeployMarker struct {
	Head   string    `json:"head"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func selfDeployMarkerPath(root string) string {
	return filepath.Join(root, selfDeployMarkerFile)
}

func writeSelfDeployMarker(root string, m selfDeployMarker) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding self-deploy marker: %w", err)
	}
	if err := os.WriteFile(selfDeployMarkerPath(root), body, 0o644); err != nil {
		return fmt.Errorf("writing self-deploy marker: %w", err)
	}
	return nil
}

// readSelfDeployMarker reports the marker and whether one was present at
// all — an absent marker is the overwhelmingly common start, not an error.
func readSelfDeployMarker(root string) (selfDeployMarker, bool, error) {
	body, err := os.ReadFile(selfDeployMarkerPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return selfDeployMarker{}, false, nil
	}
	if err != nil {
		return selfDeployMarker{}, false, fmt.Errorf("reading self-deploy marker: %w", err)
	}
	var m selfDeployMarker
	if err := json.Unmarshal(body, &m); err != nil {
		return selfDeployMarker{}, false, fmt.Errorf("decoding self-deploy marker: %w", err)
	}
	return m, true, nil
}

func removeSelfDeployMarker(root string) error {
	err := os.Remove(selfDeployMarkerPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// preserveRunning copies (or, where the filesystem allows it, hard-links)
// the running executable to exe+".prev" before it is overwritten. It runs
// before the install rename, never instead of it: the rename stays the sole
// atomic swap of what a new exec sees, and this is purely insurance for a
// process that never gets that far.
func preserveRunning(exe string) error {
	prev := exe + ".prev"
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Link(exe, prev); err == nil {
		return nil
	}
	return copyFile(exe, prev)
}

// copyFile is preserveRunning's fallback when a hard link cannot be made —
// exe and its ".prev" sibling on different filesystems, most often.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// restoreExe puts the preserved prior binary back, the same atomic rename
// selfDeployBuild installs a new one with.
func restoreExe(exe string) error {
	return os.Rename(exe+".prev", exe)
}

// stateRoot is the daemon's workspace root — mirrors, worktrees, plans, and
// the self-deploy marker all live under it. Read under mu like every other
// field a reload could in principle touch, even though Root is identity and
// Reload refuses a config that changes it.
func (d *Daemon) stateRoot() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.Root
}

// resolveSelfDeployMarker settles whatever a previous process left behind,
// once this one has actually come up — called from Run after the listener
// is bound, never from New, so a daemon that fails before serving leaves the
// marker for [RecoverFromFailedStart] to find instead.
//
// A binary that cannot say what commit it was built from cannot be compared
// against the marker's head either way; unknown never reads as zero, so it
// leaves the marker alone rather than guessing at either outcome.
func (d *Daemon) resolveSelfDeployMarker(ctx context.Context) {
	root := d.stateRoot()
	marker, ok, err := readSelfDeployMarker(root)
	if err != nil {
		slog.Error("self-deploy: reading marker", "err", err)
		return
	}
	if !ok {
		return
	}
	build := d.build()
	if !build.Known {
		slog.Warn("self-deploy: this binary's own build commit is unknown — leaving the marker for a build that can identify itself")
		return
	}
	if build.Commit != marker.Head {
		if err := d.recordRolledBack(ctx, marker); err != nil {
			slog.Error("self-deploy: recording rollback", "err", err)
			return
		}
		slog.Warn("self-deploy: rolled back", "head", marker.Head, "reason", marker.Reason)
	}
	if err := removeSelfDeployMarker(root); err != nil {
		slog.Error("self-deploy: removing marker", "err", err)
	}
}

// recordRolledBack is the standing chain's record of a head that was
// installed but never came up, plus the index/exceptions record that puts it
// in front of a person — the same two-write shape [Daemon.selfDeployOne]
// uses for a refusal, subject to the same append-only, on-behalf-of rules
// (docs/PLAN.md §6: evidence is written where the fact is known, by the actor
// that knows it).
func (d *Daemon) recordRolledBack(ctx context.Context, marker selfDeployMarker) error {
	if _, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: selfDeployScope, Kind: kindSelfDeployRolledBack,
		Subject: marker.Head, Actor: d.cfgRunner(), Body: []byte(marker.Reason),
	}); err != nil {
		return err
	}
	_, err := d.store.Log().Append(ctx, evidence.Record{
		Tenant: d.tenant, Scope: "index/exceptions", Kind: "indexed",
		Subject: selfDeployExceptionPrefix + marker.Head, Actor: d.cfgRunner(), Body: []byte(marker.Reason),
	})
	return err
}

// RollbackTarget is what cmd/boxd needs to attempt a rollback after a
// startup failure: where to look for a marker, and which executable path to
// restore over.
type RollbackTarget struct {
	Root string
	Exe  string
}

// TargetForRollback resolves a RollbackTarget straight from box.yaml.
// daemon.New may have failed before producing a *Daemon at all, so this
// reads the config fresh rather than through one.
func TargetForRollback(cfgPath string) (RollbackTarget, error) {
	cfg, err := boxcfg.Load(cfgPath)
	if err != nil {
		return RollbackTarget{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return RollbackTarget{}, err
	}
	return RollbackTarget{Root: cfg.Root, Exe: exe}, nil
}

// RecoverFromFailedStart is cmd/boxd's whole rollback decision: called when
// daemon.New has errored, or Run has errored before serving. A present
// marker means this boot is a self-deploy that did not come up; its absence
// means either an ordinary failure with nothing to roll back to, or a
// rollback this process (or an earlier attempt) already resolved — either
// way, false and a nil error.
//
// The exe is restored before the marker is touched: a restore failure must
// leave the original build/check context in place for the next attempt,
// not a reason describing a recovery that never happened.
func RecoverFromFailedStart(target RollbackTarget, startupErr error) (bool, error) {
	marker, ok, err := readSelfDeployMarker(target.Root)
	if err != nil || !ok {
		return false, err
	}
	if err := restoreExe(target.Exe); err != nil {
		return false, fmt.Errorf("restoring the previous binary: %w", err)
	}
	marker.Reason = firstLine(startupErr.Error())
	if err := writeSelfDeployMarker(target.Root, marker); err != nil {
		return true, fmt.Errorf("updating self-deploy marker: %w", err)
	}
	return true, nil
}
