package deej

import (
	"io"
	"time"

	"github.com/Microsoft/go-winio"
)

// how long to wait for the named pipe to accept a connection
const discordPipeDialTimeout = 2 * time.Second

func discordIPCPaths() []string {
	return []string{`\\.\pipe\discord-ipc-`}
}

// openDiscordIPC connects to one candidate pipe.
//
// This deliberately does not use os.OpenFile: that opens the pipe as a
// synchronous handle, and Windows serializes I/O on one of those - a blocking
// ReadFile from the read loop makes a concurrent WriteFile on the same handle
// wait behind it, which deadlocks the moment deej sends a command while
// waiting for a reply. winio opens the pipe overlapped, which is what makes
// concurrent reads and writes safe
func openDiscordIPC(path string) (io.ReadWriteCloser, error) {
	timeout := discordPipeDialTimeout

	return winio.DialPipe(path, &timeout)
}
