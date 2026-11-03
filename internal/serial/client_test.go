package serial

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/reconnect"
	transport "go.bug.st/serial"
	"go.uber.org/zap"
)

// Embedding the transport interface leaves unused operations out of the fake.
type blockingPort struct {
	transport.Port
	closed chan struct{}
	once   sync.Once
}

func (p *blockingPort) Read([]byte) (int, error) {
	<-p.closed
	return 0, io.EOF
}

func (p *blockingPort) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func testConnectedClient(t *testing.T, dial func() (serialConn, error)) *Client {
	t.Helper()
	settings := &fakeSettings{value: &config.Settings{}, changes: make(chan bool)}
	client, err := New(settings, zap.NewNop().Sugar(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	client.reconnector = reconnect.New(reconnect.Options[serialConn]{
		Dial: dial, Watch: client.watch, Close: client.close, OnUp: client.onUp,
	})
	t.Cleanup(client.Stop)
	return client
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for serial shutdown")
	}
}

func TestStopDeliversDisconnectedBeforeConsumersExit(t *testing.T) {
	port := &blockingPort{closed: make(chan struct{})}
	client := testConnectedClient(t, func() (serialConn, error) {
		return serialConn{port: port, adopted: new(atomic.Bool)}, nil
	})
	states := client.SubscribeToStateChangeEvent()
	client.Start()
	select {
	case connected := <-states:
		if !connected {
			t.Fatal("first state should be connected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not start")
	}
	stopped := make(chan struct{})
	go func() { client.Stop(); close(stopped) }()
	select {
	case connected := <-states:
		if connected {
			t.Fatal("Stop should report disconnected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not deliver disconnected")
	}
	waitForSignal(t, stopped)
	// A repeated Stop must neither close channels twice nor block.
	client.Stop()
}

func TestLateDialCloseDoesNotSendAfterStop(t *testing.T) {
	port := &blockingPort{closed: make(chan struct{})}
	dialStarted, dialFinish := make(chan struct{}), make(chan struct{})
	client := testConnectedClient(t, func() (serialConn, error) {
		close(dialStarted)
		<-dialFinish
		return serialConn{port: port, adopted: new(atomic.Bool)}, nil
	})
	// There is deliberately no receiver once Stop returns.
	client.SubscribeToStateChangeEvent()
	client.Start()
	waitForSignal(t, dialStarted)
	stopped := make(chan struct{})
	go func() { client.Stop(); close(stopped) }()
	waitForSignal(t, stopped)
	close(dialFinish)
	waitForSignal(t, port.closed)
}

func TestConfigReloadReemitsStationarySliders(t *testing.T) {
	provider := &fakeSettings{value: &config.Settings{}, changes: make(chan bool)}
	client, err := New(provider, zap.NewNop().Sugar(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Stop)
	events := make(chan SliderMoveEvent, 4)
	client.sliderMoveConsumers = append(client.sliderMoveConsumers, events)
	client.handleLine(client.logger, "512|1023\r\n")
	drainEvents(events)
	client.handleLine(client.logger, "512|1023\r\n")
	if got := drainEvents(events); len(got) != 0 {
		t.Fatalf("unchanged line emitted %d events before reload", len(got))
	}
	// The second unbuffered send synchronizes with completion of the first
	// reset, just as two coalesced reload generations would.
	provider.changes <- true
	provider.changes <- true
	client.handleLine(client.logger, "512|1023\r\n")
	if got := drainEvents(events); len(got) != 2 {
		t.Fatalf("unchanged line emitted %d events after reload, want 2", len(got))
	}
}
