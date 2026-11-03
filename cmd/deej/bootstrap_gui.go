//go:build !headless

package main

import (
	"github.com/nik9play/deej/internal/app"
	"github.com/nik9play/deej/internal/desktop"
	"go.uber.org/zap"
	"os"
)

func run(core *app.App, logger *zap.SugaredLogger, version string) error {
	if _, disabled := os.LookupEnv("DEEJ_NO_TRAY_ICON"); disabled {
		core.Start()
		core.Wait()
		return core.Stop()
	}
	shell := desktop.New(desktop.Options{Config: core.Config(), Serial: core.Serial(), Mixer: core.Mixer(), OBS: core.OBS(), Discord: core.Discord(), Logger: logger, Notifier: core.Notifier(), Localizer: core.Localizer(), Language: core.Language(), Version: version, Start: core.Start, Wait: core.Wait, Stop: core.Stop, RequestStop: core.RequestStop})
	return shell.Run()
}
