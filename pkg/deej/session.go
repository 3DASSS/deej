package deej

import (
	"strings"

	"go.uber.org/zap"
)

// Session represents a single addressable audio session
type Session interface {
	GetVolume() float32
	SetVolume(v float32) error

	// TODO: future mute support
	// GetMute() bool
	// SetMute(m bool) error

	Key() string

	// IsDevice returns true for device master sessions (a specific audio
	// device's volume, as opposed to a process session or the default
	// master/mic session)
	IsDevice() bool

	Release()
}

const (

	// ideally these would share a common ground in baseSession
	// but it will not call the child GetVolume correctly :/
	sessionCreationLogMessage = "Created audio session instance"

	// format this with s.humanReadableDesc and whatever the current volume is
	sessionStringFormat = "<session: %s, vol: %.2f>"
)

type baseSession struct {
	logger *zap.SugaredLogger
	system bool
	master bool
	device bool

	// used by Key(), needs to be set by child
	name string

	// used by String(), needs to be set by child
	humanReadableDesc string
}

func (s *baseSession) IsDevice() bool {
	return s.device
}

func (s *baseSession) Key() string {
	if s.system {
		return systemSessionName
	}

	if s.master {
		return strings.ToLower(s.name) // could be master or mic, or any device's friendly name
	}

	return strings.ToLower(s.name)
}
