// Package mixer routes slider positions to audio and integration targets.
package mixer

import (
	"fmt"
	"sort"
	"sync"

	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/serial"
	"go.uber.org/zap"
)

// SettingsProvider supplies the latest immutable settings and reload signals.
type SettingsProvider interface {
	Values() *config.Settings
	SubscribeToChanges() chan bool
}

// SliderSource broadcasts slider movements to independent consumers.
type SliderSource interface {
	SubscribeToSliderMoveEvents() chan serial.SliderMoveEvent
}

// OBSController controls named OBS inputs.
type OBSController interface {
	IsConnected() bool
	SetInputVolume(string, float32) error
}

// DiscordController accepts integration-local target keys.
type DiscordController interface{ SetVolume(string, float32) }

// SessionInfo is a snapshot of the available audio targets.
type SessionInfo struct {
	Key         string
	DisplayName string
	IsDevice    bool
	IsInput     bool
}

type Mixer struct {
	settings   SettingsProvider
	sliders    SliderSource
	obs        OBSController
	discord    DiscordController
	foreground func(bool) ([]string, error)
	done       chan struct{}
	workers    sync.WaitGroup
	stopOnce   sync.Once
	logger     *zap.SugaredLogger

	m    map[string][]audio.Session
	lock sync.Locker

	sessionFinder audio.SessionFinder

	unmappedSessions []audio.Session

	// channel for notifying about session count changes
	sessionCountChangeChan chan struct{}
}

const (
	masterSessionName = audio.MasterSessionName // master device volume
	systemSessionName = audio.SystemSessionName // system sounds volume
	inputSessionName  = audio.InputSessionName  // microphone input level

	// some targets need to be transformed before their correct audio sessions can be accessed.
	// this prefix identifies those targets to ensure they don't contradict with another similarly-named process
	specialTargetTransformPrefix = "deej."

	// obs targets are handled directly via OBS WebSocket API
	obsTargetPrefix = "deej.obs:"

	// discord targets are handled directly via Discord's local RPC connection.
	// mapping the discord.exe process covers none of these: they reach inside
	// Discord rather than at its audio session
	discordUserTargetPrefix = "deej.discord:"      // one person in the current voice channel
	discordInputTarget      = "deej.discord.input" // own microphone level
	discordOutputTarget     = "deej.discord.output"

	// targets the currently active window (Windows-only, experimental)
	specialTargetCurrentWindow = "current"

	// targets the currently active fullscreen window (Windows-only, experimental)
	specialTargetCurrentFullscreenWindow = "current.fullscreen"

	// targets all currently unmapped sessions (experimental)
	specialTargetAllUnmapped = "unmapped"
)

func New(settings SettingsProvider, sliders SliderSource, logger *zap.SugaredLogger, sessionFinder audio.SessionFinder, obs OBSController, discord DiscordController, foreground func(bool) ([]string, error)) (*Mixer, error) {
	logger = logger.Named("sessions")

	m := &Mixer{
		settings: settings, sliders: sliders, obs: obs, discord: discord, foreground: foreground, done: make(chan struct{}),
		logger:                 logger,
		m:                      make(map[string][]audio.Session),
		lock:                   &sync.Mutex{},
		sessionFinder:          sessionFinder,
		sessionCountChangeChan: make(chan struct{}, 1),
	}

	logger.Debug("Created session map instance")

	return m, nil
}

func (m *Mixer) SubscribeToSessionCountChange() <-chan struct{} {
	return m.sessionCountChangeChan
}

func (m *Mixer) notifySessionCountChange() {
	select {
	case m.sessionCountChangeChan <- struct{}{}:
	default:
		// channel already has a pending notification
	}
}

func (m *Mixer) Initialize() error {
	m.setupOnSliderMove()
	m.setupOnConfigReload()

	// the handler must be registered before the finder starts discovering
	// sessions, otherwise startup enumeration events would be lost
	if err := m.sessionFinder.Start(m.handleSessionEvent); err != nil {
		return fmt.Errorf("start session finder: %w", err)
	}

	return nil
}

func (m *Mixer) Release() error {
	m.stopOnce.Do(func() { close(m.done) })
	m.workers.Wait()
	if err := m.sessionFinder.Release(); err != nil {
		m.logger.Warnw("Failed to release session finder during session map release", "error", err)
		return fmt.Errorf("release session finder during release: %w", err)
	}

	return nil
}

func (m *Mixer) setupOnSliderMove() {
	sliderEventsChannel := m.sliders.SubscribeToSliderMoveEvents()

	m.workers.Go(func() {
		for {
			select {
			case event := <-sliderEventsChannel:
				m.handleSliderMoveEvent(event)
			case <-m.done:
				return
			}
		}
	})
}

// setupOnConfigReload recomputes the unmapped session list whenever the config
// is reloaded, since edits to the slider mapping change which sessions count
// as unmapped
func (m *Mixer) setupOnConfigReload() {
	configReloadedChannel := m.settings.SubscribeToChanges()

	m.workers.Go(func() {
		for {
			select {
			case <-configReloadedChannel:
				m.refreshUnmappedSessions()
			case <-m.done:
				return
			}
		}
	})
}

// refreshUnmappedSessions rebuilds the unmapped session list from the current
// session map and slider mapping
func (m *Mixer) refreshUnmappedSessions() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.unmappedSessions = nil

	for _, sessions := range m.m {
		for _, session := range sessions {
			if !m.sessionMapped(session) {
				m.unmappedSessions = append(m.unmappedSessions, session)
			}
		}
	}

	m.logger.Debugw("Refreshed unmapped sessions after config reload", "count", len(m.unmappedSessions))
}

// handleSessionEvent is called synchronously by the session finder,
// potentially from multiple goroutines
func (m *Mixer) handleSessionEvent(event audio.SessionEvent) {
	switch event.Type {
	case audio.SessionEventAdded:
		m.handleSessionAdded(event)
	case audio.SessionEventRemoved:
		m.handleSessionRemoved(event)
	}
}

func (m *Mixer) handleSessionAdded(event audio.SessionEvent) {
	m.logger.Debugw("audio.Session added event received", "session", event.Session)

	// add to the map and track as unmapped in one locked block, so a
	// concurrent refreshUnmappedSessions can't observe the session in the map
	// and track it a second time
	m.lock.Lock()

	m.addLocked(event.Session)

	if !m.sessionMapped(event.Session) {
		m.logger.Debugw("Tracking unmapped session from event", "session", event.Session)
		m.unmappedSessions = append(m.unmappedSessions, event.Session)
	}

	m.lock.Unlock()

	m.notifySessionCountChange()
}

func (m *Mixer) handleSessionRemoved(event audio.SessionEvent) {
	if event.Session == nil {
		return
	}

	m.logger.Debugw("audio.Session removed event received", "key", event.Session.Key())

	// the finder releases the session as soon as this handler returns. taking
	// the lock here guarantees the session is unreachable by then: it's out of
	// the map, and no setSessionVolumes call is still using it
	m.lock.Lock()

	// Remove from the main map
	m.removeSessionLocked(event.Session)

	// Remove from unmapped sessions if present
	for i, unmapped := range m.unmappedSessions {
		if unmapped == event.Session {
			m.unmappedSessions = append(m.unmappedSessions[:i], m.unmappedSessions[i+1:]...)
			break
		}
	}

	m.lock.Unlock()

	m.notifySessionCountChange()
}

// removeSession removes a specific session from the map
// removeSessionLocked removes a specific session from the map. m.lock must be held
func (m *Mixer) removeSessionLocked(session audio.Session) {
	key := session.Key()
	sessions, ok := m.m[key]
	if !ok {
		return
	}

	// Find and remove the specific session
	for i, s := range sessions {
		if s == session {
			m.m[key] = append(sessions[:i], sessions[i+1:]...)
			break
		}
	}

	// Remove the key entirely if no sessions left
	if len(m.m[key]) == 0 {
		delete(m.m, key)
	}
}

// uniqueStrings returns the input without duplicates, preserving order
func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))

	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	return out
}

// addLocked adds a session to the map. m.lock must be held
func (m *Mixer) addLocked(value audio.Session) {
	key := value.Key()

	existing, ok := m.m[key]
	if !ok {
		m.m[key] = []audio.Session{value}
	} else {
		m.m[key] = append(existing, value)
	}
}

func (m *Mixer) SessionCount() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	count := 0
	for _, sessions := range m.m {
		count += len(sessions)
	}

	return count
}

// sessionInfos returns a sorted copy of the current session keys with
// friendly display names, used by the settings GUI for target suggestions
func (m *Mixer) SessionInfos() []SessionInfo {
	m.lock.Lock()
	defer m.lock.Unlock()

	infos := make([]SessionInfo, 0, len(m.m))
	for key, sessions := range m.m {
		info := SessionInfo{Key: key}
		for _, session := range sessions {
			if session.IsDevice() {
				info.IsDevice = true
			}
			if session.IsInput() {
				info.IsInput = true
			}
			if info.DisplayName == "" {
				info.DisplayName = session.DisplayName()
			}
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key })

	return infos
}

func (m *Mixer) String() string {
	return fmt.Sprintf("<%d audio sessions>", m.SessionCount())
}
