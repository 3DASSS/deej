package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// how long after the last file event to wait before reloading, so editors
// that write multiple times (or write partial content) settle first
const watchDebounceDelay = 250 * time.Millisecond

// WatchConfigFileChanges starts watching for configuration file changes
// and attempts reloading the config when they happen
func (cc *Store) WatchConfigFileChanges() {
	cc.logger.Debugw("Starting to watch user config file for changes", "path", cc.configPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		cc.logger.Warnw("Failed to create filesystem watcher", "error", err)
		return
	}
	defer watcher.Close()

	// watch the directory rather than the file itself, so atomic
	// (write-temp-then-rename) saves and editors that replace the file
	// don't break the watch
	if err := watcher.Add(filepath.Dir(cc.configPath)); err != nil {
		cc.logger.Warnw("Failed to watch config directory", "error", err)
		return
	}

	// trailing-edge debounce timer, armed on every relevant event. Go 1.23+
	// timer channels are unbuffered, so Stop/Reset never leave a stale tick
	// behind and the usual drain dance isn't needed
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	defer debounce.Stop()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if !strings.EqualFold(filepath.Clean(event.Name), filepath.Clean(cc.configPath)) {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			debounce.Reset(watchDebounceDelay)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			cc.logger.Warnw("Config file watcher error", "error", err)

		case <-debounce.C:
			cc.handleConfigFileChange()

		case <-cc.stopWatcher:
			cc.logger.Debug("Stopping user config file watcher")
			return
		}
	}
}

func (cc *Store) handleConfigFileChange() {
	cc.logger.Debug("Config file modified, attempting reload")

	cc.lock.Lock()
	previous := cc.current.Load()

	if err := cc.loadLocked(); err != nil {
		cc.lock.Unlock()
		cc.logger.Warnw("Failed to reload config file", "error", err)
		return
	}

	// a GUI save already loaded, applied and notified synchronously; the file
	// event it triggers just reloads identical content. Skip the redundant
	// toast and consumer notification whenever the config didn't actually
	// change - this also covers a hand edit that only touched comments or
	// whitespace
	if previous != nil && reflect.DeepEqual(previous, cc.current.Load()) {
		cc.lock.Unlock()
		cc.logger.Debug("Config unchanged after reload, skipping notification")
		return
	}

	cc.lock.Unlock()
	cc.logger.Info("Reloaded config successfully")

	cc.report(Reloaded, nil)

	cc.onConfigReloaded()
}

// StopWatchingConfigFile signals our filesystem watcher to stop
func (cc *Store) StopWatchingConfigFile() {
	cc.stopOnce.Do(func() { close(cc.stopWatcher) })
}

func (cc *Store) onConfigReloaded() {
	cc.logger.Debug("Notifying consumers about configuration reload")

	cc.consumersLock.Lock()
	defer cc.consumersLock.Unlock()

	for _, consumer := range cc.reloadConsumers {
		// non-blocking send: a signal already pending in the buffer tells the
		// consumer everything it needs (re-read Values), so never block on it
		select {
		case consumer <- true:
		default:
		}
	}
}
