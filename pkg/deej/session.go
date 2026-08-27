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

	// DisplayName returns a human-friendly name for this session (e.g. the
	// executable's file description), or "" if none is known
	DisplayName() string

	// IsDevice returns true for device master sessions (a specific audio
	// device's volume, as opposed to a process session or the default
	// master/mic session)
	IsDevice() bool

	// IsInput returns true for capture-side sessions (microphones and other
	// input devices)
	IsInput() bool

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
	input  bool

	// used by Key(), needs to be set by child
	name string

	// used by String(), needs to be set by child
	humanReadableDesc string

	// optional human-friendly name for the settings GUI, may be empty
	displayName string
}

func (s *baseSession) DisplayName() string {
	return s.displayName
}

func (s *baseSession) IsDevice() bool {
	return s.device
}

func (s *baseSession) IsInput() bool {
	return s.input
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
