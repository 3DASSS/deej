package deej

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/nik9play/deej/pkg/deej/util"
	"go.uber.org/zap"
)

type sessionMap struct {
	deej   *Deej
	logger *zap.SugaredLogger

	m    map[string][]Session
	lock sync.Locker

	sessionFinder SessionFinder

	unmappedSessions []Session

	// channel for notifying about session count changes
	sessionCountChangeChan chan struct{}
}

const (
	masterSessionName = "master" // master device volume
	systemSessionName = "system" // system sounds volume
	inputSessionName  = "mic"    // microphone input level

	// some targets need to be transformed before their correct audio sessions can be accessed.
	// this prefix identifies those targets to ensure they don't contradict with another similarly-named process
	specialTargetTransformPrefix = "deej."

	// obs targets are handled directly via OBS WebSocket API
	obsTargetPrefix = "deej.obs:"

	// targets the currently active window (Windows-only, experimental)
	specialTargetCurrentWindow = "current"

	// targets the currently active fullscreen window (Windows-only, experimental)
	specialTargetCurrentFullscreenWindow = "current.fullscreen"

	// targets all currently unmapped sessions (experimental)
	specialTargetAllUnmapped = "unmapped"
)

func newSessionMap(deej *Deej, logger *zap.SugaredLogger, sessionFinder SessionFinder) (*sessionMap, error) {
	logger = logger.Named("sessions")

	m := &sessionMap{
		deej:                   deej,
		logger:                 logger,
		m:                      make(map[string][]Session),
		lock:                   &sync.Mutex{},
		sessionFinder:          sessionFinder,
		sessionCountChangeChan: make(chan struct{}, 1),
	}

	logger.Debug("Created session map instance")

	return m, nil
}

func (m *sessionMap) SubscribeToSessionCountChange() <-chan struct{} {
	return m.sessionCountChangeChan
}

func (m *sessionMap) notifySessionCountChange() {
	select {
	case m.sessionCountChangeChan <- struct{}{}:
	default:
		// channel already has a pending notification
	}
}

func (m *sessionMap) initialize() error {
	m.setupOnSliderMove()
	m.setupOnConfigReload()

	// the handler must be registered before the finder starts discovering
	// sessions, otherwise startup enumeration events would be lost
	if err := m.sessionFinder.Start(m.handleSessionEvent); err != nil {
		return fmt.Errorf("start session finder: %w", err)
	}

	return nil
}

func (m *sessionMap) release() error {
	if err := m.sessionFinder.Release(); err != nil {
		m.logger.Warnw("Failed to release session finder during session map release", "error", err)
		return fmt.Errorf("release session finder during release: %w", err)
	}

	return nil
}

func (m *sessionMap) setupOnSliderMove() {
	sliderEventsChannel := m.deej.serial.SubscribeToSliderMoveEvents()

	go func() {
		for {
			event := <-sliderEventsChannel
			m.handleSliderMoveEvent(event)
		}
	}()
}

// setupOnConfigReload recomputes the unmapped session list whenever the config
// is reloaded, since edits to the slider mapping change which sessions count
// as unmapped
func (m *sessionMap) setupOnConfigReload() {
	configReloadedChannel := m.deej.config.SubscribeToChanges()

	go func() {
		for range configReloadedChannel {
			m.refreshUnmappedSessions()
		}
	}()
}

// refreshUnmappedSessions rebuilds the unmapped session list from the current
// session map and slider mapping
func (m *sessionMap) refreshUnmappedSessions() {
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
func (m *sessionMap) handleSessionEvent(event SessionEvent) {
	switch event.Type {
	case SessionEventAdded:
		m.handleSessionAdded(event)
	case SessionEventRemoved:
		m.handleSessionRemoved(event)
	}
}

func (m *sessionMap) handleSessionAdded(event SessionEvent) {
	m.logger.Debugw("Session added event received", "session", event.Session)

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

func (m *sessionMap) handleSessionRemoved(event SessionEvent) {
	if event.Session == nil {
		return
	}

	m.logger.Debugw("Session removed event received", "key", event.Session.Key())

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
func (m *sessionMap) removeSession(session Session) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.removeSessionLocked(session)
}

// removeSessionLocked removes a specific session from the map. m.lock must be held
func (m *sessionMap) removeSessionLocked(session Session) {
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

// returns true if a session is not currently mapped to any slider, false otherwise
// special sessions (master, system, mic) and device-specific sessions always count as mapped,
// even when absent from the config. this makes sense for every current feature that uses "unmapped sessions"
func (m *sessionMap) sessionMapped(session Session) bool {

	// count master/system/mic as mapped
	if slices.Contains([]string{masterSessionName, systemSessionName, inputSessionName}, session.Key()) {
		return true
	}

	// count device sessions as mapped
	if session.IsDevice() {
		return true
	}

	matchFound := false

	// look through the actual mappings
	m.deej.config.Values().SliderMapping.iterate(func(_ int, targets []string) {
		for _, target := range targets {

			// ignore special transforms
			if m.targetHasSpecialTransform(target) {
				continue
			}

			// safe to assume this has a single element because we made sure there's no special transform
			target = m.resolveTarget(target)[0]

			if target == session.Key() {
				matchFound = true
				return
			}
		}
	})

	return matchFound
}

func (m *sessionMap) handleSliderMoveEvent(event SliderMoveEvent) {

	// get the targets mapped to this slider from the config
	targets, ok := m.deej.config.Values().SliderMapping.get(event.SliderID)

	// if slider not found in config, silently ignore
	if !ok {
		return
	}

	// for each possible target for this slider...
	for _, target := range targets {

		// handle special action targets (OBS, etc.) that don't map to audio sessions
		if m.applySpecialTargetAction(target, event.PercentValue) {
			continue
		}

		// resolve the target name by cleaning it up and applying any special transformations.
		// depending on the transformation applied, this can result in more than one target name
		resolvedTargets := m.resolveTarget(target)

		// for each resolved target...
		for _, resolvedTarget := range resolvedTargets {
			m.setSessionVolumes(resolvedTarget, event.PercentValue)
		}
	}
}

// setSessionVolumes adjusts the volume of every session matching the target.
// it holds the lock for the duration of the adjustment so a session can't be
// removed (and released) while its volume is being set
func (m *sessionMap) setSessionVolumes(target string, volume float32) {
	m.lock.Lock()
	defer m.lock.Unlock()

	sessions, ok := m.m[target]

	// no sessions matching this target - move on
	if !ok {
		return
	}

	// iterate all matching sessions and adjust the volume of each one
	for _, session := range sessions {
		if session.GetVolume() != volume {
			if err := session.SetVolume(volume); err != nil {
				m.logger.Warnw("Failed to set target session volume", "error", err)
			}
		}
	}
}

// applySpecialTargetAction handles targets that control external systems rather than audio sessions
// (e.g. OBS, and potentially Discord or others in the future).
// Returns true if the target was handled, false if it should be treated as a normal audio target.
func (m *sessionMap) applySpecialTargetAction(target string, volume float32) bool {
	switch {
	case strings.HasPrefix(strings.ToLower(target), obsTargetPrefix):
		inputName := target[len(obsTargetPrefix):]
		m.handleOBSTarget(inputName, volume)
		return true
	}

	return false
}

func (m *sessionMap) handleOBSTarget(inputName string, volume float32) {
	if m.deej.obs == nil || !m.deej.obs.IsConnected() {
		return
	}

	if err := m.deej.obs.SetInputVolume(inputName, volume); err != nil {
		m.logger.Debugw("Failed to set OBS input volume", "input", inputName, "error", err)
	}
}

func (m *sessionMap) targetHasSpecialTransform(target string) bool {
	return strings.HasPrefix(target, specialTargetTransformPrefix)
}

func (m *sessionMap) resolveTarget(target string) []string {

	// start by ignoring the case
	target = strings.ToLower(target)

	// look for any special targets first, by examining the prefix
	if m.targetHasSpecialTransform(target) {
		return m.applyTargetTransform(strings.TrimPrefix(target, specialTargetTransformPrefix))
	}

	return []string{target}
}

func (m *sessionMap) applyTargetTransform(specialTargetName string) []string {
	checkFullscreen := false

	// select the transformation based on its name
	switch specialTargetName {

	// get current active fullscreen window
	case specialTargetCurrentFullscreenWindow:
		checkFullscreen = true
		fallthrough

	// get current active window
	case specialTargetCurrentWindow:
		currentWindowProcessNames, err := util.GetCurrentWindowProcessNames(checkFullscreen)

		// silently ignore errors here, as this is on deej's "hot path" (and it could just mean the user's running linux)
		if err != nil {
			return nil
		}

		// we could have gotten a non-lowercase names from that, so let's ensure we return ones that are lowercase
		for targetIdx, target := range currentWindowProcessNames {
			currentWindowProcessNames[targetIdx] = strings.ToLower(target)
		}

		// remove dupes, preserving order
		return uniqueStrings(currentWindowProcessNames)

	// get currently unmapped sessions
	case specialTargetAllUnmapped:
		// the unmapped session list is mutated by the session event and config
		// reload handlers, so it must be read under the lock
		m.lock.Lock()
		targetKeys := make([]string, len(m.unmappedSessions))
		for sessionIdx, session := range m.unmappedSessions {
			targetKeys[sessionIdx] = session.Key()
		}
		m.lock.Unlock()

		return targetKeys
	}

	return nil
}

func (m *sessionMap) add(value Session) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.addLocked(value)
}

// addLocked adds a session to the map. m.lock must be held
func (m *sessionMap) addLocked(value Session) {
	key := value.Key()

	existing, ok := m.m[key]
	if !ok {
		m.m[key] = []Session{value}
	} else {
		m.m[key] = append(existing, value)
	}
}

func (m *sessionMap) get(key string) ([]Session, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	value, ok := m.m[key]
	return value, ok
}

func (m *sessionMap) getSessionCount() int {
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
func (m *sessionMap) sessionInfos() []SessionInfoDTO {
	m.lock.Lock()
	defer m.lock.Unlock()

	infos := make([]SessionInfoDTO, 0, len(m.m))
	for key, sessions := range m.m {
		info := SessionInfoDTO{Key: key}
		for _, session := range sessions {
			if session.IsDevice() {
				info.IsDevice = true
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

func (m *sessionMap) String() string {
	return fmt.Sprintf("<%d audio sessions>", m.getSessionCount())
}
