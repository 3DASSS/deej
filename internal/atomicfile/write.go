package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to a temp file in the target's directory and
// renames it over the target, so a crash mid-write can't corrupt the config.
// The config holds the OBS websocket password, so it stays owner-only: the
// mode comes from os.CreateTemp, which creates at 0600, and rename preserves
// it - don't widen it
func Write(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}
