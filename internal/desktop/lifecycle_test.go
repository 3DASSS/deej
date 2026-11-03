//go:build !headless

package desktop

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func awaitDesktop(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(description)
	}
}

func TestCoreShutdownKeepsEventConsumersUntilFinalState(t *testing.T) {
	started, requested, quit, consumerDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	states := make(chan bool)
	var received atomic.Int32
	var requestOnce sync.Once
	d := New(Options{
		Logger:      zap.NewNop().Sugar(),
		Start:       func() { close(started) },
		Wait:        func() { <-requested },
		RequestStop: func() { requestOnce.Do(func() { close(requested) }) },
		Stop: func() error {
			states <- false // Serial's synchronous final disconnected event.
			return errors.New("audio release failed")
		},
	})
	d.eventWorkers.Go(func() {
		defer close(consumerDone)
		for {
			select {
			case <-states:
				received.Add(1)
			case <-d.eventsDone:
				return
			}
		}
	})
	d.runtimeWorkers.Go(func() { d.runCoreWithQuit(func() { close(quit) }) })
	awaitDesktop(t, started, "core was not started")
	d.requestStop()
	awaitDesktop(t, quit, "shutdown could not deliver its final state event")
	d.runtimeWorkers.Wait()
	awaitDesktop(t, consumerDone, "event consumer survived shutdown")
	if !d.allowQuit.Load() || received.Load() != 1 {
		t.Fatalf("quit allowed=%v, final state count=%d", d.allowQuit.Load(), received.Load())
	}
	var repeated sync.WaitGroup
	for range 8 {
		repeated.Go(d.stopEvents)
	}
	repeated.Wait()
}

func TestRunFailureCleanupCoordinatesWithQuitRequest(t *testing.T) {
	started, requested, consumerDone, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	states := make(chan bool)
	var requestOnce, stopOnce sync.Once
	var quits atomic.Int32
	d := New(Options{
		Logger:      zap.NewNop().Sugar(),
		Start:       func() { close(started) },
		Wait:        func() { <-requested },
		RequestStop: func() { requestOnce.Do(func() { close(requested) }) },
		Stop: func() error {
			stopOnce.Do(func() {
				states <- false
			})
			return nil
		},
	})
	d.eventWorkers.Go(func() {
		defer close(consumerDone)
		for {
			select {
			case <-states:
			case <-d.eventsDone:
				return
			}
		}
	})
	d.runtimeWorkers.Go(func() { d.runCoreWithQuit(func() { quits.Add(1) }) })
	awaitDesktop(t, started, "core was not started")
	go func() { d.cleanupCore(); close(cleaned) }()
	awaitDesktop(t, cleaned, "Run failure cleanup raced shutdown and left a blocked worker")
	awaitDesktop(t, consumerDone, "cleanup did not stop event consumers")
	if quits.Load() != 0 || !d.closing.Load() {
		t.Fatalf("late GUI quits=%d, closing=%v", quits.Load(), d.closing.Load())
	}
}

func TestRunFailureBeforeApplicationStartedStillStopsCore(t *testing.T) {
	var starts, stops atomic.Int32
	requested := make(chan struct{})
	d := New(Options{
		Logger:      zap.NewNop().Sugar(),
		Start:       func() { starts.Add(1) },
		Wait:        func() { <-requested },
		RequestStop: func() { close(requested) },
		Stop:        func() error { stops.Add(1); return nil },
	})
	d.cleanupCore()
	if starts.Load() != 0 || stops.Load() != 1 || !d.closing.Load() {
		t.Fatalf("early failure started=%d, stopped=%d, closing=%v", starts.Load(), stops.Load(), d.closing.Load())
	}
	select {
	case <-d.eventsDone:
	default:
		t.Fatal("early failure did not close event lifetime")
	}
}
