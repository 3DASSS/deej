package deej

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type fakeNotifier struct{}

func (fakeNotifier) Notify(string, string) {}

func newTestLocalizer() *i18n.Localizer {
	bundle := i18n.NewBundle(language.English)
	return i18n.NewLocalizer(bundle, "en")
}

func newTestConfig(t *testing.T, contents string) *CanonicalConfig {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	cc, err := NewConfig(zap.NewNop().Sugar(), fakeNotifier{}, configPath)
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	if err := cc.Load(newTestLocalizer()); err != nil {
		t.Fatalf("load config: %v", err)
	}

	return cc
}

const testConfigContents = `slider_mapping:
  0: master
  1: [chrome.exe, firefox.exe]

invert_sliders: false

com:
  port: COM4
  baud_rate: 9600

obs:
  enabled: false
  host: localhost
  port: 4455
  password: ""
`

// TestSaveUserSettingsRoundTrip touches every field of Settings, so a save
// that drops or mangles one shows up here
func TestSaveUserSettingsRoundTrip(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	settings := cc.UserSettings()
	settings.COM.Port = "COM7"
	settings.COM.BaudRate = 115200
	settings.COM.VID = "2341"
	settings.COM.PID = "0043"
	settings.InvertSliders = true
	settings.NoiseReduction = "high"
	settings.Language = "ru"
	settings.OBS.Enabled = true
	settings.OBS.Host = "192.168.1.5"
	settings.OBS.Port = 4456
	settings.OBS.Password = "secret"
	settings.SliderMapping = SliderMappings{
		{Slider: 0, Targets: []string{"master"}},
		{Slider: 2, Targets: []string{"discord.exe", "spotify.exe"}},
	}

	if err := cc.SaveUserSettings(settings, newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// the file must carry the config's own key names, not just the values
	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)

	for _, want := range []string{
		"port: COM7",
		"invert_sliders: true",
		"noise_reduction: high",
		"discord.exe",
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved config missing %q:\n%s", want, saved)
		}
	}

	// every value must survive the write -> read cycle, applied immediately
	got := cc.UserSettings()
	if got.COM.Port != "COM7" || got.COM.BaudRate != 115200 ||
		got.COM.VID != "2341" || got.COM.PID != "0043" ||
		!got.InvertSliders || got.NoiseReduction != "high" || got.Language != "ru" ||
		!got.OBS.Enabled || got.OBS.Host != "192.168.1.5" ||
		got.OBS.Port != 4456 || got.OBS.Password != "secret" {
		t.Errorf("round trip mismatch: %+v", got)
	}

	if len(got.SliderMapping) != 2 {
		t.Fatalf("expected 2 mapping entries, got %v", got.SliderMapping)
	}
	targets, ok := got.SliderMapping.get(2)
	if !ok || len(targets) != 2 {
		t.Errorf("multi-target mapping not applied, got %v", targets)
	}
}

func TestSaveUserSettingsNotifiesConsumers(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	// the channel is buffered and the save notifies synchronously, so the
	// signal is already waiting by the time SaveUserSettings returns
	reloaded := cc.SubscribeToChanges()

	if err := cc.SaveUserSettings(cc.UserSettings(), newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	select {
	case <-reloaded:
	default:
		t.Error("expected reload notification")
	}
}

func TestLoadParsesHexWordFormats(t *testing.T) {
	// decimal 6790 == 0x1A86; quoted strings and 0x-prefixed ints must all work
	cc := newTestConfig(t, "com:\n  vid: 6790\n  pid: \"0x7523\"\n")

	values := cc.Values()
	if values.COM.VID != "1A86" {
		t.Errorf("COM.VID = %q, expected 1A86", values.COM.VID)
	}
	if values.COM.PID != "7523" {
		t.Errorf("COM.PID = %q, expected 7523", values.COM.PID)
	}

	vid, ok := values.COM.VID.Value()
	if !ok || vid != 0x1A86 {
		t.Errorf("COM.VID.Value() = %v, %v; expected 0x1A86", vid, ok)
	}
}

func TestLoadReadsLegacyComKeys(t *testing.T) {
	cc := newTestConfig(t, `
com_port: COM9
baud_rate: 115200
com_vid: "2341"
com_pid: "0043"
`)

	values := cc.Values()
	if values.COM.Port != "COM9" {
		t.Errorf("COM.Port = %q, expected COM9", values.COM.Port)
	}
	if values.COM.BaudRate != 115200 {
		t.Errorf("COM.BaudRate = %d, expected 115200", values.COM.BaudRate)
	}
	if values.COM.VID != "2341" || values.COM.PID != "0043" {
		t.Errorf("COM VID/PID = %q/%q, expected 2341/0043", values.COM.VID, values.COM.PID)
	}
}

func TestComSectionWinsOverLegacyKeys(t *testing.T) {
	cc := newTestConfig(t, `
com_port: COM3
baud_rate: 19200
com:
  port: COM9
`)

	values := cc.Values()

	// the section's key wins over its legacy counterpart...
	if values.COM.Port != "COM9" {
		t.Errorf("COM.Port = %q, expected COM9", values.COM.Port)
	}

	// ...but legacy keys the section doesn't override still apply
	if values.COM.BaudRate != 19200 {
		t.Errorf("COM.BaudRate = %d, expected 19200", values.COM.BaudRate)
	}
}

func TestSaveMigratesLegacyKeysToComSection(t *testing.T) {
	cc := newTestConfig(t, "com_port: COM9\nbaud_rate: 115200\n")

	if err := cc.SaveUserSettings(cc.UserSettings(), newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)

	// legacy keys live at the top level, so they'd start a line unindented
	// (the com section's own baud_rate is indented)
	for line := range strings.SplitSeq(saved, "\n") {
		for _, stale := range []string{"com_port:", "baud_rate:", "com_vid:", "com_pid:"} {
			if strings.HasPrefix(line, stale) {
				t.Errorf("saved config still contains legacy key %q:\n%s", stale, saved)
			}
		}
	}
	if !strings.Contains(saved, "com:") || !strings.Contains(saved, "  port: COM9") ||
		!strings.Contains(saved, "  baud_rate: 115200") {
		t.Errorf("saved config missing com section values:\n%s", saved)
	}
}

func TestLoadFallsBackOnInvalidValues(t *testing.T) {
	cc := newTestConfig(t, `
com:
  port: ""
  baud_rate: -5
  vid: zzzz
language: de
noise_reduction: extreme
obs:
  port: 99999
`)

	values := cc.Values()
	defaults := defaultSettings()

	if values.COM.Port != defaults.COM.Port {
		t.Errorf("COM.Port = %q, expected default %q", values.COM.Port, defaults.COM.Port)
	}
	if values.COM.BaudRate != defaults.COM.BaudRate {
		t.Errorf("COM.BaudRate = %d, expected default %d", values.COM.BaudRate, defaults.COM.BaudRate)
	}
	if values.Language != defaults.Language {
		t.Errorf("Language = %q, expected default %q", values.Language, defaults.Language)
	}
	if values.NoiseReduction != defaults.NoiseReduction {
		t.Errorf("NoiseReduction = %q, expected default %q", values.NoiseReduction, defaults.NoiseReduction)
	}
	if values.COM.VID != defaults.COM.VID {
		t.Errorf("COM.VID = %q, expected default %q", values.COM.VID, defaults.COM.VID)
	}
	if values.OBS.Port != defaults.OBS.Port {
		t.Errorf("OBS.Port = %d, expected default %d", values.OBS.Port, defaults.OBS.Port)
	}
}

func TestLoadToleratesWrongValueTypes(t *testing.T) {
	// baud_rate has a wrong type and falls back to its default; the rest of
	// the file must still apply, through the com section and the legacy keys
	// alike (the two take separate decode paths)
	for _, tc := range []struct {
		name     string
		contents string
	}{
		{"com section", "com:\n  baud_rate: [what]\n  port: COM9\n"},
		{"legacy keys", "baud_rate: [what]\ncom_port: COM9\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := newTestConfig(t, tc.contents).Values()

			if values.COM.BaudRate != defaultSettings().COM.BaudRate {
				t.Errorf("COM.BaudRate = %d, expected default", values.COM.BaudRate)
			}
			if values.COM.Port != "COM9" {
				t.Errorf("COM.Port = %q, expected COM9", values.COM.Port)
			}
		})
	}
}

func TestDefaultComPortIsAuto(t *testing.T) {
	// autodetection by VID/PID is the shipped default; a hardcoded port name
	// would only ever be right on the machine it was picked on
	if port := defaultSettings().COM.Port; port != "auto" {
		t.Errorf("default com port = %q, expected auto", port)
	}
}

func TestSettingsNormalize(t *testing.T) {
	valid := Settings{
		COM: COMSettings{
			Port:     "auto",
			BaudRate: 9600,
			VID:      "1A86",
			PID:      "7523",
		},
		NoiseReduction: "default",
		Language:       "auto",
		OBS:            OBSSettings{Host: "localhost", Port: 4455},
		SliderMapping: SliderMappings{
			{Slider: 0, Targets: []string{"master"}},
		},
	}

	v := valid
	if problems := v.normalize(); len(problems) != 0 {
		t.Errorf("valid settings reported problems: %v", problems)
	}

	// empty VID/PID mean "use the built-in default": no problem, and they
	// canonicalize to the default value
	emptyVIDPID := valid
	emptyVIDPID.COM.VID = ""
	emptyVIDPID.COM.PID = ""
	if problems := emptyVIDPID.normalize(); len(problems) != 0 {
		t.Errorf("empty VID/PID reported problems: %v", problems)
	}
	if emptyVIDPID.COM.VID != defaultSettings().COM.VID || emptyVIDPID.COM.PID != defaultSettings().COM.PID {
		t.Errorf("empty VID/PID not defaulted: %q/%q", emptyVIDPID.COM.VID, emptyVIDPID.COM.PID)
	}

	// an entry whose targets are all empty is dropped, not reported: the
	// snapshot has to match what a save writes to the file
	emptyTargets := valid
	emptyTargets.SliderMapping = SliderMappings{
		{Slider: 0, Targets: []string{"master"}},
		{Slider: 1, Targets: []string{""}},
		{Slider: 2, Targets: nil},
	}
	if problems := emptyTargets.normalize(); len(problems) != 0 {
		t.Errorf("empty targets reported problems: %v", problems)
	}
	if len(emptyTargets.SliderMapping) != 1 || emptyTargets.SliderMapping[0].Slider != 0 {
		t.Errorf("target-less entries not dropped: %+v", emptyTargets.SliderMapping)
	}

	cases := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"empty com port", func(s *Settings) { s.COM.Port = "" }},
		{"zero baud rate", func(s *Settings) { s.COM.BaudRate = 0 }},
		{"bad vid", func(s *Settings) { s.COM.VID = "xyz" }},
		{"vid too large", func(s *Settings) { s.COM.VID = "12345" }},
		{"bad noise level", func(s *Settings) { s.NoiseReduction = "extreme" }},
		{"bad language", func(s *Settings) { s.Language = "de" }},
		{"obs port too large", func(s *Settings) { s.OBS.Port = 70000 }},
		{"negative slider", func(s *Settings) {
			s.SliderMapping = SliderMappings{{Slider: -1, Targets: []string{"a"}}}
		}},
		{"duplicate slider", func(s *Settings) {
			s.SliderMapping = SliderMappings{
				{Slider: 1, Targets: []string{"a"}},
				{Slider: 1, Targets: []string{"b"}},
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := valid
			tc.mutate(&settings)
			if problems := settings.normalize(); len(problems) == 0 {
				t.Error("expected normalize to report a problem")
			}
		})
	}
}

func TestSaveUserSettingsRejectsInvalid(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	settings := cc.UserSettings()
	settings.COM.BaudRate = -1

	if err := cc.SaveUserSettings(settings, newTestLocalizer()); err == nil {
		t.Error("expected save to fail validation")
	}

	// file must be untouched
	data, _ := os.ReadFile(cc.configPath)
	if string(data) != testConfigContents {
		t.Error("config file was modified by a rejected save")
	}
}

// startTestWatcher runs the config file watcher and makes sure it is torn
// down before the test's temp dir is removed
func startTestWatcher(t *testing.T, cc *CanonicalConfig) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		cc.WatchConfigFileChanges(newTestLocalizer())
		close(done)
	}()
	t.Cleanup(func() {
		cc.StopWatchingConfigFile()
		<-done
	})

	// give the watcher a moment to establish its directory watch
	time.Sleep(100 * time.Millisecond)
}

func TestWatcherReloadsOnHandEdit(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)
	reloaded := cc.SubscribeToChanges()

	startTestWatcher(t, cc)

	edited := strings.Replace(testConfigContents, "port: COM4", "port: COM9", 1)
	if err := os.WriteFile(cc.configPath, []byte(edited), 0o644); err != nil {
		t.Fatalf("edit config: %v", err)
	}

	select {
	case <-reloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("no reload notification after a hand edit")
	}

	if cc.Values().COM.Port != "COM9" {
		t.Errorf("COM.Port = %q after reload, expected COM9", cc.Values().COM.Port)
	}
}

func TestWatcherIgnoresGUISave(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)
	reloaded := cc.SubscribeToChanges()

	startTestWatcher(t, cc)

	settings := cc.UserSettings()
	settings.COM.Port = "COM7"
	if err := cc.SaveUserSettings(settings, newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// the save itself notifies once, synchronously
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("no notification from the save itself")
	}

	// the watcher must recognize the file event as our own write and stay
	// quiet; wait well past the debounce window
	select {
	case <-reloaded:
		t.Fatal("watcher re-applied a GUI save")
	case <-time.After(watchDebounceDelay*4 + 500*time.Millisecond):
	}
}
