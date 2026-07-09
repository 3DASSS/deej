package deej

import (
	"testing"

	"go.uber.org/zap"
)

// newTestSerialIO builds a SerialIO with just enough wiring for handleLine,
// plus a buffered channel that captures emitted slider move events.
func newTestSerialIO(invertSliders bool, noiseReductionLevel string) (*SerialIO, chan SliderMoveEvent) {
	config := &CanonicalConfig{}
	config.current.Store(&ConfigValues{
		InvertSliders:       invertSliders,
		NoiseReductionLevel: noiseReductionLevel,
	})

	sio := &SerialIO{
		deej:   &Deej{config: config},
		logger: zap.NewNop().Sugar(),
	}

	events := make(chan SliderMoveEvent, 64)
	sio.sliderMoveConsumers = append(sio.sliderMoveConsumers, events)

	return sio, events
}

func drainEvents(ch chan SliderMoveEvent) []SliderMoveEvent {
	var events []SliderMoveEvent
	for {
		select {
		case e := <-ch:
			events = append(events, e)
		default:
			return events
		}
	}
}

func TestHandleLineParsesValidLine(t *testing.T) {
	sio, ch := newTestSerialIO(false, "")

	sio.handleLine(sio.logger, "0|512|1023\r\n")

	events := drainEvents(ch)
	if len(events) != 3 {
		t.Fatalf("got %d events, expected 3", len(events))
	}

	expected := []SliderMoveEvent{
		{SliderID: 0, PercentValue: 0.0},
		{SliderID: 1, PercentValue: 0.5},
		{SliderID: 2, PercentValue: 1.0},
	}
	for i, e := range events {
		if e != expected[i] {
			t.Errorf("event %d = %+v, expected %+v", i, e, expected[i])
		}
	}
}

func TestHandleLineIgnoresMalformedLines(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"garbage", "hello world\r\n"},
		{"missing CR", "512|512\n"},
		{"missing line ending", "512|512"},
		{"non-numeric value", "512|abc\r\n"},
		{"negative value", "-1|512\r\n"},
		{"five digit value", "10000|512\r\n"},
		{"trailing pipe", "512|512|\r\n"},
		{"dirty first value", "4558|925|41\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sio, ch := newTestSerialIO(false, "")

			sio.handleLine(sio.logger, tt.line)

			if events := drainEvents(ch); len(events) != 0 {
				t.Errorf("line %q produced %d events, expected none", tt.line, len(events))
			}
		})
	}
}

func TestHandleLineNoiseReduction(t *testing.T) {
	sio, ch := newTestSerialIO(false, "")

	// first line always emits (values initialized to an impossible -1023)
	sio.handleLine(sio.logger, "512|512\r\n")
	if events := drainEvents(ch); len(events) != 2 {
		t.Fatalf("initial line produced %d events, expected 2", len(events))
	}

	// jitter below the default threshold of 10 must be filtered out
	sio.handleLine(sio.logger, "515|509\r\n")
	if events := drainEvents(ch); len(events) != 0 {
		t.Errorf("noisy line produced %d events, expected none", len(events))
	}

	// a real move at/above the threshold must go through
	sio.handleLine(sio.logger, "525|512\r\n")
	events := drainEvents(ch)
	if len(events) != 1 {
		t.Fatalf("got %d events, expected 1", len(events))
	}
	if events[0].SliderID != 0 {
		t.Errorf("event came from slider %d, expected 0", events[0].SliderID)
	}
}

func TestHandleLineNoiseReductionNone(t *testing.T) {
	sio, ch := newTestSerialIO(false, "none")

	sio.handleLine(sio.logger, "512\r\n")
	drainEvents(ch)

	// with "none", even a difference of 1 is significant
	sio.handleLine(sio.logger, "513\r\n")
	if events := drainEvents(ch); len(events) != 1 {
		t.Errorf("got %d events, expected 1", len(events))
	}
}

func TestHandleLineEdgeSnapping(t *testing.T) {
	sio, ch := newTestSerialIO(false, "")

	sio.handleLine(sio.logger, "1013\r\n")
	drainEvents(ch)

	// near the top edge the threshold drops to 5, so a move of 5 emits
	// even though it is below the default threshold of 10
	sio.handleLine(sio.logger, "1018\r\n")
	events := drainEvents(ch)
	if len(events) != 1 {
		t.Fatalf("got %d events, expected 1", len(events))
	}
	if events[0].PercentValue != 1.0 {
		t.Errorf("edge value = %v, expected 1.0", events[0].PercentValue)
	}
}

func TestHandleLineSliderCountChange(t *testing.T) {
	sio, ch := newTestSerialIO(false, "")

	sio.handleLine(sio.logger, "512\r\n")
	if events := drainEvents(ch); len(events) != 1 {
		t.Fatalf("got %d events, expected 1", len(events))
	}
	if sio.lastKnownNumSliders != 1 {
		t.Errorf("lastKnownNumSliders = %d, expected 1", sio.lastKnownNumSliders)
	}

	// a different slider count resets state and re-emits everything,
	// including sliders whose values did not change
	sio.handleLine(sio.logger, "512|512\r\n")
	if events := drainEvents(ch); len(events) != 2 {
		t.Errorf("got %d events after count change, expected 2", len(events))
	}
	if sio.lastKnownNumSliders != 2 {
		t.Errorf("lastKnownNumSliders = %d, expected 2", sio.lastKnownNumSliders)
	}
}
