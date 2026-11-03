package mixer

import (
	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/serial"
	"slices"
	"strings"
)

// returns true if a session is not currently mapped to any slider, false otherwise
// special sessions (master, system, mic) and device-specific sessions always count as mapped,
// even when absent from the config. this makes sense for every current feature that uses "unmapped sessions"
func (m *Mixer) sessionMapped(session audio.Session) bool {

	// count master/system/mic as mapped
	if slices.Contains([]string{masterSessionName, systemSessionName, inputSessionName}, session.Key()) {
		return true
	}

	// count device sessions as mapped
	if session.IsDevice() {
		return true
	}

	// look through the actual mappings
	for _, entry := range m.settings.Values().ActiveMapping() {
		for _, target := range entry.Targets {
			target = strings.ToLower(target)

			// ignore special transforms
			if m.targetHasSpecialTransform(target) {
				continue
			}

			resolvedTargets := m.resolveTarget(target)
			if len(resolvedTargets) == 0 {
				continue
			}
			target = resolvedTargets[0]

			if target == session.Key() {
				return true
			}
		}
	}

	return false
}

func (m *Mixer) handleSliderMoveEvent(event serial.SliderMoveEvent) {

	// get the targets mapped to this slider from the active profile
	targets, ok := m.settings.Values().ActiveMapping().Get(event.SliderID)

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
func (m *Mixer) setSessionVolumes(target string, volume float32) {
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
// (e.g. OBS and Discord).
// Returns true if the target was handled, false if it should be treated as a normal audio target.
func (m *Mixer) applySpecialTargetAction(target string, volume float32) bool {
	lowerTarget := strings.ToLower(target)
	discordKey, isDiscordTarget := DiscordTargetKey(target)

	switch {
	case strings.HasPrefix(lowerTarget, obsTargetPrefix):
		inputName := target[len(obsTargetPrefix):]
		m.handleOBSTarget(inputName, volume)
		return true

	case isDiscordTarget:
		if m.discord != nil {
			m.discord.SetVolume(discordKey, volume)
		}
		return true
	}

	return false
}

func (m *Mixer) handleOBSTarget(inputName string, volume float32) {
	if m.obs == nil || !m.obs.IsConnected() {
		return
	}

	if err := m.obs.SetInputVolume(inputName, volume); err != nil {
		m.logger.Debugw("Failed to set OBS input volume", "input", inputName, "error", err)
	}
}

func (m *Mixer) targetHasSpecialTransform(target string) bool {
	return strings.HasPrefix(target, specialTargetTransformPrefix)
}

func (m *Mixer) resolveTarget(target string) []string {

	// start by ignoring the case
	target = strings.ToLower(target)

	// look for any special targets first, by examining the prefix
	if m.targetHasSpecialTransform(target) {
		return m.applyTargetTransform(strings.TrimPrefix(target, specialTargetTransformPrefix))
	}

	return []string{target}
}

func (m *Mixer) applyTargetTransform(specialTargetName string) []string {
	checkFullscreen := false

	// select the transformation based on its name
	switch specialTargetName {

	// get current active fullscreen window
	case specialTargetCurrentFullscreenWindow:
		checkFullscreen = true
		fallthrough

	// get current active window
	case specialTargetCurrentWindow:
		currentWindowProcessNames, err := m.foreground(checkFullscreen)

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

// DiscordTargetKey parses config syntax into a Discord-local target key.
func DiscordTargetKey(target string) (string, bool) {
	lower := strings.ToLower(target)
	switch {
	case lower == discordInputTarget:
		return "input", true
	case lower == discordOutputTarget:
		return "output", true
	case strings.HasPrefix(lower, discordUserTargetPrefix):
		return "user:" + strings.TrimSpace(lower[len(discordUserTargetPrefix):]), true
	default:
		return "", false
	}
}
