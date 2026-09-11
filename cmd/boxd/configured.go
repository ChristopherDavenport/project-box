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

// runConfigured is boxd from box.yaml, via internal/daemon: every box, every
// repo, one API — and a live reload on SIGHUP or POST /v1/reload, with the
// old configuration kept running if the new one is refused.
func runConfigured(path string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := daemon.New(path)
	if err != nil {
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
	slog.Info("boxd down", "reason", err)
	return nil
}
