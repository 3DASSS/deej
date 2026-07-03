package deej

// SessionEventHandler receives session add/remove events from a SessionFinder
type SessionEventHandler func(event SessionEvent)

// SessionFinder represents an entity that can find all current audio sessions
type SessionFinder interface {
	// Start begins session discovery. Events are delivered by calling handler
	// synchronously: the finder owns every session it emits and releases a
	// removed session only after the handler for its removed event returns,
	// so the handler must ensure the session is unreachable by then
	Start(handler SessionEventHandler) error

	Release() error
}

// SessionEvent represents a session add/remove event
type SessionEvent struct {
	Type      SessionEventType
	Session   Session
	SessionID string
}

// SessionEventType indicates whether a session was added or removed
type SessionEventType int

const (
	// SessionEventAdded indicates a new session was created
	SessionEventAdded SessionEventType = iota
	// SessionEventRemoved indicates a session was removed/disconnected
	SessionEventRemoved
)
