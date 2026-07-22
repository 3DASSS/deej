//go:build headless

package deej

// This file provides the no-GUI implementation of the tray surface, selected by
// the "headless" build tag. It contains no Wails import, so a headless build
// links none of the Wails GTK4/WebKitGTK backend and can be built with
// CGO_ENABLED=0 (and cross-compiled). deej then runs as a config-file-driven
// daemon: the serial->audio loop with fsnotify hot-reload, but no tray icon or
// settings window.

// trayState is an empty placeholder in headless builds; the GUI build backs it
// with the Wails application (see tray.go).
type trayState struct{}

// initializeTray runs the main loop directly instead of hosting a tray, mirroring
// the existing DEEJ_NO_TRAY_ICON runtime path in Initialize. onDone blocks until
// deej stops.
func (d *Deej) initializeTray(onDone func()) {
	d.logger.Debug("Running headless (no tray, built with -tags headless)")
	onDone()
}

// stopTray is a no-op in headless builds; there is no tray to tear down.
func (d *Deej) stopTray() {}
