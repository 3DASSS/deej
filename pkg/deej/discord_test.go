package deej

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestDiscordClient() *DiscordClient {
	return &DiscordClient{
		pending:  map[string]float32{},
		lastSent: map[string]float32{},
	}
}

func TestNewDiscordClientBeforeConfigLoad(t *testing.T) {
	config := &CanonicalConfig{configPath: filepath.Join(t.TempDir(), "config.yaml")}
	deej := &Deej{config: config}

	client := NewDiscordClient(deej, zap.NewNop().Sugar())
	if client == nil {
		t.Fatal("expected Discord client")
	}
}

func TestDiscordFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	body := []byte(`{"cmd":"AUTHENTICATE","nonce":"abc"}`)
	if err := writeDiscordFrame(&buf, discordOpFrame, body); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	opcode, got, err := readDiscordFrame(&buf)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if opcode != discordOpFrame {
		t.Errorf("opcode = %d, want %d", opcode, discordOpFrame)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

// the transport is a stream, so a frame can arrive in pieces
func TestDiscordFrameReadsAcrossChunks(t *testing.T) {
	var buf bytes.Buffer

	body := bytes.Repeat([]byte("x"), 5000)
	if err := writeDiscordFrame(&buf, discordOpFrame, body); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	// hand the reader one byte at a time
	opcode, got, err := readDiscordFrame(iotest(buf.Bytes()))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if opcode != discordOpFrame {
		t.Errorf("opcode = %d, want %d", opcode, discordOpFrame)
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
	var buf bytes.Buffer

	// a header claiming more than the cap, with no body behind it
	if err := writeDiscordFrame(&buf, discordOpFrame, nil); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	header := buf.Bytes()
	header[4], header[5], header[6], header[7] = 0xFF, 0xFF, 0xFF, 0xFF

	if _, _, err := readDiscordFrame(bytes.NewReader(header)); err == nil {
		t.Fatal("expected an error for an oversized frame length")
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
		want    float64
	}{
		{0, 0},
		{0.01, 0.0001},
		{0.02, 0.0008},
		{0.5, 12.5},
		{1, 100},
		{-1, 0}, // clamped
		{2, 100},
	}

	for _, testCase := range cases {
		if got := discordUserVolume(testCase.percent); math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("discordUserVolume(%v) = %v, want %v", testCase.percent, got, testCase.want)
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
	client := newTestDiscordClient()
	client.logger = zap.NewNop().Sugar()

	if client.resubscribeVoiceStates(conn, "", "channel") {
		t.Fatal("failed subscriptions must not be reported as successful")
	}
}

type testDiscordTransport struct {
	bytes.Buffer
}

func (*testDiscordTransport) Close() error { return nil }

// a slider sweep must collapse to one send per target, and the final resting
// position must be the value that survives
func TestDiscordThrottleCollapsesBurst(t *testing.T) {
	client := newTestDiscordClient()

	for _, percent := range []float32{0.1, 0.2, 0.3, 0.4} {
		client.pending[discordKeyInput] = percent
	}

	todo := client.takePending()
	if len(todo) != 1 {
		t.Fatalf("takePending returned %d entries, want 1", len(todo))
	}
	if todo[discordKeyInput] != 0.4 {
		t.Errorf("value = %v, want the last one (0.4)", todo[discordKeyInput])
	}

	if len(client.pending) != 0 {
		t.Errorf("pending should be drained, got %d entries", len(client.pending))
	}
}

func TestDiscordThrottleSkipsUnchangedValues(t *testing.T) {
	client := newTestDiscordClient()

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
	client := newTestDiscordClient()
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
	client := newTestDiscordClient()

	client.applyVoiceStateEvent(discordEvent{
		name: "VOICE_STATE_CREATE",
		data: json.RawMessage(`{"user":{"id":"1","username":"adam"}}`),
	})

	if roster := client.roster.Load(); roster != nil {
		t.Errorf("expected no roster, got %+v", roster)
	}
}

func TestDiscordResolveUser(t *testing.T) {
	client := newTestDiscordClient()
	client.roster.Store(&discordRoster{
		channel: "General",
		users:   []DiscordUserDTO{{ID: "123", Name: "Nikita"}},
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

// the slider handler has to route discord targets away from the audio session
// path, and leave everything else alone
func TestDiscordTargetRouting(t *testing.T) {
	sessions := &sessionMap{deej: &Deej{}}

	handled := []string{
		"deej.discord.input",
		"deej.discord.output",
		"deej.discord:Nikita",
		"DEEJ.DISCORD:Nikita", // targets are case-insensitive
	}

	for _, target := range handled {
		if !sessions.applySpecialTargetAction(target, 0.5) {
			t.Errorf("%q should be handled as a discord target", target)
		}
	}

	notHandled := []string{"discord.exe", "master", "deej.current", "deej.discordant"}

	for _, target := range notHandled {
		if sessions.applySpecialTargetAction(target, 0.5) {
			t.Errorf("%q should fall through to the audio session path", target)
		}
	}
}

func TestDiscordReplayMappedSliderValues(t *testing.T) {
	settings := defaultSettings()
	settings.Profiles[0].SliderMapping = SliderMappings{
		{Slider: 0, Targets: []string{"master", "DEEJ.DISCORD.INPUT", "deej.discord:Nikita"}},
		{Slider: 1, Targets: []string{"deej.discord.output"}},
		{Slider: 99, Targets: []string{"deej.discord:missing-slider"}},
	}

	config := &CanonicalConfig{}
	config.current.Store(&settings)
	deej := &Deej{config: config}
	deej.serial = &SerialIO{deej: deej, currentSliderValues: []int{256, 768}}
	client := newTestDiscordClient()
	client.deej = deej
	deej.discord = client

	client.replayMappedSliderValues()

	values := deej.serial.CurrentSliderPercentValues()
	want := map[string]float32{
		discordKeyInput:           values[0],
		discordKeyUser + "nikita": values[0],
		discordKeyOutput:          values[1],
	}
	if !maps.Equal(client.pending, want) {
		t.Errorf("pending = %v, want %v", client.pending, want)
	}

	client.markSent(client.takePending())
	client.roster.Store(&discordRoster{id: "channel"})
	client.applyVoiceStateEvent(discordEvent{
		name: "VOICE_STATE_CREATE",
		data: json.RawMessage(`{"user":{"id":"123","username":"Nikita"}}`),
	})

	if len(client.lastSent) != 0 {
		t.Errorf("join should invalidate sent values, got %v", client.lastSent)
	}
	if !maps.Equal(client.pending, want) {
		t.Errorf("pending after join = %v, want replay %v", client.pending, want)
	}
}

func TestNormalizeDiscordRequiresCredentials(t *testing.T) {
	settings := defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true}

	problems := settings.normalize()
	if len(problems) != 2 {
		t.Fatalf("expected both credentials to be reported, got %v", problems)
	}
	if settings.Discord.Enabled {
		t.Error("enabling without credentials should leave the integration off")
	}

	// a client secret pasted into the client id field is caught up front
	settings = defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true, ClientID: "not-a-snowflake", ClientSecret: "secret"}

	if problems := settings.normalize(); len(problems) != 1 {
		t.Fatalf("expected the non-numeric client id to be reported, got %v", problems)
	}

	// the valid case is left alone, whitespace aside
	settings = defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true, ClientID: " 1234567890 ", ClientSecret: " secret "}

	if problems := settings.normalize(); len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
	if settings.Discord.ClientID != "1234567890" || settings.Discord.ClientSecret != "secret" {
		t.Errorf("credentials should be trimmed, got %+v", settings.Discord)
	}
	if !settings.Discord.Enabled {
		t.Error("valid credentials should stay enabled")
	}
}

func TestDiscordCredentialsChanged(t *testing.T) {
	base := DiscordSettings{Enabled: true, ClientID: "123", ClientSecret: "secret"}

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
