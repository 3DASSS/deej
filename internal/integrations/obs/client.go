package obs

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/inputs"
	"go.uber.org/zap"

	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/reconnect"
)

const obsRetryDelay = 5 * time.Second

// obsConn is one OBS connection generation, carrying the config snapshot it
// was dialed with so config reloads can detect parameter changes
type obsConn struct {
	client *goobs.Client
	cfg    config.OBSSettings
}

type Client struct {
	settings  SettingsProvider
	lifecycle sync.Mutex
	stop      chan struct{}
	wg        sync.WaitGroup
	logger    *zap.SugaredLogger

	reconnector *reconnect.Reconnector[obsConn]
}

func New(settings SettingsProvider, logger *zap.SugaredLogger) *Client {
	logger = logger.Named("obs")

	o := &Client{
		settings: settings,
		logger:   logger,
	}

	o.reconnector = reconnect.New(reconnect.Options[obsConn]{
		Logger:  logger,
		Enabled: func() bool { return o.settings.Values().OBS.Enabled },
		Dial:    o.dial,
		Watch:   o.watch,
		Close:   o.close,
		OnUp:    o.onUp,
		OnDown: func(err error) {
			o.logger.Warnw("OBS connection error, reconnecting...", "error", err)
		},
		Backoff: func(int) time.Duration { return obsRetryDelay },
	})

	logger.Debug("Created OBS client instance")

	return o
}

func (o *Client) Start() {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()
	if o.stop != nil {
		return
	}
	o.stop = make(chan struct{})
	o.setupOnConfigReload()
	o.logger.Info("OBS client starting")
	o.reconnector.Start()
}

func (o *Client) Stop() {
	o.lifecycle.Lock()
	defer o.lifecycle.Unlock()
	if o.stop == nil {
		return
	}
	close(o.stop)
	o.reconnector.Stop()
	o.wg.Wait()
	o.stop = nil
	o.logger.Info("OBS client stopped")
}

func (o *Client) IsConnected() bool {
	return o.reconnector.Connected()
}

// obsVolumeMulFromPercent converts a 0.0-1.0 slider position to an OBS volume
// multiplier. Conversion uses the cubic curve OBS's own mixer faders use;
// when disabled, the physical slider maps linearly to the multiplier.
func obsVolumeMulFromPercent(percent float32, convert bool) float64 {
	position := min(max(float64(percent), 0), 1)
	if convert {
		return position * position * position
	}

	return position
}

func (o *Client) SetInputVolume(inputName string, percent float32) error {
	conn, ok := o.reconnector.Current()
	if !ok {
		return fmt.Errorf("not connected to OBS")
	}

	convert := o.settings.Values().OBS.VolumeConversion
	vol := obsVolumeMulFromPercent(percent, convert)
	_, err := conn.client.Inputs.SetInputVolume(&inputs.SetInputVolumeParams{
		InputName:      &inputName,
		InputVolumeMul: &vol,
	})

	if err != nil {
		return err
	}

	o.logger.Debugw("Set OBS input volume", "input", inputName, "volume", percent)

	return nil
}

// ListInputs returns the sorted names of all inputs in the connected OBS
// instance, used by the settings GUI for target suggestions
func (o *Client) ListInputs() ([]string, error) {
	conn, ok := o.reconnector.Current()
	if !ok {
		return nil, errors.New("not connected to OBS")
	}

	resp, err := conn.client.Inputs.GetInputList()
	if err != nil {
		return nil, fmt.Errorf("get OBS input list: %w", err)
	}

	names := make([]string, 0, len(resp.Inputs))
	for _, input := range resp.Inputs {
		names = append(names, input.InputName)
	}

	sort.Strings(names)

	return names, nil
}

func (o *Client) onUp(conn obsConn) {
	// re-check the snapshot now that the connection is adopted
	if o.settings.Values().OBS != conn.cfg {
		o.logger.Debug("OBS config changed while connecting, triggering reconnect")
		o.reconnector.Reconnect(errors.New("config changed during dial"))
		return
	}

	o.logger.Info("Connected to OBS")
}

// dial connects to OBS using the current config without touching any client
// state - the reconnector decides whether to adopt the returned connection
func (o *Client) dial() (obsConn, error) {
	cfg := o.settings.Values().OBS
	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	o.logger.Debugw("Attempting OBS connection", "address", address)

	opts := []goobs.Option{}
	if cfg.Password != "" {
		opts = append(opts, goobs.WithPassword(cfg.Password))
	}

	client, err := goobs.New(address, opts...)
	if err != nil {
		o.logger.Debugw("Failed to connect to OBS", "error", err)
		return obsConn{}, fmt.Errorf("connect to OBS: %w", err)
	}

	return obsConn{client: client, cfg: cfg}, nil
}

// watch drains a single connection's incoming events to detect
// disconnection; Disconnect closes the events channel, which ends the loop
func (o *Client) watch(conn obsConn, errChannel chan<- error) {
	for range conn.client.IncomingEvents { //nolint:revive // draining only
	}

	select {
	case errChannel <- errors.New("OBS connection closed"):
	default:
	}
}

func (o *Client) close(conn obsConn) {
	_ = conn.client.Disconnect()
	o.logger.Info("Disconnected from OBS")
}

// setupOnConfigReload triggers a reconnect when
// the OBS connection parameters change
func (o *Client) setupOnConfigReload() {
	configReloadedChannel := o.settings.SubscribeToChanges()

	stop := o.stop
	o.wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			case <-configReloadedChannel:
			}

			conn, ok := o.reconnector.Current()
			if !ok {
				continue
			}

			cfg := o.settings.Values().OBS

			if cfg != conn.cfg {
				o.logger.Debug("OBS config changed, triggering reconnect")
				o.reconnector.Reconnect(errors.New("config changed"))
			}
		}
	})
}

// SettingsProvider supplies current settings and independent reload subscriptions.
type SettingsProvider interface {
	Values() *config.Settings
	SubscribeToChanges() chan bool
}
