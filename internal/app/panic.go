package app

import (
	"bytes"
	"fmt"

	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

const (
	crashlogFilename        = "deej-crash-%s.log"
	crashlogTimestampFormat = "2006.01.02-15.04.05"

	crashMessage = `-----------------------------------------------------------------
                        deej crashlog
-----------------------------------------------------------------
Unfortunately, deej has crashed. This really shouldn't happen!
If you've just encountered this, please contact @omriharel and attach this error log.
You can also join the deej Discord server at https://discord.gg/nf88NJu.
-----------------------------------------------------------------
Time: %s
Panic occurred: %s
Stack trace:
%s
-----------------------------------------------------------------
`
)

// ReportCrash records a recovered panic and requests coordinated shutdown.
func (d *App) ReportCrash(r any) {

	if r == nil {
		return
	}

	// if we got here, we're recovering from a panic!
	now := time.Now()

	ex, err := os.Executable()
	if err != nil {
		panic(fmt.Errorf("get executable dir: %w", err))
	}

	logDirectory := filepath.Join(filepath.Dir(ex), "logs")

	// that would suck
	if err := os.MkdirAll(logDirectory, 0755); err != nil {
		panic(fmt.Errorf("ensure crashlog dir exists: %w", err))
	}

	crashlogBytes := bytes.NewBufferString(fmt.Sprintf(crashMessage, now.Format(crashlogTimestampFormat), r, debug.Stack()))
	crashlogPath := filepath.Join(logDirectory, fmt.Sprintf(crashlogFilename, now.Format(crashlogTimestampFormat)))

	// that would REALLY suck
	if err := os.WriteFile(crashlogPath, crashlogBytes.Bytes(), os.ModePerm); err != nil {
		panic(fmt.Errorf("can't even write the crashlog file contents: %w", err))
	}

	d.logger.Errorw("Encountered and logged panic, crashing",
		"crashlogPath", crashlogPath,
		"error", r)

	d.notifier.Notify("Unexpected crash occurred...",
		fmt.Sprintf("More details in %s", crashlogPath))

	// bye :(
	d.RequestStop()
}
