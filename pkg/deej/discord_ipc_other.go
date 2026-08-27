//go:build !windows

package deej

import (
	"cmp"
	"io"
	"net"
	"os"
	"path/filepath"
)

func discordIPCPaths() []string {
	root := cmp.Or(os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("TMPDIR"), "/tmp")

	// flatpak and snap installs put their socket in a sandbox subdirectory
	dirs := []string{"", "app/com.discordapp.Discord", "app/com.discordapp.DiscordCanary", "snap.discord"}

	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(root, dir, "discord-ipc-"))
	}

	return paths
}

// openDiscordIPC connects to one candidate socket. A unix socket supports
// concurrent reads and writes, which is what the read loop relies on
func openDiscordIPC(path string) (io.ReadWriteCloser, error) {
	return net.Dial("unix", path)
}
