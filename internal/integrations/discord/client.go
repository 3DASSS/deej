package discord

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/reconnect"
	"go.uber.org/zap"
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

	// TokenFileName is the credential file kept beside the user configuration.
	TokenFileName = "discord_token.json"
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

// Client controls volumes inside the Discord client over its local RPC
// connection: the volume of individual people in a voice channel, and the
// user's own input and output levels. Mapping the discord.exe process covers
// neither of those, which is the whole reason this exists
type Client struct {
	settings       SettingsProvider
	tokenFile      string
	currentVolumes func() []TargetVolume
	lifecycle      sync.Mutex
	wg             sync.WaitGroup
	logger         *zap.SugaredLogger

	reconnector *reconnect.Reconnector[*discordConn]

	token  atomic.Pointer[discordToken]
	roster atomic.Pointer[discordRoster]

	// Serialize token commits and removal, without holding the lock during
	// network requests. Older requests must not undo Unlink or a newer Link.
	authLock       sync.Mutex
	authGeneration uint64

	// volumeLock guards the throttle's two maps: pending is what the slider
	// handler wrote, lastSent is what actually reached Discord
	volumeLock       sync.Mutex
	pending          map[string]float32
	lastSent         map[string]float32
	volumeGeneration uint64

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

func New(settings SettingsProvider, logger *zap.SugaredLogger, tokenPath string, currentVolumes func() []TargetVolume) *Client {
	logger = logger.Named("discord")

	d := &Client{
		settings:            settings,
		tokenFile:           tokenPath,
		currentVolumes:      currentVolumes,
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
			cfg := d.settings.Values().Discord
			return cfg.Enabled && cfg.ClientID != "" && d.token.Load() != nil
		},

		Dial:  d.dial,
		Watch: d.watch,
		Close: d.close,
		OnUp:  d.onUp,
		OnDown: func(err error) {
			d.logger.Warnw("Discord connection error, reconnecting...", "error", err)
		},
		Backoff: func(int) time.Duration { return discordRetryDelay },
	})

	logger.Debug("Created Discord client instance")

	return d
}

func (d *Client) Start() {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	if d.stopChannel != nil {
		return
	}
	d.stopChannel = make(chan struct{})
	d.logger.Info("Discord client starting")

	// NewClient is constructed before the initial config load.
	d.loadToken()
	d.setupOnConfigReload()

	d.reconnector.Start()

	// the event loop issues blocking requests of its own, so it doesn't share
	// a goroutine with the volume flush
	stop := d.stopChannel
	d.wg.Go(func() { discordTickLoop(stop, discordFlushInterval, d.flush) })
	d.wg.Go(func() { d.eventLoop(stop) })
}

func (d *Client) Stop() {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	if d.stopChannel == nil {
		return
	}
	close(d.stopChannel)
	d.reconnector.Stop()
	d.wg.Wait()
	d.stopChannel = nil
	d.logger.Info("Discord client stopped")
}

func (d *Client) IsConnected() bool {
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
func (d *Client) Status() Status {
	status := Status{Linked: d.token.Load() != nil, Connected: d.reconnector.Connected()}

	if roster := d.roster.Load(); roster != nil {
		status.Channel = roster.channel
	}

	return status
}

// dial establishes an authenticated connection using the stored token,
// without touching any client state - the reconnector decides whether the
// result is adopted
func (d *Client) dial() (*discordConn, error) {
	cfg := d.settings.Values().Discord

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
func (d *Client) open(cfg config.DiscordSettings) (*discordConn, error) {
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
func (d *Client) watch(conn *discordConn, errChannel chan<- error) {
	<-conn.done

	select {
	case errChannel <- conn.doneErr:
	default:
	}
}

func (d *Client) close(conn *discordConn) {
	_ = conn.Close()
	// The reconnector clears Current before Close. Reconcile on its own loop
	// so a disconnect cannot race a roster update from an RPC event.
	d.requestReconcile()
	d.logger.Info("Disconnected from Discord")
}

func (d *Client) onUp(conn *discordConn) {
	// re-check the snapshot now that the connection is adopted
	if d.settings.Values().Discord != conn.cfg {
		d.logger.Debug("Discord config changed while connecting, triggering reconnect")
		d.reconnector.Reconnect(errors.New("config changed during dial"))
		return
	}

	// a fresh connection knows nothing about what deej last sent
	d.invalidateVolumes()

	d.logger.Info("Connected to Discord")

	// the event loop subscribes and reads the initial roster; it must not run
	// here, since this is the reconnector's goroutine
	d.requestReconcile()
}

func (d *Client) requestReconcile() {
	select {
	case d.reconcile <- struct{}{}:
	default:
		// one is already queued, which does the same work
	}
}

// setupOnConfigReload triggers a reconnect when the Discord credentials change
func (d *Client) setupOnConfigReload() {
	configReloadedChannel := d.settings.SubscribeToChanges()
	previous := d.settings.Values().Discord

	stop := d.stopChannel
	d.wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			case <-configReloadedChannel:
			}
			current := d.settings.Values().Discord

			credentialsChanged := discordCredentialsChanged(previous, current)
			if credentialsChanged {
				if err := d.clearToken(); err != nil {
					d.logger.Warnw("Failed to remove Discord token after credentials changed", "error", err)
				}
			}
			previous = current

			conn, ok := d.reconnector.Current()
			if !ok {
				if credentialsChanged {
					d.reconnector.Reconnect(errors.New("discord credentials changed"))
				}
				continue
			}

			if current != conn.cfg {
				d.logger.Debug("Discord config changed, triggering reconnect")
				d.reconnector.Reconnect(errors.New("config changed"))
			}
		}
	})
}

func discordCredentialsChanged(before config.DiscordSettings, after config.DiscordSettings) bool {
	return before.ClientID != after.ClientID || before.ClientSecret != after.ClientSecret
}

// SettingsProvider supplies live immutable settings and coalesced reload signals.
type SettingsProvider interface {
	Values() *config.Settings
	SubscribeToChanges() chan bool
}

// User is a participant in the current Discord voice channel.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status describes account authorization, connection and the selected voice channel.
type Status struct {
	Linked    bool   `json:"linked"`
	Connected bool   `json:"connected"`
	Channel   string `json:"channel"`
}
