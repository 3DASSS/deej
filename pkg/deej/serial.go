package deej

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"go.uber.org/zap"

	"github.com/nik9play/deej/pkg/deej/util"
	"github.com/nik9play/deej/pkg/reconnect"
)

// serialConn is one serial connection generation, carrying the port name it
// resolved to and the connection parameters it was opened with so config
// reloads can detect changes
type serialConn struct {
	port     serial.Port
	comPort  string
	connInfo ConnectionInfo
}

// SerialIO provides a deej-aware abstraction layer to managing serial I/O
type SerialIO struct {
	deej   *Deej
	logger *zap.SugaredLogger

	reconnector *reconnect.Reconnector[serialConn]

	stateLock           sync.Mutex
	comPortToUse        string
	lastKnownNumSliders int
	currentSliderValues []int

	sliderMoveConsumers  []chan SliderMoveEvent
	stateChangeConsumers []chan bool
}

var ErrNoSerialPorts = errors.New("no serial ports found")
var ErrAutoPortNotFound = errors.New("can't autodetect com port")

// var allowedVIDPIDs = []VIDPID{{0x1A86, 0x7523}}

// SliderMoveEvent represents a single slider move captured by deej
type SliderMoveEvent struct {
	SliderID     int
	PercentValue float32
}

var expectedLinePattern = regexp.MustCompile(`^\d{1,4}(\|\d{1,4})*\r\n$`)

// NewSerialIO creates a SerialIO instance that uses the provided deej
// instance's connection info to establish communications with the arduino chip
func NewSerialIO(deej *Deej, logger *zap.SugaredLogger) (*SerialIO, error) {
	logger = logger.Named("serial")

	sio := &SerialIO{
		deej:                 deej,
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
func (sio *SerialIO) dial() (serialConn, error) {
	config := sio.deej.config.Values()

	comPort := config.ConnectionInfo.COMPort
	allowedVIDPID := config.AutoSearchVIDPID

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

				if vid == allowedVIDPID.VID && pid == allowedVIDPID.PID {
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

	mode := serial.Mode{
		BaudRate: config.ConnectionInfo.BaudRate,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	sio.logger.Debugw("Attempting serial connection",
		"comPort", comPort,
		"baudRate", mode.BaudRate)

	port, err := serial.Open(comPort, &mode)

	if err != nil {
		// might need a user notification here, TBD
		sio.logger.Debugw("Failed to open serial connection", "error", err)
		return serialConn{}, fmt.Errorf("open serial connection: %w", err)
	}

	// actually, this sets timeout to 0x7FFFFFFE instead of 0xFFFFFFFE
	// to make serial chip work properly.
	// see https://github.com/arduino/serial-monitor/issues/112
	err = port.SetReadTimeout(serial.NoTimeout)
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
		connInfo: config.ConnectionInfo,
	}, nil
}

func (sio *SerialIO) onUp(conn serialConn) {
	sio.stateLock.Lock()
	sio.comPortToUse = conn.comPort
	sio.stateLock.Unlock()

	if sio.deej.config.Values().ConnectionInfo != conn.connInfo {
		sio.logger.Info("Connection parameters changed while connecting, renewing connection")
		sio.reconnector.Reconnect(errors.New("connection parameters changed during dial"))
		return
	}

	sio.sendStateChangeEvent(true)

	sio.logger.Named(strings.ToLower(conn.comPort)).Infow("Connected")

	connectedTitle := sio.deej.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ComPortConnectedNotificationTitle",
			Other: "Connected to {{.ComPort}}.",
		},
		TemplateData: map[string]string{
			"ComPort": conn.comPort,
		},
	})
	connectedDescription := sio.deej.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ComPortConnectedNotificationDescription",
			Other: "Succesfully connected to deej.",
		},
	})
	sio.deej.notifier.Notify(connectedTitle, connectedDescription)
}

func (sio *SerialIO) onDown(err error) {
	sio.logger.Warnw("Read line error", "err", err)
	sio.logger.Warn("Closing serial port")

	disconnectedTitle := sio.deej.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ComPortDisconnectedNotificationTitle",
			Other: "Disconnected from {{.ComPort}} due to an error.",
		},
		TemplateData: map[string]string{
			"ComPort": sio.CurrentComPort(),
		},
	})
	disconnectedDescription := sio.deej.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ComPortDisconnectedNotificationDescription",
			Other: "Trying to reconnect.",
		},
	})
	sio.deej.notifier.Notify(disconnectedTitle, disconnectedDescription)
}

func (sio *SerialIO) close(conn serialConn) {
	if err := conn.port.Close(); err != nil {
		sio.logger.Warnw("Failed to close serial connection", "error", err)
	} else {
		sio.logger.Info("Serial connection closed")
	}

	sio.sendStateChangeEvent(false)
}

func (sio *SerialIO) GetState() bool {
	return sio.reconnector.Connected()
}

func (sio *SerialIO) CurrentComPort() string {
	sio.stateLock.Lock()
	defer sio.stateLock.Unlock()

	return sio.comPortToUse
}

func (sio *SerialIO) CurrentSliderValues() []int {
	sio.stateLock.Lock()
	defer sio.stateLock.Unlock()

	return slices.Clone(sio.currentSliderValues)
}

// Start attempts to connect to our arduino chip
func (sio *SerialIO) Start() {
	config := sio.deej.config.Values()
	sio.logger.Infow("Trying serial connection",
		"port", config.ConnectionInfo.COMPort,
		"vid", fmt.Sprintf("%X", config.AutoSearchVIDPID.VID),
		"pid", fmt.Sprintf("%X", config.AutoSearchVIDPID.PID),
	)

	sio.reconnector.Start()
}

// Stop signals us to shut down our serial connection, if one is active
func (sio *SerialIO) Stop() {
	sio.reconnector.Stop()
	sio.logger.Info("Serial stopped")
}

// SubscribeToSliderMoveEvents returns an unbuffered channel that receives
// a sliderMoveEvent struct every time a slider moves
func (sio *SerialIO) SubscribeToSliderMoveEvents() chan SliderMoveEvent {
	ch := make(chan SliderMoveEvent)
	sio.sliderMoveConsumers = append(sio.sliderMoveConsumers, ch)

	return ch
}

func (sio *SerialIO) SubscribeToStateChangeEvent() chan bool {
	ch := make(chan bool)
	sio.stateChangeConsumers = append(sio.stateChangeConsumers, ch)

	return ch
}

func (sio *SerialIO) sendStateChangeEvent(state bool) {
	for _, consumer := range sio.stateChangeConsumers {
		consumer <- state
	}
}

func (sio *SerialIO) setupOnConfigReload() {
	configReloadedChannel := sio.deej.config.SubscribeToChanges()

	go func() {
		for {
			<-configReloadedChannel

			// reset the slider count so the next line re-emits all values
			sio.stateLock.Lock()
			sio.lastKnownNumSliders = 0
			sio.stateLock.Unlock()

			// if connection params have changed, ask the reconnector to
			// renew the connection. when disconnected there's nothing to do:
			// every dial attempt reads the current config anyway
			conn, ok := sio.reconnector.Current()
			if !ok {
				continue
			}

			config := sio.deej.config.Values()

			if config.ConnectionInfo != conn.connInfo {
				sio.logger.Info("Detected change in connection parameters, attempting to renew connection")
				sio.reconnector.Reconnect(errors.New("connection parameters changed"))
			}
		}
	}()
}

// watch reads lines off a single connection until it fails; closing the port
// unblocks the pending read
func (sio *SerialIO) watch(conn serialConn, errChannel chan<- error) {
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

		if sio.deej.Verbose() {
			logger.Debugw("Read new line", "line", line)
		}

		sio.handleLine(logger, line)
	}
}

func (sio *SerialIO) handleLine(logger *zap.SugaredLogger, line string) {
	// this function receives an unsanitized line which is guaranteed to end with LF,
	// but most lines will end with CRLF. it may also have garbage instead of
	// deej-formatted values, so we must check for that! just ignore bad ones
	if !expectedLinePattern.MatchString(line) {
		return
	}

	// trim the suffix
	line = strings.TrimSuffix(line, "\r\n")

	// split on pipe (|), this gives a slice of numerical strings between "0" and "1023"
	splitLine := strings.Split(line, "|")
	numSliders := len(splitLine)

	// turns out the first line could come out dirty sometimes (i.e. "4558|925|41|643|220")
	// so let's check the first number for correctness just in case
	if firstNumber, _ := strconv.Atoi(splitLine[0]); firstNumber > 1023 {
		logger.Debugw("Got malformed line from serial, ignoring", "line", line)
		return
	}

	config := sio.deej.config.Values()

	sio.stateLock.Lock()

	// update our slider count, if needed - this will send slider move events for all
	if numSliders != sio.lastKnownNumSliders {
		logger.Infow("Detected sliders", "amount", numSliders)
		sio.lastKnownNumSliders = numSliders
		sio.currentSliderValues = make([]int, numSliders)

		// reset everything to be an impossible value to force the slider move event later
		for idx := range sio.currentSliderValues {
			sio.currentSliderValues[idx] = -1023
		}
	}

	// for each slider:
	moveEvents := []SliderMoveEvent{}
	for sliderIdx, stringValue := range splitLine {

		// convert string values to integers ("1023" -> 1023)
		number, _ := strconv.Atoi(stringValue)

		// map the value from raw to a "dirty" float between 0 and 1 (e.g. 0.15451...)
		dirtyFloat := float32(number) / 1023.0

		// normalize it to an actual volume scalar between 0.0 and 1.0 with 2 points of precision
		normalizedScalar := util.NormalizeScalar(dirtyFloat)

		// if sliders are inverted, take the complement of 1.0
		if config.InvertSliders {
			normalizedScalar = 1 - normalizedScalar
		}

		// check if it changes the desired state (could just be a jumpy raw slider value)
		if util.SignificantlyDifferent(sio.currentSliderValues[sliderIdx], number, config.NoiseReductionLevel) {

			// if it does, update the saved value and create a move event
			sio.currentSliderValues[sliderIdx] = number

			moveEvents = append(moveEvents, SliderMoveEvent{
				SliderID:     sliderIdx,
				PercentValue: normalizedScalar,
			})

			if sio.deej.Verbose() {
				logger.Debugw("Slider moved", "event", moveEvents[len(moveEvents)-1])
			}
		}
	}

	// release the lock before delivering events, since sends can block on consumers
	sio.stateLock.Unlock()

	// deliver move events if there are any, towards all potential consumers
	if len(moveEvents) > 0 {
		for _, consumer := range sio.sliderMoveConsumers {
			for _, moveEvent := range moveEvents {
				consumer <- moveEvent
			}
		}
	}
}
