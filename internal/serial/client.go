package serial

import (
	"bufio"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	transport "go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"go.uber.org/zap"

	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/reconnect"
)

// serialConn is one serial connection generation, carrying the port name it
// resolved to and the connection parameters it was opened with so config
// reloads can detect changes
type serialConn struct {
	port     transport.Port
	comPort  string
	connInfo config.COMSettings
	adopted  *atomic.Bool
}

// Client manages hardware connections and synchronous event subscriptions.
type Client struct {
	settings      SettingsProvider
	options       Options
	lifecycle     sync.Mutex
	stopped       bool
	configDone    chan struct{}
	configWG      sync.WaitGroup
	consumersLock sync.Mutex
	logger        *zap.SugaredLogger

	reconnector *reconnect.Reconnector[serialConn]

	stateLock           sync.Mutex
	lastKnownNumSliders int
	currentSliderValues []int

	sliderMoveConsumers  []chan SliderMoveEvent
	stateChangeConsumers []chan bool
}

var ErrNoSerialPorts = errors.New("no serial ports found")
var ErrAutoPortNotFound = errors.New("can't autodetect com port")

// SliderMoveEvent represents a single slider move captured by deej
type SliderMoveEvent struct {
	SliderID     int
	PercentValue float32
}

// New constructs a serial client and subscribes to settings changes.
// Stop must be called even when Start has not been called.
func New(settings SettingsProvider, logger *zap.SugaredLogger, options Options) (*Client, error) {
	logger = logger.Named("serial")

	sio := &Client{
		settings:             settings,
		options:              options,
		configDone:           make(chan struct{}),
		logger:               logger,
		sliderMoveConsumers:  []chan SliderMoveEvent{},
		stateChangeConsumers: []chan bool{},
	}

	sio.reconnector = reconnect.New(reconnect.Options[serialConn]{
		Logger: logger,
		Dial:   sio.dial,
		Watch:  sio.watch,
		Close:  sio.close,
		OnUp:   sio.onUp,
		OnDown: sio.onDown,
	})

	logger.Debug("Created serial i/o instance")

	// respond to config changes
	sio.setupOnConfigReload()

	return sio, nil
}

// dial resolves the com port (autodetecting by VID/PID if configured) and
// opens it
func (sio *Client) dial() (serialConn, error) {
	config := sio.settings.Values()

	comPort := config.COM.Port
	wantVID, _ := config.COM.VID.Value()
	wantPID, _ := config.COM.PID.Value()

	if comPort == "auto" {
		sio.logger.Debugw("Trying to autodetect serial port")

		ports, err := enumerator.GetDetailedPortsList()

		if err != nil {
			sio.logger.Errorw("Failed to enumarate serial ports, retrying", "err", err)
			return serialConn{}, ErrNoSerialPorts
		}
		if len(ports) == 0 {
			sio.logger.Debug("No serial ports found, retrying")
			return serialConn{}, ErrNoSerialPorts
		}
		for _, port := range ports {
			sio.logger.Debugf("Found port: %s", port.Name)
			if port.IsUSB {
				sio.logger.Debugf("   USB ID     %s:%s", port.VID, port.PID)

				vid, _ := strconv.ParseUint(port.VID, 16, 16)
				pid, _ := strconv.ParseUint(port.PID, 16, 16)

				if uint16(vid) == wantVID && uint16(pid) == wantPID {
					sio.logger.Debugw("Found COM port", "com", port.Name, "vid", port.VID, "pid", port.PID)

					comPort = port.Name
					break
				}

			}
		}

		if comPort == "auto" {
			sio.logger.Debug("COM port not found, retrying")
			return serialConn{}, ErrAutoPortNotFound
		}
	}

	mode := transport.Mode{
		BaudRate: config.COM.BaudRate,
		DataBits: 8,
		StopBits: transport.OneStopBit,
	}

	sio.logger.Debugw("Attempting serial connection",
		"comPort", comPort,
		"baudRate", mode.BaudRate)

	port, err := transport.Open(comPort, &mode)

	if err != nil {
		// might need a user notification here, TBD
		sio.logger.Debugw("Failed to open serial connection", "error", err)
		return serialConn{}, fmt.Errorf("open serial connection: %w", err)
	}

	// actually, this sets timeout to 0x7FFFFFFE instead of 0xFFFFFFFE
	// to make serial chip work properly.
	// see https://github.com/arduino/serial-monitor/issues/112
	err = port.SetReadTimeout(transport.NoTimeout)
	if err != nil {
		sio.logger.Warnw("Failed to set read timeout", "error", err)

		// close the port before bailing, otherwise the handle stays open
		// and every future open attempt fails with access denied
		if closeErr := port.Close(); closeErr != nil {
			sio.logger.Warnw("Failed to close serial connection", "error", closeErr)
		}

		return serialConn{}, fmt.Errorf("set read timeout: %w", err)
	}

	return serialConn{
		port:     port,
		comPort:  comPort,
		connInfo: config.COM,
		adopted:  new(atomic.Bool),
	}, nil
}

func (sio *Client) onUp(conn serialConn) {
	if sio.settings.Values().COM != conn.connInfo {
		sio.logger.Info("Connection parameters changed while connecting, renewing connection")
		sio.reconnector.Reconnect(errors.New("connection parameters changed during dial"))
		return
	}

	sio.sendStateChangeEvent(true)

	sio.logger.Named(strings.ToLower(conn.comPort)).Infow("Connected")

	if conn.adopted != nil {
		conn.adopted.Store(true)
	}
	if sio.options.OnConnected != nil {
		sio.options.OnConnected(conn.comPort)
	}

}

func (sio *Client) onDown(err error) {
	sio.logger.Warnw("Read line error", "err", err)
	sio.logger.Warn("Closing serial port")

	if sio.options.OnDisconnected != nil {
		sio.options.OnDisconnected(sio.CurrentComPort())
	}

}

func (sio *Client) close(conn serialConn) {
	if err := conn.port.Close(); err != nil {
		sio.logger.Warnw("Failed to close serial connection", "error", err)
	} else {
		sio.logger.Info("Serial connection closed")
	}

	// Late dial results were never published, so closing them must not emit
	// events after Stop has let the consumers exit.
	if conn.adopted != nil && conn.adopted.Swap(false) {
		sio.sendStateChangeEvent(false)
	}
}

func (sio *Client) GetState() bool {
	return sio.reconnector.Connected()
}

// CurrentComPort returns the port the live connection resolved to, or the
// configured port (verbatim, including "auto") while disconnected
func (sio *Client) CurrentComPort() string {
	if conn, ok := sio.reconnector.Current(); ok {
		return conn.comPort
	}

	return sio.settings.Values().COM.Port
}

func (sio *Client) CurrentSliderValues() []int {
	sio.stateLock.Lock()
	defer sio.stateLock.Unlock()

	return slices.Clone(sio.currentSliderValues)
}

// Start attempts to connect to our arduino chip
func (sio *Client) Start() {
	sio.lifecycle.Lock()
	defer sio.lifecycle.Unlock()
	if sio.stopped {
		return
	}
	config := sio.settings.Values()
	sio.logger.Infow("Trying serial connection",
		"port", config.COM.Port,
		"vid", string(config.COM.VID),
		"pid", string(config.COM.PID),
	)

	sio.reconnector.Start()
}

// Stop terminates the client and its settings worker. Consumers must keep
// receiving events until Stop returns. Subscription channels remain open.
func (sio *Client) Stop() {
	sio.lifecycle.Lock()
	defer sio.lifecycle.Unlock()
	if sio.stopped {
		return
	}
	sio.stopped = true
	close(sio.configDone)
	sio.configWG.Wait()
	sio.reconnector.Stop()
	sio.logger.Info("Serial stopped")
}

// SubscribeToSliderMoveEvents returns an unbuffered channel that receives
// a sliderMoveEvent struct every time a slider moves
func (sio *Client) SubscribeToSliderMoveEvents() chan SliderMoveEvent {
	ch := make(chan SliderMoveEvent)
	sio.consumersLock.Lock()
	sio.sliderMoveConsumers = append(sio.sliderMoveConsumers, ch)
	sio.consumersLock.Unlock()

	return ch
}

func (sio *Client) SubscribeToStateChangeEvent() chan bool {
	ch := make(chan bool)
	sio.consumersLock.Lock()
	sio.stateChangeConsumers = append(sio.stateChangeConsumers, ch)
	sio.consumersLock.Unlock()

	return ch
}

func (sio *Client) sendStateChangeEvent(state bool) {
	sio.consumersLock.Lock()
	consumers := slices.Clone(sio.stateChangeConsumers)
	sio.consumersLock.Unlock()
	for _, consumer := range consumers {
		consumer <- state
	}
}

func (sio *Client) setupOnConfigReload() {
	configReloadedChannel := sio.settings.SubscribeToChanges()

	sio.configWG.Go(func() {
		for {
			select {
			case <-sio.configDone:
				return
			case <-configReloadedChannel:
			}

			// reset the slider count so the next line re-emits all values
			sio.stateLock.Lock()
			sio.lastKnownNumSliders = 0
			sio.stateLock.Unlock()

			config := sio.settings.Values()

			// if connection params have changed, ask the reconnector to
			// renew the connection. when disconnected every dial attempt
			// reads the current config anyway
			conn, ok := sio.reconnector.Current()
			if !ok {
				continue
			}

			if config.COM != conn.connInfo {
				sio.logger.Info("Detected change in connection parameters, attempting to renew connection")
				sio.reconnector.Reconnect(errors.New("connection parameters changed"))
			}
		}
	})
}

// watch reads lines off a single connection until it fails; closing the port
// unblocks the pending read
func (sio *Client) watch(conn serialConn, errChannel chan<- error) {
	logger := sio.logger.Named(strings.ToLower(conn.comPort))

	reader := bufio.NewReader(conn.port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// non-blocking send: Reconnect may have already filled this
			// generation's buffer, and nothing drains it after teardown
			select {
			case errChannel <- fmt.Errorf("read error: %w", err):
			default:
			}
			return
		}

		if sio.options.Verbose {
			logger.Debugw("Read new line", "line", line)
		}

		sio.handleLine(logger, line)
	}
}
