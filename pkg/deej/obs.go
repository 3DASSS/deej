package deej

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/inputs"
	"go.uber.org/zap"

	"github.com/nik9play/deej/pkg/reconnect"
)

const obsRetryDelay = 5 * time.Second

// obsConn is one OBS connection generation, carrying the config snapshot it
// was dialed with so config reloads can detect parameter changes
type obsConn struct {
	client *goobs.Client
	cfg    OBSSettings
}

type OBSClient struct {
	deej   *Deej
	logger *zap.SugaredLogger

	reconnector *reconnect.Reconnector[obsConn]
}

func NewOBSClient(deej *Deej, logger *zap.SugaredLogger) *OBSClient {
	logger = logger.Named("obs")

	o := &OBSClient{
		deej:   deej,
		logger: logger,
	}

	o.reconnector = reconnect.New(reconnect.Options[obsConn]{
		Logger:  logger,
		Enabled: func() bool { return o.deej.config.Values().OBS.Enabled },
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

	o.setupOnConfigReload()

	return o
}

func (o *OBSClient) Start() {
	o.logger.Info("OBS client starting")
	o.reconnector.Start()
}

func (o *OBSClient) Stop() {
	o.reconnector.Stop()
	o.logger.Info("OBS client stopped")
}

func (o *OBSClient) IsConnected() bool {
	return o.reconnector.Connected()
}

// obsVolumeMulFromPercent converts a 0.0-1.0 slider position to an OBS volume
// multiplier using the cubic curve OBS's own mixer faders use
func obsVolumeMulFromPercent(percent float32) float64 {
	p := float64(percent)
	return p * p * p
}

func (o *OBSClient) SetInputVolume(inputName string, percent float32) error {
	conn, ok := o.reconnector.Current()
	if !ok {
		return fmt.Errorf("not connected to OBS")
	}

	vol := obsVolumeMulFromPercent(percent)
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
func (o *OBSClient) ListInputs() ([]string, error) {
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

func (o *OBSClient) onUp(conn obsConn) {
	// re-check the snapshot now that the connection is adopted
	if o.deej.config.Values().OBS != conn.cfg {
		o.logger.Debug("OBS config changed while connecting, triggering reconnect")
		o.reconnector.Reconnect(errors.New("config changed during dial"))
		return
	}

	o.logger.Info("Connected to OBS")
}

// dial connects to OBS using the current config without touching any client
// state - the reconnector decides whether to adopt the returned connection
func (o *OBSClient) dial() (obsConn, error) {
	cfg := o.deej.config.Values().OBS
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
func (o *OBSClient) watch(conn obsConn, errChannel chan<- error) {
	for range conn.client.IncomingEvents { //nolint:revive // draining only
	}

	select {
	case errChannel <- errors.New("OBS connection closed"):
	default:
	}
}

func (o *OBSClient) close(conn obsConn) {
	_ = conn.client.Disconnect()
	o.logger.Info("Disconnected from OBS")
}

// setupOnConfigReload triggers a reconnect when
// the OBS connection parameters change
func (o *OBSClient) setupOnConfigReload() {
	configReloadedChannel := o.deej.config.SubscribeToChanges()

	go func() {
		for {
			<-configReloadedChannel

			conn, ok := o.reconnector.Current()
			if !ok {
				continue
			}

			cfg := o.deej.config.Values().OBS

			if cfg != conn.cfg {
				o.logger.Debug("OBS config changed, triggering reconnect")
				o.reconnector.Reconnect(errors.New("config changed"))
			}
		}
	}()
}
