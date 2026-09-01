package main

import (
	"context"
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

	err = d.Run(ctx)
	slog.Info("boxd down", "reason", err)
	return nil
}
