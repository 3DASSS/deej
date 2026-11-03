package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/integrations/discord"
	"github.com/nik9play/deej/internal/integrations/obs"
	"github.com/nik9play/deej/internal/mixer"
	"github.com/nik9play/deej/internal/serial"
	"go.uber.org/zap"
)

func TestDiscordVolumeSourceUsesCurrentProfileAndSliderReadings(t *testing.T) {
	settings := &config.Settings{
		ActiveProfile: "active",
		Profiles: []config.Profile{
			{Name: "inactive", SliderMapping: config.SliderMappings{{Slider: 0, Targets: []string{"deej.discord:wrong-profile"}}}},
			{Name: "active", SliderMapping: config.SliderMappings{
				{Slider: 0, Targets: []string{"master", "DEEJ.DISCORD.INPUT", "deej.discord: Nikita "}},
				{Slider: 1, Targets: []string{"Deej.Discord.Output", "discord.exe", "deej.obs:Music"}},
				{Slider: -1, Targets: []string{"deej.discord:negative"}},
				{Slider: 99, Targets: []string{"deej.discord:missing-slider"}},
			}},
		},
	}
	values := []float32{0.25, 0.75}
	currentVolumes := discordVolumeSource(func() *config.Settings { return settings }, func() []float32 { return values })
	want := []discord.TargetVolume{
		{Target: discord.InputTarget, Percent: values[0]},
		{Target: discord.UserTargetPrefix + "nikita", Percent: values[0]},
		{Target: discord.OutputTarget, Percent: values[1]},
	}
	if got := currentVolumes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped volumes = %#v, want %#v", got, want)
	}
	values = []float32{0.9, 0.75}
	want[0].Percent, want[1].Percent = 0.9, 0.9
	if got := currentVolumes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("restoration captured an old slider snapshot: %#v", got)
	}
	updated := *settings
	updated.ActiveProfile = "inactive"
	settings = &updated
	if got := currentVolumes(); !reflect.DeepEqual(got, []discord.TargetVolume{{Target: "user:wrong-profile", Percent: 0.9}}) {
		t.Fatalf("restoration captured an old profile: %#v", got)
	}
	values = nil
	if got := currentVolumes(); len(got) != 0 {
		t.Fatalf("sliders without a reading must not be restored: %#v", got)
	}
}

type lifecycleFinder struct {
	startErr   error
	releaseErr error
	releases   atomic.Int32
	onRelease  func()
}

func (f *lifecycleFinder) Start(audio.SessionEventHandler) error { return f.startErr }
func (f *lifecycleFinder) Release() error {
	f.releases.Add(1)
	if f.onRelease != nil {
		f.onRelease()
	}
	return f.releaseErr
}

// Compose the real core services against a missing port and fake audio finder.
// This exercises their subscription shutdown without device or audio access.
func testCore(t *testing.T, finder *lifecycleFinder) *App {
	t.Helper()
	logger := zap.NewNop().Sugar()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("com:\n  port: deej-test-nonexistent-port\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.New(logger, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	sliders, err := serial.New(store, logger, serial.Options{})
	if err != nil {
		t.Fatal(err)
	}
	core := &App{
		logger: logger, config: store, serial: sliders, stopRequest: make(chan struct{}),
		obs: obs.New(store, logger), discord: discord.New(store, logger, filepath.Join(filepath.Dir(path), discord.TokenFileName), nil),
	}
	core.sessions, err = mixer.New(store, sliders, logger, finder, core.obs, core.discord, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	return core
}

func TestConcurrentStopWaitsForResourcesAndReturnsSameError(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	wantErr := errors.New("finder release failed")
	finder := &lifecycleFinder{releaseErr: wantErr, onRelease: func() { close(entered); <-release }}
	core := testCore(t, finder)
	var releaseOnce sync.Once
	finishRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(finishRelease)
	if err := core.Initialize(); err != nil {
		t.Fatal(err)
	}
	core.Start()
	results := make(chan error, 9)
	go func() { results <- core.Stop() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not reach audio release")
	}
	var requests sync.WaitGroup
	for range 8 {
		requests.Go(func() { core.RequestStop(); results <- core.Stop() })
	}
	select {
	case err := <-results:
		t.Fatalf("Stop returned before audio release completed: %v", err)
	default:
	}
	finishRelease()
	for range 9 {
		select {
		case err := <-results:
			if !errors.Is(err, wantErr) {
				t.Errorf("Stop error = %v, want %v", err, wantErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a concurrent Stop remained blocked")
		}
	}
	requests.Wait()
	if got := finder.releases.Load(); got != 1 {
		t.Fatalf("audio resources released %d times", got)
	}
}

func TestStopAfterFailedInitializationReleasesConsumersAndPreventsStart(t *testing.T) {
	wantErr := errors.New("audio discovery failed")
	finder := &lifecycleFinder{startErr: wantErr}
	core := testCore(t, finder)
	if err := core.Initialize(); !errors.Is(err, wantErr) {
		t.Fatalf("Initialize error = %v, want %v", err, wantErr)
	}
	finished := make(chan error, 1)
	go func() { finished <- core.Stop() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed startup left a consumer blocked")
	}
	core.Wait()
	if got := finder.releases.Load(); got != 1 {
		t.Fatalf("failed startup released finder %d times", got)
	}
	core.Start() // No earlier Start can mask the stopped-state guard.
	if core.started {
		t.Fatal("Start accepted after failed initialization was stopped")
	}
}
