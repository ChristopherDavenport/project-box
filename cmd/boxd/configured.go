package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ChristopherDavenport/project-box/internal/daemon"
	"github.com/ChristopherDavenport/project-box/internal/telemetry"
)

// recoverSelfDeploy is the whole of cmd's rollback logic: resolve where a
// marker would be, ask daemon whether this startup failure should roll back,
// and if so re-exec into the binary it just restored. Every decision lives
// in internal/daemon, behind [daemon.TargetForRollback] and
// [daemon.RecoverFromFailedStart], so it is testable there; this is only the
// argv/syscall seam only cmd/boxd owns.
func recoverSelfDeploy(cfgPath string, startupErr error) {
	target, err := daemon.TargetForRollback(cfgPath)
	if err != nil {
		slog.Error("self-deploy rollback: resolving target", "err", err)
		return
	}
	recovered, err := daemon.RecoverFromFailedStart(target, startupErr)
	if err != nil {
		slog.Error("self-deploy rollback", "err", err)
	}
	if !recovered {
		return
	}
	slog.Error("self-deploy: startup failed, restored the previous binary and re-execing", "err", startupErr)
	argv0, err := os.Executable()
	if err != nil {
		slog.Error("self-deploy rollback: locating executable to re-exec", "err", err)
		return
	}
	if err := syscall.Exec(argv0, os.Args, os.Environ()); err != nil {
		slog.Error("self-deploy rollback: re-exec failed", "err", err)
	}
}

// runConfigured is boxd from box.yaml, via internal/daemon: every box, every
// repo, one API — and a live reload on SIGHUP or POST /v1/reload, with the
// old configuration kept running if the new one is refused.
func runConfigured(path string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := daemon.New(path)
	if err != nil {
		recoverSelfDeploy(path, err)
		return err
	}
	defer d.Close()

	shutdown, err := telemetry.Setup(ctx, os.Getenv("BOX_OTLP"), "boxd")
	if err != nil {
		return err
	}
	defer shutdown(context.Background())

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if summary, err := d.Reload(ctx, "SIGHUP"); err != nil {
				slog.Error("reload refused", "err", err)
			} else {
				slog.Info("reloaded", "summary", summary)
			}
		}
	}()

	// Only this package knows argv and owns the syscall; the daemon just
	// knows it needs to happen, once Run has returned and its own shutdown
	// (the API server, the deferred store close below) has had its turn.
	d.Exec = func() error {
		argv0, err := os.Executable()
		if err != nil {
			return err
		}
		return syscall.Exec(argv0, os.Args, os.Environ())
	}

	err = d.Run(ctx)
	if errors.Is(err, daemon.ErrSelfDeployed) {
		if rerr := d.Reexec(); rerr != nil {
			slog.Error("self-deploy re-exec failed", "err", rerr)
		}
		return nil
	}
	// ctx.Err() (context.Canceled, from the signal handler above) is an
	// ordinary graceful shutdown — the daemon came up fine. Anything else
	// here means Run returned before ever reaching "boxd up", which is
	// exactly the failure [recoverSelfDeploy] exists for.
	if err != nil && !errors.Is(err, context.Canceled) {
		// Closed here, ahead of the deferred Close above: recoverSelfDeploy
		// may re-exec, and a re-exec never returns to run that defer — the
		// store must already be shut before then, same as the success path's
		// Reexec does for this same reason. Close is idempotent, so the
		// deferred call above still firing on any other return is harmless.
		if cerr := d.Close(); cerr != nil {
			slog.Error("closing store before self-deploy rollback", "err", cerr)
		}
		recoverSelfDeploy(path, err)
		return err
	}
	slog.Info("boxd down", "reason", err)
	return nil
}
