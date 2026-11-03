//go:generate go run github.com/tc-hib/go-winres@latest make --arch amd64,arm64 --out rsrc

package main

import (
	"flag"
	"fmt"

	"github.com/nik9play/deej/internal/app"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"syscall"
)

var (
	version    string
	gitCommit  string
	versionTag string
	buildType  string

	verbose    bool
	configPath string
)

func init() {
	flag.BoolVar(&verbose, "verbose", false, "show verbose logs (useful for debugging serial)")
	flag.BoolVar(&verbose, "v", false, "shorthand for --verbose")
	flag.StringVar(&configPath, "config", "", "custom config file path")
	flag.StringVar(&configPath, "c", "", "shorthand for --config")
	flag.Parse()
}

func main() {

	// first we need a logger
	logger, err := app.NewLogger(buildType)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}

	named := logger.Named("main")
	named.Debug("Created logger")

	named.Infow("Version info",
		"gitCommit", gitCommit,
		"versionTag", versionTag,
		"buildType", buildType)

	// provide a fair warning if the user's running in verbose mode
	if verbose {
		named.Debug("Verbose flag provided, all log messages will be shown")
	}

	// create the deej instance
	d, err := app.New(logger, verbose, configPath)
	if err != nil {
		named.Fatalw("Failed to create deej object", "error", err)
	}

	// if injected by build process, set version info to show up in the tray
	if buildType != "" && (versionTag != "" || gitCommit != "") {
		identifier := gitCommit
		if versionTag != "" {
			identifier = versionTag
		}

		versionString := fmt.Sprintf("Version %s-%s", buildType, identifier)
		version = versionString
	}

	// onwards, to glory
	if err = d.Initialize(); err != nil {
		_ = d.Stop()
		named.Fatalw("Failed to initialize deej", "error", err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	signalDone := make(chan struct{})
	defer close(signalDone)
	go func() {
		select {
		case <-signals:
			d.RequestStop()
		case <-signalDone:
		}
	}()
	if err = runSafely(d, logger, version); err != nil {
		named.Errorw("Application exited with error", "error", err)
		os.Exit(1)
	}
}

func runSafely(core *app.App, logger *zap.SugaredLogger, version string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			core.ReportCrash(recovered)
			_ = core.Stop()
			err = fmt.Errorf("application panic: %v", recovered)
		}
	}()
	return run(core, logger, version)
}
