package platform

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

// OpenExternal opens a file using the default associated program
func OpenExternal(logger *zap.SugaredLogger, filename string) error {
	command := getOpenExternalCommand(filename)

	if err := command.Run(); err != nil {
		logger.Warnw("Failed to open file",
			"filename", filename,
			"error", err)
		return fmt.Errorf("open file proc: %w", err)
	}

	return nil
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS
func SetupCloseHandler() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	return c
}

// GetCurrentWindowProcessNames returns the process names (including extension, if applicable)
// of the current foreground window. This includes child processes belonging to the window.
// This is currently only implemented for Windows
func GetCurrentWindowProcessNames(checkFullscreen bool) ([]string, error) {
	return getCurrentWindowProcessNames(checkFullscreen)
}

func GetAutostartState() bool {
	return getAutostartState()
}

func SetAutostartState(state bool) error {
	return setAutostartState(state)
}
