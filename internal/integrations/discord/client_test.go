package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/config"
	"go.uber.org/zap"
)

func newTestClient() *Client {
	return &Client{
		pending:  map[string]float32{},
		lastSent: map[string]float32{},
	}
}

type testSettings struct {
	values  *config.Settings
	changes chan bool
}

func (p *testSettings) Values() *config.Settings      { return p.values }
func (p *testSettings) SubscribeToChanges() chan bool { return p.changes }
func TestNewClientBeforeConfigLoad(t *testing.T) {
	client := New(&testSettings{}, zap.NewNop().Sugar(), filepath.Join(t.TempDir(), TokenFileName), nil)
	if client == nil {
		t.Fatal("expected Discord client")
	}
}

func TestWriteDiscordFrame(t *testing.T) {
	var buf bytes.Buffer

	body := []byte(`{"cmd":"AUTHENTICATE","nonce":"abc"}`)
	if err := writeDiscordFrame(&buf, discordOpFrame, body); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	// Opcode 1 and a 36-byte body, both encoded as little-endian uint32s.
	want := append([]byte{1, 0, 0, 0, 36, 0, 0, 0}, body...)
	if got := buf.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("frame = %x, want %x", got, want)
	}
}

// the transport is a stream, so a frame can arrive in pieces
func TestDiscordFrameReadsAcrossChunks(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 5000)
	// A wire fixture independent of writeDiscordFrame: opcode 1, length 0x1388.
	wire := append([]byte{1, 0, 0, 0, 0x88, 0x13, 0, 0}, body...)

	// hand the reader one byte at a time
	opcode, got, err := readDiscordFrame(iotest(wire))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if opcode != 1 {
		t.Errorf("opcode = %d, want 1", opcode)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body length = %d, want %d", len(got), len(body))
	}
}

// oneByteReader hands out a single byte per Read, the worst case io.ReadFull
// has to cope with
type oneByteReader struct {
	data []byte
}

func iotest(data []byte) io.Reader {
	return &oneByteReader{data: data}
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}

	p[0] = r.data[0]
	r.data = r.data[1:]

	return 1, nil
}

func TestDiscordFrameRejectsOversizedLength(t *testing.T) {
	// A length of 1 MiB + 1 must fail before trying to read the absent body.
	header := []byte{1, 0, 0, 0, 1, 0, 0x10, 0}
	const wantError = "frame too large: 1048577 bytes"
	if _, _, err := readDiscordFrame(bytes.NewReader(header)); err == nil || err.Error() != wantError {
		t.Fatalf("oversized frame error = %v, want %s", err, wantError)
	}
}

func TestDiscordVolume(t *testing.T) {
	cases := []struct {
		percent float32
		want    float64
	}{
		{0, 0},
		{0.5, 50},
		{1, 100},
		{0.335, 34}, // rounded, not truncated
		{-1, 0},     // clamped
		{2, 100},
	}

	for _, testCase := range cases {
		if got := discordVolume(testCase.percent); got != testCase.want {
			t.Errorf("discordVolume(%v) = %v, want %v", testCase.percent, got, testCase.want)
		}
	}
}

func TestDiscordUserVolume(t *testing.T) {
	cases := []struct {
		percent float32
		convert bool
		want    float64
	}{
		{0, true, 0},
		{0.01, true, 0.0001},
		{0.02, true, 0.0008},
		{0.5, true, 12.5},
		{1, true, 100},
		{0.5, false, 50},
		{-1, false, 0}, // clamped
		{2, false, 100},
	}

	for _, testCase := range cases {
		if got := discordUserVolume(testCase.percent, testCase.convert); math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("discordUserVolume(%v, %v) = %v, want %v", testCase.percent, testCase.convert, got, testCase.want)
		}
	}
}

func TestDiscordReplyDataUnwrapsError(t *testing.T) {
	data, err := discordReplyData(discordFrame{Data: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("data = %s", data)
	}

	_, err = discordReplyData(discordFrame{
		Evt:  "ERROR",
		Data: json.RawMessage(`{"code":4006,"message":"Not authenticated"}`),
	})
	if err == nil {
		t.Fatal("expected an error for an ERROR frame")
	}
	if got := err.Error(); got != "discord rpc error 4006: Not authenticated" {
		t.Errorf("error = %q", got)
	}
}

func TestDiscordConnectionFailureWakesEveryRequest(t *testing.T) {
	conn := &discordConn{
		rw:      &testDiscordTransport{},
		pending: map[string]chan discordFrame{},
		done:    make(chan struct{}),
	}

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := conn.requestWithTimeout("TEST", nil, 5*time.Second)
			results <- err
		}()
	}

	deadline := time.Now().Add(time.Second)
	for {
		conn.pendingLock.Lock()
		pending := len(conn.pending)
		conn.pendingLock.Unlock()
		if pending == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("requests did not become pending")
		}
		time.Sleep(time.Millisecond)
	}

	want := errors.New("connection lost")
	conn.fail(want)
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, want) {
				t.Errorf("request error = %v, want %v", err, want)
			}
		case <-time.After(time.Second):
			t.Fatal("request remained blocked after connection failure")
		}
	}
}

func TestDiscordVoiceStateSubscriptionFailureIsReported(t *testing.T) {
	done := make(chan struct{})
	close(done)

	conn := &discordConn{
		rw:      &testDiscordTransport{},
		pending: map[string]chan discordFrame{},
		done:    done,
		doneErr: errors.New("connection lost"),
	}
	client := newTestClient()
	client.logger = zap.NewNop().Sugar()

	if client.resubscribeVoiceStates(conn, "", "channel") {
		t.Fatal("failed subscriptions must not be reported as successful")
	}
}

type testDiscordTransport struct {
	bytes.Buffer
}

func (*testDiscordTransport) Close() error { return nil }

func TestDiscordThrottleSkipsUnchangedValues(t *testing.T) {
	client := newTestClient()

	client.pending[discordKeyOutput] = 0.5
	client.markSent(client.takePending())

	// the same value again is a no-op...
	client.pending[discordKeyOutput] = 0.5
	if todo := client.takePending(); len(todo) != 0 {
		t.Errorf("unchanged value should not be resent, got %v", todo)
	}

	// ...but a different one still goes out
	client.pending[discordKeyOutput] = 0.6
	if todo := client.takePending(); todo[discordKeyOutput] != 0.6 {
		t.Errorf("changed value should be sent, got %v", todo)
	}
}

// the roster is maintained incrementally from VOICE_STATE_* events, so the
// folding has to add, update, remove and keep the list sorted
func TestDiscordApplyVoiceStateEvent(t *testing.T) {
	client := newTestClient()
	client.roster.Store(&discordRoster{id: "c1", channel: "General"})

	names := func() []string {
		roster := client.roster.Load()
		out := make([]string, 0, len(roster.users))
		for _, user := range roster.users {
			out = append(out, user.Name+"="+user.ID)
		}
		return out
	}

	apply := func(name string, payload string) {
		client.applyVoiceStateEvent(discordEvent{name: name, data: json.RawMessage(payload)})
	}

	apply("VOICE_STATE_CREATE", `{"user":{"id":"2","username":"zoe"}}`)
	apply("VOICE_STATE_CREATE", `{"user":{"id":"1","username":"adam"}}`)

	if got := names(); !slices.Equal(got, []string{"adam=1", "zoe=2"}) {
		t.Fatalf("after two joins: %v, want them sorted by name", got)
	}

	// a nickname wins over the global name, and an update replaces in place
	apply("VOICE_STATE_UPDATE", `{"nick":"Bob","user":{"id":"2","username":"zoe","global_name":"Zoe"}}`)

	if got := names(); !slices.Equal(got, []string{"adam=1", "Bob=2"}) {
		t.Fatalf("after a rename: %v", got)
	}

	apply("VOICE_STATE_DELETE", `{"user":{"id":"1","username":"adam"}}`)

	if got := names(); !slices.Equal(got, []string{"Bob=2"}) {
		t.Fatalf("after a leave: %v", got)
	}

	// the channel identity survives the folding
	if roster := client.roster.Load(); roster.id != "c1" || roster.channel != "General" {
		t.Errorf("channel lost: %+v", roster)
	}

	// events for someone already gone, and malformed ones, change nothing
	apply("VOICE_STATE_DELETE", `{"user":{"id":"404","username":"ghost"}}`)
	apply("VOICE_STATE_CREATE", `not json`)
	apply("VOICE_STATE_CREATE", `{"user":{}}`)

	if got := names(); !slices.Equal(got, []string{"Bob=2"}) {
		t.Errorf("roster should be unchanged: %v", got)
	}
}

// an event arriving before the first reconcile has no roster to fold into,
// and must not invent one out of a single member
func TestDiscordVoiceStateEventWithoutRoster(t *testing.T) {
	client := newTestClient()

	client.applyVoiceStateEvent(discordEvent{
		name: "VOICE_STATE_CREATE",
		data: json.RawMessage(`{"user":{"id":"1","username":"adam"}}`),
	})

	if roster := client.roster.Load(); roster != nil {
		t.Errorf("expected no roster, got %+v", roster)
	}
}

func TestDiscordResolveUser(t *testing.T) {
	client := newTestClient()
	client.roster.Store(&discordRoster{
		channel: "General",
		users:   []User{{ID: "123", Name: "Nikita"}},
	})

	// case-insensitive display name match
	if id, ok := client.resolveUser("nikita"); !ok || id != "123" {
		t.Errorf("resolveUser(nikita) = %q, %v; want 123, true", id, ok)
	}

	// an all-digit target is a user id, and works with nobody in the roster
	if id, ok := client.resolveUser("987654321"); !ok || id != "987654321" {
		t.Errorf("resolveUser(987654321) = %q, %v; want it passed through", id, ok)
	}

	if _, ok := client.resolveUser("someone else"); ok {
		t.Error("expected a miss for a user who isn't in the channel")
	}
}

func TestDiscordCredentialsChanged(t *testing.T) {
	base := config.DiscordSettings{Enabled: true, ClientID: "123", ClientSecret: "secret"}

	if discordCredentialsChanged(base, base) {
		t.Fatal("unchanged credentials should keep the linked account")
	}

	changedID := base
	changedID.ClientID = "456"
	if !discordCredentialsChanged(base, changedID) {
		t.Fatal("changing the client ID should invalidate the linked account")
	}

	changedSecret := base
	changedSecret.ClientSecret = "new-secret"
	if !discordCredentialsChanged(base, changedSecret) {
		t.Fatal("changing the client secret should invalidate the linked account")
	}
}
