package deej

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/nik9play/deej/pkg/reconnect"
)

const (
	discordRetryDelay = 5 * time.Second

	// the roster is kept current by VOICE_STATE_* events, so this is only the
	// safety net that repairs it if one is ever missed or dropped
	discordReconcileInterval = 30 * time.Second

	// how many roster events may queue before they're dropped. Dropping is
	// safe: the reconcile pass above repairs whatever was missed, and never
	// blocking here is what keeps the read loop from deadlocking against the
	// event handler's own requests
	discordEventBufferSize = 64

	// minimum spacing between volume commands. A slider sweep produces tens of
	// events a second, which would trip Discord's RPC rate limit if each one
	// became a request
	discordFlushInterval = 100 * time.Millisecond

	// how long to wait for the reply to a single RPC command
	discordCommandTimeout = 10 * time.Second

	// authorizing means clicking a button in the Discord client, so this one
	// waits on a human rather than on software
	discordAuthorizeTimeout = 2 * time.Minute

	discordTokenURL = "https://discord.com/api/oauth2/token"

	// deej never receives this redirect - the authorization code comes back
	// over the IPC connection - but the token exchange still has to echo a URI
	// the application has registered
	discordRedirectURI = "http://localhost"

	discordTokenFileName = "discord_token.json"
)

// the scopes deej asks for: rpc to talk to the client at all, rpc.voice.read
// to see who's in the voice channel, rpc.voice.write to set volumes
var discordScopes = []string{"rpc", "rpc.voice.read", "rpc.voice.write"}

// IPC frame opcodes
const (
	discordOpHandshake uint32 = 0
	discordOpFrame     uint32 = 1
	discordOpClose     uint32 = 2
	discordOpPing      uint32 = 3
	discordOpPong      uint32 = 4
)

// pending volume keys, also the throttle's map keys
const (
	discordKeyInput  = "input"
	discordKeyOutput = "output"
	discordKeyUser   = "user:"
)

var discordHTTPClient = &http.Client{Timeout: 15 * time.Second}

// DiscordClient controls volumes inside the Discord client over its local RPC
// connection: the volume of individual people in a voice channel, and the
// user's own input and output levels. Mapping the discord.exe process covers
// neither of those, which is the whole reason this exists
type DiscordClient struct {
	deej   *Deej
	logger *zap.SugaredLogger

	reconnector *reconnect.Reconnector[*discordConn]

	token  atomic.Pointer[discordToken]
	roster atomic.Pointer[discordRoster]

	// volumeLock guards the throttle's two maps: pending is what the slider
	// handler wrote, lastSent is what actually reached Discord
	volumeLock sync.Mutex
	pending    map[string]float32
	lastSent   map[string]float32

	// every roster write happens on the event loop, so the roster pointer
	// above needs no lock of its own
	events    chan discordEvent
	reconcile chan struct{}

	rosterChangeChannel chan struct{}

	stopChannel chan struct{}
}

// discordEvent is one RPC event, moved off the read loop so the handler can
// issue its own requests without deadlocking against it
type discordEvent struct {
	name string
	data json.RawMessage
}

func NewDiscordClient(deej *Deej, logger *zap.SugaredLogger) *DiscordClient {
	logger = logger.Named("discord")

	d := &DiscordClient{
		deej:                deej,
		logger:              logger,
		pending:             map[string]float32{},
		lastSent:            map[string]float32{},
		events:              make(chan discordEvent, discordEventBufferSize),
		reconcile:           make(chan struct{}, 1),
		rosterChangeChannel: make(chan struct{}, 1),
	}

	d.reconnector = reconnect.New(reconnect.Options[*discordConn]{
		Logger: logger,

		// without a stored token the only way forward is the interactive
		// authorization, which must not happen on a retry loop
		Enabled: func() bool {
			cfg := d.deej.config.Values().Discord
			return cfg.Enabled && cfg.ClientID != "" && d.token.Load() != nil
		},

		Dial:  d.dial,
		Watch: d.watch,
		Close: d.close,
		OnUp:  d.onUp,
		OnDown: func(err error) {
			d.logger.Warnw("Discord connection error, reconnecting...", "error", err)
			d.roster.Store(nil)
			d.notifyRosterChange()
		},
		Backoff: func(int) time.Duration { return discordRetryDelay },
	})

	logger.Debug("Created Discord client instance")

	return d
}

func (d *DiscordClient) Start() {
	d.logger.Info("Discord client starting")

	// NewDiscordClient is constructed before the initial config load.
	d.loadToken()
	d.setupOnConfigReload()

	d.stopChannel = make(chan struct{})
	d.reconnector.Start()

	// the event loop issues blocking requests of its own, so it doesn't share
	// a goroutine with the volume flush
	go discordTickLoop(d.stopChannel, discordFlushInterval, d.flush)
	go d.eventLoop(d.stopChannel)
}

// SubscribeToRosterChange returns a channel that signals whenever the voice
// channel roster changes. Signals are coalesced - consumers should re-read
// VoiceUsers rather than count them
func (d *DiscordClient) SubscribeToRosterChange() <-chan struct{} {
	return d.rosterChangeChannel
}

func (d *DiscordClient) notifyRosterChange() {
	select {
	case d.rosterChangeChannel <- struct{}{}:
	default:
		// a signal is already pending, which says everything it needs to
	}
}

func (d *DiscordClient) Stop() {
	if d.stopChannel != nil {
		close(d.stopChannel)
		d.stopChannel = nil
	}

	d.reconnector.Stop()
	d.logger.Info("Discord client stopped")
}

func (d *DiscordClient) IsConnected() bool {
	return d.reconnector.Connected()
}

func discordTickLoop(stopChannel <-chan struct{}, interval time.Duration, job func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChannel:
			return
		case <-ticker.C:
			job()
		}
	}
}

// Status reports what the settings window shows: whether an account has been
// linked, whether the connection is up, and the voice channel deej can see
func (d *DiscordClient) Status() (linked bool, connected bool, channel string) {
	linked = d.token.Load() != nil
	connected = d.reconnector.Connected()

	if roster := d.roster.Load(); roster != nil {
		channel = roster.channel
	}

	return linked, connected, channel
}

// VoiceUsers returns the members of the voice channel the user is currently
// in, for the settings window's target picker
func (d *DiscordClient) VoiceUsers() []DiscordUserDTO {
	roster := d.roster.Load()
	if roster == nil {
		return nil
	}

	return slices.Clone(roster.users)
}

// SetInputVolume sets the user's own microphone level inside Discord
func (d *DiscordClient) SetInputVolume(percent float32) {
	d.setVolume(discordKeyInput, percent)
}

// SetOutputVolume sets Discord's own output level
func (d *DiscordClient) SetOutputVolume(percent float32) {
	d.setVolume(discordKeyOutput, percent)
}

// SetUserVolume sets the volume of one person in the current voice channel.
// The name is matched case-insensitively against the roster; an all-digit
// name is taken as a user ID
func (d *DiscordClient) SetUserVolume(name string, percent float32) {
	d.setVolume(discordKeyUser+strings.ToLower(strings.TrimSpace(name)), percent)
}

func discordTargetKey(target string) (string, bool) {
	lowerTarget := strings.ToLower(target)

	switch {
	case lowerTarget == discordInputTarget:
		return discordKeyInput, true
	case lowerTarget == discordOutputTarget:
		return discordKeyOutput, true
	case strings.HasPrefix(lowerTarget, discordUserTargetPrefix):
		return discordKeyUser + strings.ToLower(strings.TrimSpace(target[len(discordUserTargetPrefix):])), true
	default:
		return "", false
	}
}

// replayMappedSliderValues restores Discord's per-connection voice overrides
// from the physical sliders after the roster is current.
func (d *DiscordClient) replayMappedSliderValues() {
	if d.deej == nil || d.deej.config == nil || d.deej.serial == nil {
		return
	}

	values := d.deej.serial.CurrentSliderPercentValues()
	mapping := d.deej.config.Values().ActiveMapping()

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	for _, entry := range mapping {
		if entry.Slider < 0 || entry.Slider >= len(values) {
			continue
		}

		for _, target := range entry.Targets {
			if key, ok := discordTargetKey(target); ok {
				d.pending[key] = values[entry.Slider]
			}
		}
	}
}

// setVolume records the latest value for a target, which the flush loop picks
// up. Values arriving while disconnected are dropped rather than queued: a
// slider position from ten minutes ago shouldn't be applied on reconnect
func (d *DiscordClient) setVolume(key string, percent float32) {
	if !d.IsConnected() {
		return
	}

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	d.pending[key] = percent
}

// flush sends the values collected since the last tick, one request per
// target, skipping the ones Discord already has
func (d *DiscordClient) flush() {
	conn, ok := d.reconnector.Current()
	if !ok {
		return
	}

	// the requests below block, so they happen outside the lock
	sent := map[string]float32{}
	for key, percent := range d.takePending() {
		if err := d.send(conn, key, percent); err != nil {
			d.logger.Debugw("Failed to set Discord volume", "target", key, "error", err)
			continue
		}

		sent[key] = percent
	}

	d.markSent(sent)
}

// takePending swaps out everything collected since the last flush, dropping
// the targets Discord already has at that value
func (d *DiscordClient) takePending() map[string]float32 {
	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	todo := make(map[string]float32, len(d.pending))
	for key, percent := range d.pending {
		if last, sent := d.lastSent[key]; !sent || last != percent {
			todo[key] = percent
		}
	}

	clear(d.pending)

	return todo
}

func (d *DiscordClient) markSent(sent map[string]float32) {
	if len(sent) == 0 {
		return
	}

	d.volumeLock.Lock()
	defer d.volumeLock.Unlock()

	maps.Copy(d.lastSent, sent)
}

func (d *DiscordClient) send(conn *discordConn, key string, percent float32) error {
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

	cfg := d.deej.config.Values().Discord
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
func (d *DiscordClient) resolveUser(name string) (string, bool) {
	if isDigits(name) {
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

// discordRoster is a snapshot of the current voice channel
type discordRoster struct {
	id      string // channel id, the scope the voice state subscriptions use
	channel string // channel name, for display
	users   []DiscordUserDTO
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

func (s discordVoiceState) toUser() DiscordUserDTO {
	// the per-server nickname is what the user sees in the client, so it's
	// what they'd reach for when writing a mapping
	return DiscordUserDTO{
		ID:   s.User.ID,
		Name: cmp.Or(s.Nick, s.User.GlobalName, s.User.Username),
	}
}

// queueEvent hands an RPC event to the event loop. It is called from a
// connection's read loop, so it drops rather than blocks when the buffer is
// full - the periodic reconcile repairs whatever gets dropped
func (d *DiscordClient) queueEvent(evt string, data json.RawMessage) {
	select {
	case d.events <- discordEvent{name: evt, data: data}:
	default:
		d.logger.Debugw("Discord event buffer full, dropping event", "event", evt)
	}
}

// eventLoop owns the roster. Every write to it happens here - from RPC events,
// from the periodic reconcile, and from a connection coming up - so the roster
// needs no lock beyond the atomic pointer readers use
func (d *DiscordClient) eventLoop(stopChannel <-chan struct{}) {
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
func (d *DiscordClient) applyVoiceStateEvent(event discordEvent) {
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
	index := slices.IndexFunc(users, func(existing DiscordUserDTO) bool { return existing.ID == user.ID })
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
		d.volumeLock.Lock()
		clear(d.lastSent)
		d.volumeLock.Unlock()
		d.replayMappedSliderValues()
	}
}

// reconcileRoster re-reads the voice channel from scratch and re-points the
// subscriptions at it. It runs on connect, whenever the user changes voice
// channel, and periodically to repair anything a dropped event missed
func (d *DiscordClient) reconcileRoster(subscribedConn **discordConn, subscribedChannel *string) {
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

	if channel.id != *subscribedChannel {
		if d.resubscribeVoiceStates(conn, *subscribedChannel, channel.id) {
			*subscribedChannel = channel.id
		}
	}

	previous := d.roster.Load()
	d.roster.Store(channel)
	d.replayMappedSliderValues()

	if previous == nil || previous.channel != channel.channel || !slices.Equal(previous.users, channel.users) {
		d.notifyRosterChange()
	}
}

// readVoiceChannel fetches the full current voice channel state
func (d *DiscordClient) readVoiceChannel(conn *discordConn) (*discordRoster, error) {
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
func (d *DiscordClient) resubscribeVoiceStates(conn *discordConn, from string, to string) bool {
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

func sortDiscordUsers(users []DiscordUserDTO) {
	slices.SortFunc(users, func(a, b DiscordUserDTO) int {
		// case-insensitively, or a plain byte compare would file every
		// capitalised display name ahead of every lowercase one
		if diff := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); diff != 0 {
			return diff
		}

		// discord display names are not unique, so keep the order stable
		return cmp.Compare(a.ID, b.ID)
	})
}

// Link runs the one interactive step of the integration: Discord shows the
// user a consent dialog, and approving it hands back an authorization code
// that deej exchanges for a token it can reuse from now on. This is deliberately
// not part of dial - a retry every few seconds would mean a popup every few
// seconds - so it lives behind the settings window's button
func (d *DiscordClient) Link() error {
	cfg := d.deej.config.Values().Discord

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("save a Discord client ID and secret first")
	}

	conn, err := d.open(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	data, err := conn.requestWithTimeout("AUTHORIZE", map[string]any{
		"client_id": cfg.ClientID,
		"scopes":    discordScopes,
	}, discordAuthorizeTimeout)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}

	var authorized struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &authorized); err != nil || authorized.Code == "" {
		return errors.New("discord returned no authorization code")
	}

	token, err := exchangeToken(cfg, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {authorized.Code},
		"redirect_uri": {discordRedirectURI},
	})
	if err != nil {
		return err
	}

	if err := d.storeToken(token); err != nil {
		return err
	}

	d.logger.Info("Linked Discord account")
	d.reconnector.Reconnect(errors.New("account linked"))

	return nil
}

// Unlink forgets the stored token, which also drops the connection
func (d *DiscordClient) Unlink() error {
	d.token.Store(nil)

	if err := os.Remove(d.tokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Discord token: %w", err)
	}

	d.logger.Info("Unlinked Discord account")
	d.reconnector.Reconnect(errors.New("account unlinked"))

	return nil
}

// dial establishes an authenticated connection using the stored token,
// without touching any client state - the reconnector decides whether the
// result is adopted
func (d *DiscordClient) dial() (*discordConn, error) {
	cfg := d.deej.config.Values().Discord

	token := d.token.Load()
	if token == nil {
		return nil, errors.New("no linked Discord account")
	}

	// refresh before opening anything, so an expired token doesn't cost a
	// connection that's about to be rejected
	token, err := d.ensureFreshToken(cfg, token)
	if err != nil {
		return nil, err
	}

	conn, err := d.open(cfg)
	if err != nil {
		return nil, err
	}

	if _, err := conn.request("AUTHENTICATE", map[string]any{"access_token": token.AccessToken}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	return conn, nil
}

// open connects to the Discord client and completes the handshake, leaving a
// connection that can carry commands but hasn't authenticated yet
func (d *DiscordClient) open(cfg DiscordSettings) (*discordConn, error) {
	rw, err := dialDiscordIPC()
	if err != nil {
		return nil, err
	}

	conn := newDiscordConn(rw, cfg, d.queueEvent)

	handshake, err := json.Marshal(map[string]any{"v": 1, "client_id": cfg.ClientID})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err := conn.write(discordOpHandshake, handshake); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	select {
	case <-conn.ready:
		return conn, nil

	// the connection failed before it was ever adopted, so taking its error
	// here doesn't strand a Watch
	case <-conn.done:
		_ = conn.Close()
		return nil, fmt.Errorf("handshake: %w", conn.doneErr)

	case <-time.After(discordCommandTimeout):
		_ = conn.Close()
		return nil, errors.New("timed out waiting for the Discord handshake")
	}
}

// watch waits for this connection's read loop to finish, which happens either
// on a connection error or because Close was called
func (d *DiscordClient) watch(conn *discordConn, errChannel chan<- error) {
	<-conn.done

	select {
	case errChannel <- conn.doneErr:
	default:
	}
}

func (d *DiscordClient) close(conn *discordConn) {
	_ = conn.Close()
	d.logger.Info("Disconnected from Discord")
}

func (d *DiscordClient) onUp(conn *discordConn) {
	// re-check the snapshot now that the connection is adopted
	if d.deej.config.Values().Discord != conn.cfg {
		d.logger.Debug("Discord config changed while connecting, triggering reconnect")
		d.reconnector.Reconnect(errors.New("config changed during dial"))
		return
	}

	// a fresh connection knows nothing about what deej last sent
	d.volumeLock.Lock()
	clear(d.lastSent)
	d.volumeLock.Unlock()

	d.logger.Info("Connected to Discord")

	// the event loop subscribes and reads the initial roster; it must not run
	// here, since this is the reconnector's goroutine
	d.requestReconcile()
}

func (d *DiscordClient) requestReconcile() {
	select {
	case d.reconcile <- struct{}{}:
	default:
		// one is already queued, which does the same work
	}
}

// setupOnConfigReload triggers a reconnect when the Discord credentials change
func (d *DiscordClient) setupOnConfigReload() {
	configReloadedChannel := d.deej.config.SubscribeToChanges()
	previous := d.deej.config.Values().Discord

	go func() {
		for {
			<-configReloadedChannel
			current := d.deej.config.Values().Discord

			credentialsChanged := discordCredentialsChanged(previous, current)
			if credentialsChanged {
				d.token.Store(nil)
				if err := os.Remove(d.tokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
					d.logger.Warnw("Failed to remove Discord token after credentials changed", "error", err)
				}
			}
			previous = current

			conn, ok := d.reconnector.Current()
			if !ok {
				if credentialsChanged {
					d.reconnector.Reconnect(errors.New("Discord credentials changed"))
				}
				continue
			}

			if current != conn.cfg {
				d.logger.Debug("Discord config changed, triggering reconnect")
				d.reconnector.Reconnect(errors.New("config changed"))
			}
		}
	}()
}

func discordCredentialsChanged(before DiscordSettings, after DiscordSettings) bool {
	return before.ClientID != after.ClientID || before.ClientSecret != after.ClientSecret
}

// discordToken is the OAuth token deej persists between runs. It's kept beside
// the config file rather than inside it: a token written by the backend would
// otherwise race the settings window's whole-document saves, and every write
// would churn the config watcher
type discordToken struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// expired reports whether the token needs refreshing, a minute early so one
// doesn't lapse midway through a request
func (t *discordToken) expired() bool {
	return time.Now().After(t.ExpiresAt.Add(-time.Minute))
}

func (d *DiscordClient) tokenPath() string {
	return filepath.Join(filepath.Dir(d.deej.config.configPath), discordTokenFileName)
}

func (d *DiscordClient) loadToken() {
	data, err := os.ReadFile(d.tokenPath())
	if err != nil {
		return
	}

	var token discordToken
	if err := json.Unmarshal(data, &token); err != nil {
		d.logger.Warnw("Ignoring malformed Discord token file", "path", d.tokenPath(), "error", err)
		return
	}

	if token.AccessToken == "" || token.ClientID != d.deej.config.Values().Discord.ClientID {
		return
	}

	d.token.Store(&token)
	d.logger.Debug("Loaded stored Discord token")
}

// storeToken persists the token next to the config file. writeFileAtomic
// creates at 0600 and the rename preserves it, which is exactly what a
// credential file needs
func (d *DiscordClient) storeToken(token *discordToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal Discord token: %w", err)
	}

	if err := writeFileAtomic(d.tokenPath(), data); err != nil {
		return fmt.Errorf("write Discord token: %w", err)
	}

	d.token.Store(token)

	return nil
}

func (d *DiscordClient) ensureFreshToken(cfg DiscordSettings, token *discordToken) (*discordToken, error) {
	if !token.expired() {
		return token, nil
	}

	if token.RefreshToken == "" {
		return nil, errors.New("the stored Discord token expired and can't be refreshed, link again")
	}

	d.logger.Debug("Discord token expired, refreshing")

	refreshed, err := exchangeToken(cfg, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token.RefreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("refresh Discord token: %w", err)
	}

	if err := d.storeToken(refreshed); err != nil {
		return nil, err
	}

	return refreshed, nil
}

// exchangeToken posts to Discord's OAuth token endpoint, for both the initial
// authorization code and later refreshes
func exchangeToken(cfg DiscordSettings, form url.Values) (*discordToken, error) {
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	resp, err := discordHTTPClient.PostForm(discordTokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// the body carries Discord's own error description (and echoes nothing
		// secret), which is far more useful in the settings window than a bare
		// status code
		return nil, fmt.Errorf("token request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	if parsed.AccessToken == "" {
		return nil, errors.New("the token response had no access token")
	}

	return &discordToken{
		ClientID:     cfg.ClientID,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

// discordConn is one Discord IPC connection generation, carrying the config
// snapshot it was opened with so config reloads can detect credential changes
type discordConn struct {
	rw  io.ReadWriteCloser
	cfg DiscordSettings

	writeLock sync.Mutex

	// pending correlates replies to the callers waiting on them, by nonce
	pendingLock sync.Mutex
	pending     map[string]chan discordFrame

	// onEvent receives subscribed RPC events. It is called from the read loop,
	// so it must never block
	onEvent func(evt string, data json.RawMessage)

	ready     chan struct{}
	readyOnce sync.Once

	// done is closed when the read loop exits, broadcasting the stored error
	// to every caller waiting on this connection
	done     chan struct{}
	doneOnce sync.Once
	doneErr  error
}

func newDiscordConn(rw io.ReadWriteCloser, cfg DiscordSettings, onEvent func(evt string, data json.RawMessage)) *discordConn {
	conn := &discordConn{
		rw:      rw,
		cfg:     cfg,
		onEvent: onEvent,
		pending: map[string]chan discordFrame{},
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
	}

	go conn.readLoop()

	return conn
}

// Close releases the transport, which unblocks the read loop and in turn any
// Watch waiting on it
func (c *discordConn) Close() error {
	return c.rw.Close()
}

// discordFrame is the envelope every RPC command, reply and event shares
type discordFrame struct {
	Cmd   string          `json:"cmd"`
	Nonce string          `json:"nonce,omitempty"`
	Evt   string          `json:"evt,omitempty"`
	Args  any             `json:"args,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func (c *discordConn) request(cmd string, args any) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Args: args}, discordCommandTimeout)
}

// requestEvent issues SUBSCRIBE and UNSUBSCRIBE, the two commands that name
// their event in evt rather than in args
func (c *discordConn) requestEvent(cmd string, evt string, args any) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Evt: evt, Args: args}, discordCommandTimeout)
}

func (c *discordConn) requestWithTimeout(cmd string, args any, timeout time.Duration) (json.RawMessage, error) {
	return c.roundTrip(discordFrame{Cmd: cmd, Args: args}, timeout)
}

func (c *discordConn) roundTrip(frame discordFrame, timeout time.Duration) (json.RawMessage, error) {
	cmd := frame.Cmd
	nonce := uuid.NewString()
	frame.Nonce = nonce

	body, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", cmd, err)
	}

	// buffered, so the read loop never blocks handing over a reply nobody is
	// waiting for anymore
	replyChannel := make(chan discordFrame, 1)

	c.pendingLock.Lock()
	c.pending[nonce] = replyChannel
	c.pendingLock.Unlock()

	defer func() {
		c.pendingLock.Lock()
		delete(c.pending, nonce)
		c.pendingLock.Unlock()
	}()

	if err := c.write(discordOpFrame, body); err != nil {
		return nil, err
	}

	select {
	case reply := <-replyChannel:
		return discordReplyData(reply)

	case <-c.done:
		return nil, c.doneErr

	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for a reply to %s", cmd)
	}
}

// discordReplyData unwraps a reply, turning an ERROR frame into a Go error
func discordReplyData(reply discordFrame) (json.RawMessage, error) {
	if reply.Evt != "ERROR" {
		return reply.Data, nil
	}

	var rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(reply.Data, &rpcErr); err == nil && rpcErr.Message != "" {
		return nil, fmt.Errorf("discord rpc error %d: %s", rpcErr.Code, rpcErr.Message)
	}

	return nil, errors.New("discord rpc error")
}

func (c *discordConn) write(opcode uint32, body []byte) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	return writeDiscordFrame(c.rw, opcode, body)
}

func (c *discordConn) fail(err error) {
	c.doneOnce.Do(func() {
		c.doneErr = err
		close(c.done)
	})
}

// readLoop is the connection's only reader: it hands replies to the callers
// waiting on their nonce and answers pings, until the transport fails or is
// closed
func (c *discordConn) readLoop() {
	for {
		opcode, body, err := readDiscordFrame(c.rw)
		if err != nil {
			c.fail(fmt.Errorf("read frame: %w", err))
			return
		}

		switch opcode {
		case discordOpPing:
			// the client wants its own payload echoed back
			if err := c.write(discordOpPong, body); err != nil {
				c.fail(err)
				return
			}
			continue

		case discordOpClose:
			c.fail(fmt.Errorf("discord closed the connection: %s", strings.TrimSpace(string(body))))
			return

		case discordOpFrame:

		default:
			continue
		}

		var frame discordFrame
		if err := json.Unmarshal(body, &frame); err != nil {
			continue
		}

		// the handshake is acknowledged with an event rather than a reply, so
		// it can't be correlated by nonce like everything else
		if frame.Cmd == "DISPATCH" && frame.Evt == "READY" {
			c.readyOnce.Do(func() { close(c.ready) })
			continue
		}

		if frame.Nonce == "" {
			// a subscribed event. onEvent must not block: it's handing work to
			// a goroutine that issues requests this very loop has to answer
			if frame.Cmd == "DISPATCH" && frame.Evt != "" && c.onEvent != nil {
				c.onEvent(frame.Evt, frame.Data)
			}

			continue
		}

		c.pendingLock.Lock()
		replyChannel, waiting := c.pending[frame.Nonce]
		delete(c.pending, frame.Nonce)
		c.pendingLock.Unlock()

		if waiting {
			replyChannel <- frame
		}
	}
}

// discordMaxFrameSize caps what a single frame may claim, so a desynced stream
// can't make deej allocate wildly
const discordMaxFrameSize = 1 << 20

// writeDiscordFrame writes one IPC frame: a little-endian opcode and length,
// then the JSON body
func writeDiscordFrame(w io.Writer, opcode uint32, body []byte) error {
	frame := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], opcode)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(body)))

	if _, err := w.Write(append(frame, body...)); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	return nil
}

func readDiscordFrame(r io.Reader) (uint32, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	opcode := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])

	if length > discordMaxFrameSize {
		return 0, nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}

	return opcode, body, nil
}

// dialDiscordIPC opens the Discord client's local RPC transport. Discord takes
// the first free slot of ten, so all of them get probed
func dialDiscordIPC() (io.ReadWriteCloser, error) {
	for _, base := range discordIPCPaths() {
		for i := range 10 {
			rw, err := openDiscordIPC(fmt.Sprintf("%s%d", base, i))
			if err != nil {
				continue
			}

			return rw, nil
		}
	}

	return nil, errors.New("no Discord IPC socket found (is Discord running?)")
}
