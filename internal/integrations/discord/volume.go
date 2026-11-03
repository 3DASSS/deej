package discord

import (
	"fmt"
	"maps"
	"math"
	"strings"
)

// SetVolume queues a value for a Discord-local input, output, or user:<name> target.
func (d *Client) SetVolume(target string, percent float32) {
	if key, ok := localTargetKey(target); ok {
		d.setVolume(key, percent)
	}
}

func localTargetKey(target string) (string, bool) {
	target = strings.ToLower(target)
	switch {
	case target == InputTarget || target == OutputTarget:
		return target, true
	case strings.HasPrefix(target, UserTargetPrefix):
		return UserTargetPrefix + strings.TrimSpace(strings.TrimPrefix(target, UserTargetPrefix)), true
	default:
		return "", false
	}
}

// SetInputVolume sets the user's own microphone level inside Discord
func (d *Client) SetInputVolume(percent float32) {
	d.setVolume(discordKeyInput, percent)
}

// SetOutputVolume sets Discord's own output level
func (d *Client) SetOutputVolume(percent float32) {
	d.setVolume(discordKeyOutput, percent)
}

// SetUserVolume sets the volume of one person in the current voice channel.
// The name is matched case-insensitively against the roster; an all-digit
// name is taken as a user ID
func (d *Client) SetUserVolume(name string, percent float32) {
	d.setVolume(discordKeyUser+strings.ToLower(strings.TrimSpace(name)), percent)
}

// Targets within Discord use local keys; config target parsing belongs to the mixer.
const (
	InputTarget      = "input"
	OutputTarget     = "output"
	UserTargetPrefix = "user:"
)

// TargetVolume is a current physical slider value for a Discord-local target.
type TargetVolume struct {
	Target  string
	Percent float32
}

// replayMappedSliderValues restores per-connection overrides after the roster is current.
func (d *Client) replayMappedSliderValues() {
	if d.currentVolumes == nil {
		return
	}
	values := d.currentVolumes()
	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()
	for _, value := range values {
		if key, ok := localTargetKey(value.Target); ok {
			d.pending[key] = value.Percent
		}
	}
}

// setVolume records the latest value for a target, which the flush loop picks
// up. Values arriving while disconnected are dropped rather than queued: a
// slider position from ten minutes ago shouldn't be applied on reconnect
func (d *Client) setVolume(key string, percent float32) {
	if !d.IsConnected() {
		return
	}

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	d.pending[key] = percent
}

// flush sends the values collected since the last tick, one request per
// target, skipping the ones Discord already has
func (d *Client) flush() {
	conn, ok := d.reconnector.Current()
	if !ok {
		return
	}

	// the requests below block, so they happen outside the lock
	d.volumeLock.Lock()
	generation := d.volumeGeneration
	pending := d.takePendingLocked()
	d.volumeLock.Unlock()
	sent := map[string]float32{}
	for key, percent := range pending {
		if err := d.send(conn, key, percent); err != nil {
			d.logger.Debugw("Failed to set Discord volume", "target", key, "error", err)
			continue
		}

		sent[key] = percent
	}

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()
	if current, connected := d.reconnector.Current(); connected && current == conn && generation == d.volumeGeneration {
		maps.Copy(d.lastSent, sent)
	}
}

func (d *Client) invalidateVolumes() {
	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()
	clear(d.lastSent)
	d.volumeGeneration++
}

// takePending swaps out everything collected since the last flush, dropping
// the targets Discord already has at that value
func (d *Client) takePending() map[string]float32 {
	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()
	return d.takePendingLocked()
}

func (d *Client) takePendingLocked() map[string]float32 {
	todo := make(map[string]float32, len(d.pending))
	for key, percent := range d.pending {
		if last, sent := d.lastSent[key]; !sent || last != percent {
			todo[key] = percent
		}
	}

	clear(d.pending)

	return todo
}

func (d *Client) markSent(sent map[string]float32) {
	if len(sent) == 0 {
		return
	}

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	maps.Copy(d.lastSent, sent)
}

func (d *Client) send(conn *discordConn, key string, percent float32) error {
	switch key {
	case discordKeyInput:
		_, err := conn.request("SET_VOICE_SETTINGS", map[string]any{"input": map[string]any{"volume": discordVolume(percent)}})
		return err

	case discordKeyOutput:
		_, err := conn.request("SET_VOICE_SETTINGS", map[string]any{"output": map[string]any{"volume": discordVolume(percent)}})
		return err
	}

	name := strings.TrimPrefix(key, discordKeyUser)

	userID, ok := d.resolveUser(name)
	if !ok {
		return fmt.Errorf("no user %q in the current voice channel", name)
	}

	cfg := d.settings.Values().Discord
	_, err := conn.request("SET_USER_VOICE_SETTINGS", map[string]any{
		"user_id": userID,
		"volume":  discordUserVolume(percent, cfg.VolumeConversion),
	})

	return err
}

// discordVolume converts a 0.0-1.0 slider position to Discord's linear input
// and output voice-settings scale.
func discordVolume(percent float32) float64 {
	return math.Round(min(max(float64(percent), 0), 1) * 100)
}

// discordUserVolume maps a 0.0-1.0 slider position onto Discord's participant
// 0-100 participant-volume range. When conversion is enabled, the RPC value
// uses the inverse of Discord's cube-root display curve.
func discordUserVolume(percent float32, convert bool) float64 {
	position := min(max(float64(percent), 0), 1)
	if convert {
		position = position * position * position
	}

	return position * 100
}

// resolveUser maps a mapping target to a Discord user ID. An all-digit target
// is taken as an ID directly, so someone who renames (or whose display name
// deej can't see) can still be pinned in the config
func (d *Client) resolveUser(name string) (string, bool) {
	if name != "" && strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		return name, true
	}

	roster := d.roster.Load()
	if roster == nil {
		return "", false
	}

	for _, user := range roster.users {
		if strings.EqualFold(user.Name, name) {
			return user.ID, true
		}
	}

	return "", false
}
