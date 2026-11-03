// Package serial connects to the hardware mixer and broadcasts normalized slider events.
package serial

import "github.com/nik9play/deej/internal/config"

// SettingsProvider supplies current snapshots and coalesced change signals.
type SettingsProvider interface {
	Values() *config.Settings
	SubscribeToChanges() chan bool
}

// Options provides diagnostics and localized connection notifications.
// Callbacks execute on the connection goroutine and should return promptly.
type Options struct {
	Verbose        bool
	OnConnected    func(port string)
	OnDisconnected func(port string)
}
