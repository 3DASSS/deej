//go:build headless

package main

import (
	"github.com/nik9play/deej/internal/app"
	"go.uber.org/zap"
)

func run(core *app.App, _ *zap.SugaredLogger, _ string) error {
	core.Start()
	core.Wait()
	return core.Stop()
}
