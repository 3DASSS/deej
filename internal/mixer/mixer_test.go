package mixer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/serial"
	"go.uber.org/zap"
)

type fakeSettings struct {
	value   atomic.Pointer[config.Settings]
	mu      sync.Mutex
	changes []chan bool
}

func settingsWithMapping(mapping config.SliderMappings) *fakeSettings {
	f := &fakeSettings{}
	f.value.Store(&config.Settings{ActiveProfile: "default", Profiles: []config.Profile{{Name: "default", SliderMapping: mapping}}})
	return f
}

func (f *fakeSettings) Values() *config.Settings { return f.value.Load() }
func (f *fakeSettings) SubscribeToChanges() chan bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan bool, 1)
	f.changes = append(f.changes, ch)
	return ch
}

type fakeSliders struct {
	events        chan serial.SliderMoveEvent
	subscriptions int
}

func (f *fakeSliders) SubscribeToSliderMoveEvents() chan serial.SliderMoveEvent {
	f.subscriptions++
	return f.events
}
func (f *fakeSliders) send(t *testing.T, slider int, value float32) {
	t.Helper()
	select {
	case f.events <- serial.SliderMoveEvent{SliderID: slider, PercentValue: value}:
	case <-time.After(2 * time.Second):
		t.Fatal("mixer did not receive slider event")
	}
}

type fakeSession struct {
	key, name            string
	device, input        bool
	mu                   sync.Mutex
	volume               float32
	updates              chan float32
	setting, continueSet chan struct{}
	released             atomic.Bool
}

func newSession(key string) *fakeSession {
	return &fakeSession{key: key, updates: make(chan float32, 16)}
}
func (s *fakeSession) Key() string         { return s.key }
func (s *fakeSession) DisplayName() string { return s.name }
func (s *fakeSession) IsDevice() bool      { return s.device }
func (s *fakeSession) IsInput() bool       { return s.input }
func (s *fakeSession) GetVolume() float32  { s.mu.Lock(); defer s.mu.Unlock(); return s.volume }
func (s *fakeSession) SetVolume(v float32) error {
	if s.setting != nil {
		close(s.setting)
		<-s.continueSet
	}
	s.mu.Lock()
	s.volume = v
	s.mu.Unlock()
	s.updates <- v
	return nil
}
func (s *fakeSession) Release() { s.released.Store(true) }

type fakeFinder struct {
	mu       sync.Mutex
	sessions map[audio.Session]bool
	handler  audio.SessionEventHandler
	onStart  func()
}

func newFinder(sessions ...*fakeSession) *fakeFinder {
	f := &fakeFinder{sessions: map[audio.Session]bool{}}
	for _, s := range sessions {
		f.sessions[s] = true
	}
	return f
}
func (f *fakeFinder) Start(handler audio.SessionEventHandler) error {
	f.handler = handler
	if f.onStart != nil {
		f.onStart()
	}
	for session := range f.sessions {
		handler(audio.SessionEvent{Type: audio.SessionEventAdded, Session: session})
	}
	return nil
}
func (f *fakeFinder) remove(session audio.Session) {
	f.mu.Lock()
	present := f.sessions[session]
	delete(f.sessions, session)
	f.mu.Unlock()
	if present {
		f.handler(audio.SessionEvent{Type: audio.SessionEventRemoved, Session: session})
		session.Release()
	}
}
func (f *fakeFinder) Release() error {
	f.mu.Lock()
	sessions := make([]audio.Session, 0, len(f.sessions))
	for s := range f.sessions {
		sessions = append(sessions, s)
	}
	f.mu.Unlock()
	for _, s := range sessions {
		f.remove(s)
	}
	return nil
}

type volumeCall struct {
	target string
	value  float32
}
type fakeOBS struct {
	connected bool
	calls     chan volumeCall
}

func (f *fakeOBS) IsConnected() bool { return f.connected }
func (f *fakeOBS) SetInputVolume(target string, v float32) error {
	f.calls <- volumeCall{target, v}
	return nil
}

type fakeDiscord struct{ calls chan volumeCall }

func (f *fakeDiscord) SetVolume(target string, v float32) { f.calls <- volumeCall{target, v} }

func makeMixer(t *testing.T, settings SettingsProvider, finder *fakeFinder, obs OBSController, discord DiscordController, foreground func(bool) ([]string, error)) (*Mixer, *fakeSliders) {
	t.Helper()
	sliders := &fakeSliders{events: make(chan serial.SliderMoveEvent)}
	m, err := New(settings, sliders, zap.NewNop().Sugar(), finder, obs, discord, foreground)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Release(); err != nil {
			t.Error(err)
		}
	})
	return m, sliders
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("expected mixer output")
		var zero T
		return zero
	}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("mixer state did not settle")
		case <-tick.C:
		}
	}
}

func TestSessionMappedIgnoresMixedCaseSpecialTarget(t *testing.T) {
	settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{"DEEJ.DISCORD:Nikita"}}})
	m, _ := makeMixer(t, settings, newFinder(), nil, nil, nil)
	if m.sessionMapped(newSession("other.exe")) {
		t.Fatal("Discord target should not map an audio session")
	}
}

func TestSpecialTargetsRouteToIntegrationControllers(t *testing.T) {
	settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{
		"DEEJ.OBS:Desktop Audio", "DEEJ.DISCORD:Nikita", "deej.discord.input", "deej.discord.output",
	}}})
	obs := &fakeOBS{connected: true, calls: make(chan volumeCall, 4)}
	discord := &fakeDiscord{calls: make(chan volumeCall, 4)}
	_, sliders := makeMixer(t, settings, newFinder(), obs, discord, nil)
	sliders.send(t, 0, .42)
	if got := receive(t, obs.calls); got != (volumeCall{"Desktop Audio", .42}) {
		t.Fatalf("OBS call = %+v", got)
	}
	for _, target := range []string{"user:nikita", "input", "output"} {
		if got := receive(t, discord.calls); got != (volumeCall{target, .42}) {
			t.Fatalf("Discord call = %+v, want %s", got, target)
		}
	}
}

func TestForegroundTargetsUseRequestedWindowAndDeduplicateProcesses(t *testing.T) {
	for _, fullscreen := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "fullscreen"}[fullscreen], func(t *testing.T) {
			target := "deej.current"
			if fullscreen {
				target += ".fullscreen"
			}
			settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{target}}})
			browser, child := newSession("chrome.exe"), newSession("child.exe")
			foreground := func(wantFullscreen bool) ([]string, error) {
				if wantFullscreen != fullscreen {
					t.Errorf("fullscreen = %v, want %v", wantFullscreen, fullscreen)
				}
				return []string{"Chrome.EXE", "CHROME.exe", "child.exe"}, nil
			}
			m, sliders := makeMixer(t, settings, newFinder(browser, child), nil, nil, foreground)
			if got := m.resolveTarget(target); !slices.Equal(got, []string{"chrome.exe", "child.exe"}) {
				t.Fatalf("foreground targets = %v", got)
			}
			sliders.send(t, 0, .35)
			if got := receive(t, browser.updates); got != .35 {
				t.Fatalf("browser volume = %v", got)
			}
			if got := receive(t, child.updates); got != .35 {
				t.Fatalf("child volume = %v", got)
			}
		})
	}
}

func TestForegroundLookupFailureIsIgnored(t *testing.T) {
	settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{"deej.current"}}})
	m, _ := makeMixer(t, settings, newFinder(), nil, nil, func(bool) ([]string, error) { return nil, errors.New("no foreground window") })
	if got := m.resolveTarget("deej.current"); got != nil {
		t.Fatalf("failed lookup resolved %v", got)
	}
}

func TestUnmappedExcludesNamedTargetsAndDeviceSessions(t *testing.T) {
	settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{"deej.unmapped"}}, {Slider: 1, Targets: []string{"mapped.exe"}}})
	other, mapped := newSession("other.exe"), newSession("mapped.exe")
	master, system, mic, device := newSession("master"), newSession("system"), newSession("mic"), newSession("speakers")
	device.device = true
	m, sliders := makeMixer(t, settings, newFinder(other, mapped, master, system, mic, device), nil, nil, nil)
	if got := m.resolveTarget("deej.unmapped"); !reflect.DeepEqual(got, []string{"other.exe"}) {
		t.Fatalf("unmapped = %v", got)
	}
	sliders.send(t, 0, .61)
	if got := receive(t, other.updates); got != .61 {
		t.Fatalf("unmapped volume = %v", got)
	}
	for _, s := range []*fakeSession{mapped, master, system, mic, device} {
		if got := s.GetVolume(); got != 0 {
			t.Errorf("excluded session %s volume = %v", s.Key(), got)
		}
	}
}

func TestProfileReloadUsesFreshMappingForStationaryEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("active_profile: first\nprofiles:\n  - name: first\n    slider_mapping:\n      0: first.exe\n      1: deej.unmapped\n  - name: second\n    slider_mapping:\n      0: second.exe\n      1: deej.unmapped\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.New(zap.NewNop().Sugar(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Load(); err != nil {
		t.Fatal(err)
	}
	first, second := newSession("first.exe"), newSession("second.exe")
	m, sliders := makeMixer(t, store, newFinder(first, second), nil, nil, nil)
	sliders.send(t, 0, .73)
	receive(t, first.updates)
	if err = store.SetActiveProfile("second"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return slices.Equal(m.resolveTarget("deej.unmapped"), []string{"first.exe"}) })
	// serial's reload test verifies reset/re-emission of this stationary value;
	// this boundary verifies routing and cache refresh against the real store.
	sliders.send(t, 0, .73)
	if got := receive(t, second.updates); got != .73 {
		t.Fatalf("new profile volume = %v", got)
	}
	sliders.send(t, 1, .2)
	if got := receive(t, first.updates); got != .2 {
		t.Fatalf("newly unmapped volume = %v", got)
	}
}

func TestSessionRemovalWaitsForActiveVolumeChange(t *testing.T) {
	settings := settingsWithMapping(config.SliderMappings{{Slider: 0, Targets: []string{"busy.exe"}}})
	session := newSession("busy.exe")
	session.setting, session.continueSet = make(chan struct{}), make(chan struct{})
	finder := newFinder(session)
	m, sliders := makeMixer(t, settings, finder, nil, nil, nil)
	sliders.send(t, 0, .8)
	receive(t, session.setting)
	removing, removed := make(chan struct{}), make(chan struct{})
	go func() { close(removing); finder.remove(session); close(removed) }()
	receive(t, removing)
	select {
	case <-removed:
		t.Fatal("session was removed while SetVolume was active")
	case <-time.After(20 * time.Millisecond):
	}
	if session.released.Load() {
		t.Fatal("session was released while SetVolume was active")
	}
	close(session.continueSet)
	receive(t, removed)
	if !session.released.Load() {
		t.Fatal("finder did not release removed session")
	}
	if m.SessionCount() != 0 {
		t.Fatal("removed session remains reachable")
	}
	sliders.send(t, 0, .2)
	if got := session.GetVolume(); got != .8 {
		t.Fatalf("removed session volume changed to %v", got)
	}
}

func TestInitializeSubscribesBeforeDiscoveryAndReleaseStopsConsumers(t *testing.T) {
	settings := settingsWithMapping(nil)
	sliders := &fakeSliders{events: make(chan serial.SliderMoveEvent)}
	session := newSession("startup.exe")
	finder := newFinder(session)
	finder.onStart = func() {
		if sliders.subscriptions != 1 {
			t.Error("slider subscription missing before discovery")
		}
		settings.mu.Lock()
		count := len(settings.changes)
		settings.mu.Unlock()
		if count != 1 {
			t.Error("config subscription missing before discovery")
		}
	}
	m, err := New(settings, sliders, zap.NewNop().Sugar(), finder, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Initialize(); err != nil {
		t.Fatal(err)
	}
	if m.SessionCount() != 1 {
		t.Fatal("startup enumeration event was lost")
	}
	if err = m.Release(); err != nil {
		t.Fatal(err)
	}
	if !session.released.Load() || m.SessionCount() != 0 {
		t.Fatal("Release did not synchronously release sessions")
	}
	select {
	case sliders.events <- serial.SliderMoveEvent{}:
		t.Fatal("slider consumer still receiving after Release")
	default:
	}
}
