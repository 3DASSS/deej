package discord

import (
	"encoding/json"
	"maps"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/reconnect"
	"go.uber.org/zap"
)

// The RPC peer exercises reconciliation and flushes through real framed
// requests, without requiring a running Discord client or private app fixtures.
func testRPCClient(t *testing.T, reply func(discordFrame) json.RawMessage) *Client {
	t.Helper()
	rw, peer := net.Pipe()
	go func() {
		defer func() { _ = peer.Close() }()
		for {
			_, body, err := readDiscordFrame(peer)
			if err != nil {
				return
			}
			var request discordFrame
			if err := json.Unmarshal(body, &request); err != nil {
				return
			}
			response := discordFrame{Cmd: request.Cmd, Nonce: request.Nonce, Data: reply(request)}
			body, err = json.Marshal(response)
			if err != nil || writeDiscordFrame(peer, discordOpFrame, body) != nil {
				return
			}
		}
	}()
	logger := zap.NewNop().Sugar()
	client := New(&testSettings{values: &config.Settings{}}, logger, "", nil)
	conn := newDiscordConn(rw, config.DiscordSettings{}, client.queueEvent)
	ready := make(chan struct{})
	client.reconnector = reconnect.New(reconnect.Options[*discordConn]{
		Logger: logger,
		Dial:   func() (*discordConn, error) { return conn, nil },
		Watch:  client.watch,
		Close:  client.close,
		OnUp:   func(*discordConn) { close(ready) },
	})
	client.reconnector.Start()
	t.Cleanup(client.reconnector.Stop)
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("RPC peer was not connected")
	}
	return client
}

func TestDiscordThrottleCollapsesBurst(t *testing.T) {
	requests := make(chan discordFrame, 8)
	client := testRPCClient(t, func(request discordFrame) json.RawMessage {
		requests <- request
		return json.RawMessage(`{}`)
	})
	for _, percent := range []float32{0.1, 0.2, 0.3, 0.4} {
		client.SetVolume(InputTarget, percent)
	}
	if got := len(requests); got != 0 {
		t.Fatalf("burst sent %d requests before flush", got)
	}
	client.flush()
	if got := len(requests); got != 1 {
		t.Fatalf("flush sent %d requests, want 1", got)
	}
	request := <-requests
	wantArgs := map[string]any{"input": map[string]any{"volume": float64(40)}}
	if request.Cmd != "SET_VOICE_SETTINGS" || !reflect.DeepEqual(request.Args, wantArgs) {
		t.Errorf("request = %s %+v, want SET_VOICE_SETTINGS %+v", request.Cmd, request.Args, wantArgs)
	}
	if len(client.pending) != 0 {
		t.Errorf("pending should be drained, got %d entries", len(client.pending))
	}
}

func TestReconciliationRestoresVolumesWithoutSliderMovement(t *testing.T) {
	var mu sync.Mutex
	channel := json.RawMessage(`{"id":"one","name":"First","voice_states":[{"user":{"id":"1","username":"Nikita"}}]}`)
	client := testRPCClient(t, func(request discordFrame) json.RawMessage {
		if request.Cmd == "GET_SELECTED_VOICE_CHANNEL" {
			mu.Lock()
			defer mu.Unlock()
			return channel
		}
		return json.RawMessage(`{}`)
	})
	client.currentVolumes = func() []TargetVolume {
		return []TargetVolume{{Target: "user:nikita", Percent: 0.25}}
	}
	var subscribedConn *discordConn
	var subscribedChannel string
	reconcile := func() { client.reconcileRoster(&subscribedConn, &subscribedChannel) }
	assertRestored := func() {
		t.Helper()
		pending := client.takePending()
		if !maps.Equal(pending, map[string]float32{"user:nikita": 0.25}) {
			t.Fatalf("current volume was not restored: %v", pending)
		}
		client.markSent(pending)
	}
	reconcile()
	assertRestored()
	reconcile()
	if pending := client.takePending(); len(pending) != 0 {
		t.Fatalf("unchanged roster should retain deduplication: %v", pending)
	}
	mu.Lock()
	channel = json.RawMessage(`{"id":"two","name":"Second","voice_states":[{"user":{"id":"1","username":"Nikita"}}]}`)
	mu.Unlock()
	reconcile()
	assertRestored()
	mu.Lock()
	channel = json.RawMessage(`{"id":"two","name":"Second","voice_states":[]}`)
	mu.Unlock()
	reconcile()
	client.takePending()
	mu.Lock()
	channel = json.RawMessage(`{"id":"two","name":"Second","voice_states":[{"user":{"id":"1","username":"Nikita"}}]}`)
	mu.Unlock()
	reconcile()
	assertRestored()
}

func TestVoiceJoinAndRejoinReplayCurrentCallbackValues(t *testing.T) {
	client := newTestClient()
	percent := float32(0.25)
	client.currentVolumes = func() []TargetVolume { return []TargetVolume{{Target: "user:nikita", Percent: percent}} }
	client.roster.Store(&discordRoster{id: "channel"})
	join := discordEvent{name: "VOICE_STATE_CREATE", data: json.RawMessage(`{"user":{"id":"1","username":"Nikita"}}`)}
	client.applyVoiceStateEvent(join)
	client.markSent(client.takePending())
	client.applyVoiceStateEvent(discordEvent{name: "VOICE_STATE_DELETE", data: join.data})
	percent = 0.75
	client.applyVoiceStateEvent(join)
	if got := client.takePending(); !maps.Equal(got, map[string]float32{"user:nikita": percent}) {
		t.Fatalf("rejoin did not replay the current slider value: %v", got)
	}
}

func TestOldFlushCannotSuppressRestoration(t *testing.T) {
	requested, respond := make(chan struct{}), make(chan struct{})
	client := testRPCClient(t, func(request discordFrame) json.RawMessage {
		if request.Cmd == "SET_VOICE_SETTINGS" {
			close(requested)
			<-respond
		}
		return json.RawMessage(`{}`)
	})
	client.pending[InputTarget] = 0.5
	finished := make(chan struct{})
	go func() { client.flush(); close(finished) }()
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("volume request was not sent")
	}
	client.invalidateVolumes()
	client.currentVolumes = func() []TargetVolume { return []TargetVolume{{Target: InputTarget, Percent: 0.5}} }
	client.replayMappedSliderValues()
	close(respond)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("flush did not complete")
	}
	if got := client.takePending(); !maps.Equal(got, map[string]float32{InputTarget: 0.5}) {
		t.Fatalf("old request suppressed the restored value: %v", got)
	}
}

func TestConcurrentStartStopWithoutAuthorization(t *testing.T) {
	client := New(&testSettings{values: &config.Settings{}}, zap.NewNop().Sugar(), "", nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { client.Start(); client.Start(); client.Stop(); client.Stop() })
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent lifecycle requests did not finish")
	}
}
