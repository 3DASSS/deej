package discord

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// SubscribeToRosterChange returns a channel that signals whenever the voice
// channel roster changes. Signals are coalesced - consumers should re-read
// VoiceUsers rather than count them
func (d *Client) SubscribeToRosterChange() <-chan struct{} {
	return d.rosterChangeChannel
}

func (d *Client) notifyRosterChange() {
	select {
	case d.rosterChangeChannel <- struct{}{}:
	default:
		// a signal is already pending, which says everything it needs to
	}
}

// VoiceUsers returns the members of the voice channel the user is currently
// in, for the settings window's target picker
func (d *Client) VoiceUsers() []User {
	roster := d.roster.Load()
	if roster == nil {
		return nil
	}

	return slices.Clone(roster.users)
}

// discordRoster is a snapshot of the current voice channel
type discordRoster struct {
	id      string // channel id, the scope the voice state subscriptions use
	channel string // channel name, for display
	users   []User
}

// discordVoiceState is one member's entry, both as VOICE_STATE_* events carry
// it and as GET_SELECTED_VOICE_CHANNEL nests it under voice_states
type discordVoiceState struct {
	Nick string `json:"nick"`
	User struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	} `json:"user"`
}

func (s discordVoiceState) toUser() User {
	// the per-server nickname is what the user sees in the client, so it's
	// what they'd reach for when writing a mapping
	return User{
		ID:   s.User.ID,
		Name: cmp.Or(s.Nick, s.User.GlobalName, s.User.Username),
	}
}

// queueEvent hands an RPC event to the event loop. It is called from a
// connection's read loop, so it drops rather than blocks when the buffer is
// full - the periodic reconcile repairs whatever gets dropped
func (d *Client) queueEvent(evt string, data json.RawMessage) {
	select {
	case d.events <- discordEvent{name: evt, data: data}:
	default:
		d.logger.Debugw("Discord event buffer full, dropping event", "event", evt)
	}
}

// eventLoop owns the roster. Every write to it happens here - from RPC events,
// from the periodic reconcile, and from a connection coming up - so the roster
// needs no lock beyond the atomic pointer readers use
func (d *Client) eventLoop(stopChannel <-chan struct{}) {
	defer func() {
		d.roster.Store(nil)
		d.notifyRosterChange()
	}()
	ticker := time.NewTicker(discordReconcileInterval)
	defer ticker.Stop()

	// the connection the current subscriptions belong to, and the channel
	// those subscriptions are scoped to. A new connection carries neither,
	// which is why the connection's identity is what's tracked here
	var subscribedConn *discordConn
	var subscribedChannel string

	for {
		select {
		case <-stopChannel:
			return

		case <-d.reconcile:
			d.reconcileRoster(&subscribedConn, &subscribedChannel)

		case <-ticker.C:
			d.reconcileRoster(&subscribedConn, &subscribedChannel)

		case event := <-d.events:
			// changing voice channel moves the subscriptions with it, which
			// the reconcile pass handles along with reading the new roster
			if event.name == "VOICE_CHANNEL_SELECT" {
				d.reconcileRoster(&subscribedConn, &subscribedChannel)
				continue
			}

			d.applyVoiceStateEvent(event)
		}
	}
}

// applyVoiceStateEvent folds one VOICE_STATE_* event into the roster
func (d *Client) applyVoiceStateEvent(event discordEvent) {
	var state discordVoiceState
	if err := json.Unmarshal(event.data, &state); err != nil || state.User.ID == "" {
		return
	}

	roster := d.roster.Load()
	if roster == nil {
		return
	}

	user := state.toUser()

	users := slices.Clone(roster.users)
	index := slices.IndexFunc(users, func(existing User) bool { return existing.ID == user.ID })
	joined := index < 0 && event.name != "VOICE_STATE_DELETE"

	switch event.name {
	case "VOICE_STATE_DELETE":
		if index < 0 {
			return
		}
		users = slices.Delete(users, index, index+1)

	default: // CREATE and UPDATE both mean "this is the member's current state"
		if index >= 0 {
			if users[index] == user {
				return
			}
			users[index] = user
		} else {
			users = append(users, user)
		}
	}

	sortDiscordUsers(users)

	d.roster.Store(&discordRoster{id: roster.id, channel: roster.channel, users: users})
	d.notifyRosterChange()
	if joined {
		// Discord may have discarded a user's override while they were away.
		d.invalidateVolumes()
		d.replayMappedSliderValues()
	}
}

// reconcileRoster re-reads the voice channel from scratch and re-points the
// subscriptions at it. It runs on connect, whenever the user changes voice
// channel, and periodically to repair anything a dropped event missed
func (d *Client) reconcileRoster(subscribedConn **discordConn, subscribedChannel *string) {
	conn, ok := d.reconnector.Current()
	if !ok {
		*subscribedConn = nil
		*subscribedChannel = ""

		if d.roster.Swap(nil) != nil {
			d.notifyRosterChange()
		}

		return
	}

	// a new connection carries none of the old one's subscriptions
	if conn != *subscribedConn {
		*subscribedConn = conn
		*subscribedChannel = ""

		if _, err := conn.requestEvent("SUBSCRIBE", "VOICE_CHANNEL_SELECT", nil); err != nil {
			d.logger.Debugw("Failed to subscribe to Discord voice channel changes", "error", err)
		}
	}

	channel, err := d.readVoiceChannel(conn)
	if err != nil {
		d.logger.Debugw("Failed to read Discord voice channel", "error", err)
		return
	}
	if current, connected := d.reconnector.Current(); !connected || current != conn {
		return
	}

	if channel.id != *subscribedChannel {
		if d.resubscribeVoiceStates(conn, *subscribedChannel, channel.id) {
			*subscribedChannel = channel.id
		}
	}

	previous := d.roster.Load()
	// A changed channel, or a join recovered by reconciliation, may have lost
	// Discord's per-user overrides even when the physical sliders did not move.
	if rosterNeedsReplay(previous, channel) {
		d.invalidateVolumes()
	}
	d.roster.Store(channel)
	d.replayMappedSliderValues()

	if previous == nil || previous.channel != channel.channel || !slices.Equal(previous.users, channel.users) {
		d.notifyRosterChange()
	}
}

func rosterNeedsReplay(previous, current *discordRoster) bool {
	if previous == nil || previous.id != current.id {
		return true
	}
	for _, user := range current.users {
		if !slices.ContainsFunc(previous.users, func(old User) bool { return old.ID == user.ID }) {
			return true
		}
	}
	return false
}

// readVoiceChannel fetches the full current voice channel state
func (d *Client) readVoiceChannel(conn *discordConn) (*discordRoster, error) {
	data, err := conn.request("GET_SELECTED_VOICE_CHANNEL", nil)
	if err != nil {
		return nil, err
	}

	// null when the user isn't in a voice channel
	var channel *struct {
		ID          string              `json:"id"`
		Name        string              `json:"name"`
		VoiceStates []discordVoiceState `json:"voice_states"`
	}

	if err := json.Unmarshal(data, &channel); err != nil {
		return nil, fmt.Errorf("parse voice channel: %w", err)
	}

	if channel == nil {
		return &discordRoster{}, nil
	}

	roster := &discordRoster{id: channel.ID, channel: channel.Name}
	for _, state := range channel.VoiceStates {
		roster.users = append(roster.users, state.toUser())
	}

	sortDiscordUsers(roster.users)

	return roster, nil
}

// resubscribeVoiceStates moves the per-channel event subscriptions from one
// voice channel to another; either id may be empty, meaning "not in a channel"
func (d *Client) resubscribeVoiceStates(conn *discordConn, from string, to string) bool {
	events := []string{"VOICE_STATE_CREATE", "VOICE_STATE_UPDATE", "VOICE_STATE_DELETE"}
	subscribed := true

	for _, name := range events {
		if from != "" {
			if _, err := conn.requestEvent("UNSUBSCRIBE", name, map[string]any{"channel_id": from}); err != nil {
				d.logger.Debugw("Failed to unsubscribe from Discord voice states", "event", name, "error", err)
			}
		}

		if to != "" {
			if _, err := conn.requestEvent("SUBSCRIBE", name, map[string]any{"channel_id": to}); err != nil {
				d.logger.Debugw("Failed to subscribe to Discord voice states", "event", name, "error", err)
				subscribed = false
			}
		}
	}

	return subscribed
}

func sortDiscordUsers(users []User) {
	slices.SortFunc(users, func(a, b User) int {
		// case-insensitively, or a plain byte compare would file every
		// capitalised display name ahead of every lowercase one
		if diff := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); diff != 0 {
			return diff
		}

		// discord display names are not unique, so keep the order stable
		return cmp.Compare(a.ID, b.ID)
	})
}
