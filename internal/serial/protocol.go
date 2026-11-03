package serial

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

var expectedLinePattern = regexp.MustCompile(`^\d{1,4}(\|\d{1,4})*\r\n$`)

// CurrentSliderPercentValues returns the current slider values as the 0..1
// scalars the sessions receive (normalized and inversion-applied, mirroring
// handleLine)
func (sio *Client) CurrentSliderPercentValues() []float32 {
	config := sio.settings.Values()

	sio.stateLock.Lock()
	defer sio.stateLock.Unlock()

	values := make([]float32, len(sio.currentSliderValues))
	for i, raw := range sio.currentSliderValues {
		if raw < 0 {
			// sentinel value before the first full line is received
			continue
		}

		value := normalizeScalar(float32(raw) / 1023.0)
		if config.InvertSliders {
			value = 1 - value
		}
		values[i] = value
	}

	return values
}

func (sio *Client) handleLine(logger *zap.SugaredLogger, line string) {
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

	config := sio.settings.Values()

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
		normalizedScalar := normalizeScalar(dirtyFloat)

		// if sliders are inverted, take the complement of 1.0
		if config.InvertSliders {
			normalizedScalar = 1 - normalizedScalar
		}

		// check if it changes the desired state (could just be a jumpy raw slider value)
		if significantlyDifferent(sio.currentSliderValues[sliderIdx], number, config.NoiseReduction) {

			// if it does, update the saved value and create a move event
			sio.currentSliderValues[sliderIdx] = number

			moveEvents = append(moveEvents, SliderMoveEvent{
				SliderID:     sliderIdx,
				PercentValue: normalizedScalar,
			})

			if sio.options.Verbose {
				logger.Debugw("Slider moved", "event", moveEvents[len(moveEvents)-1])
			}
		}
	}

	// release the lock before delivering events, since sends can block on consumers
	sio.stateLock.Unlock()

	// deliver move events if there are any, towards all potential consumers
	if len(moveEvents) > 0 {
		sio.consumersLock.Lock()
		consumers := slices.Clone(sio.sliderMoveConsumers)
		sio.consumersLock.Unlock()
		for _, consumer := range consumers {
			for _, moveEvent := range moveEvents {
				consumer <- moveEvent
			}
		}
	}
}
