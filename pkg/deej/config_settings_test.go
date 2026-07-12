package deej

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

const testConfigContents = `# top comment about mappings
slider_mapping:
  0: master
  1: [chrome.exe, firefox.exe]

# whether sliders are inverted
invert_sliders: false

com_port: COM4
baud_rate: 9600

# some unknown key deej doesn't manage
my_custom_key: hello

obs:
  enabled: false
  host: localhost
  port: 4455
  password: ""
`

func TestSaveUserSettingsPreservesCommentsAndUnknownKeys(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	dto := cc.UserSettings()
	dto.ComPort = "COM7"
	dto.InvertSliders = true
	dto.NoiseReduction = "high"
	dto.OBSEnabled = true
	dto.SliderMapping = []SliderMappingEntry{
		{Slider: 0, Targets: []string{"master"}},
		{Slider: 2, Targets: []string{"discord.exe", "spotify.exe"}},
	}

	if err := cc.SaveUserSettings(dto, newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)

	for _, want := range []string{
		"# top comment about mappings",
		"# whether sliders are inverted",
		"# some unknown key deej doesn't manage",
		"my_custom_key: hello",
		"com_port: COM7",
		"invert_sliders: true",
		"noise_reduction: high",
		"discord.exe",
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved config missing %q:\n%s", want, saved)
		}
	}

	// the save must have applied the new values immediately
	values := cc.Values()
	if values.ConnectionInfo.COMPort != "COM7" {
		t.Errorf("com port not applied, got %q", values.ConnectionInfo.COMPort)
	}
	if !values.InvertSliders {
		t.Error("invert_sliders not applied")
	}
	if !values.OBSConfig.Enabled {
		t.Error("obs.enabled not applied")
	}
	targets, ok := values.SliderMapping.get(2)
	if !ok || len(targets) != 2 {
		t.Errorf("slider mapping not applied, got %v", targets)
	}
}

func TestSaveUserSettingsRoundTrip(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	dto := cc.UserSettings()
	dto.BaudRate = 115200
	dto.ComVID = "2341"
	dto.ComPID = "0043"
	dto.Language = "ru"
	dto.OBSHost = "192.168.1.5"
	dto.OBSPort = 4456
	dto.OBSPassword = "secret"

	if err := cc.SaveUserSettings(dto, newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	got := cc.UserSettings()
	if got.BaudRate != 115200 || got.ComVID != "2341" || got.ComPID != "0043" ||
		got.Language != "ru" || got.OBSHost != "192.168.1.5" ||
		got.OBSPort != 4456 || got.OBSPassword != "secret" {
		t.Errorf("round trip mismatch: %+v", got)
	}

	if len(got.SliderMapping) != 2 {
		t.Fatalf("expected 2 mapping entries, got %v", got.SliderMapping)
	}
	if got.SliderMapping[1].Slider != 1 || len(got.SliderMapping[1].Targets) != 2 {
		t.Errorf("multi-target mapping mismatch: %+v", got.SliderMapping[1])
	}
}

func TestSaveUserSettingsNotifiesConsumers(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	reloaded := cc.SubscribeToChanges()
	done := make(chan bool)
	go func() {
		done <- <-reloaded
	}()

	dto := cc.UserSettings()
	if err := cc.SaveUserSettings(dto, newTestLocalizer()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	if !<-done {
		t.Error("expected reload notification")
	}
}

func TestSettingsDTOValidate(t *testing.T) {
	valid := SettingsDTO{
		ComPort:        "auto",
		BaudRate:       9600,
		ComVID:         "1A86",
		ComPID:         "7523",
		NoiseReduction: "default",
		Language:       "auto",
		OBSPort:        4455,
		SliderMapping: []SliderMappingEntry{
			{Slider: 0, Targets: []string{"master"}},
		},
	}

	if err := valid.Validate(); err != nil {
		t.Errorf("valid DTO rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*SettingsDTO)
	}{
		{"empty com port", func(d *SettingsDTO) { d.ComPort = "" }},
		{"zero baud rate", func(d *SettingsDTO) { d.BaudRate = 0 }},
		{"bad vid", func(d *SettingsDTO) { d.ComVID = "xyz" }},
		{"vid too large", func(d *SettingsDTO) { d.ComVID = "12345" }},
		{"bad noise level", func(d *SettingsDTO) { d.NoiseReduction = "extreme" }},
		{"bad language", func(d *SettingsDTO) { d.Language = "de" }},
		{"obs port too large", func(d *SettingsDTO) { d.OBSPort = 70000 }},
		{"negative slider", func(d *SettingsDTO) {
			d.SliderMapping = []SliderMappingEntry{{Slider: -1, Targets: []string{"a"}}}
		}},
		{"duplicate slider", func(d *SettingsDTO) {
			d.SliderMapping = []SliderMappingEntry{
				{Slider: 1, Targets: []string{"a"}},
				{Slider: 1, Targets: []string{"b"}},
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dto := valid
			tc.mutate(&dto)
			if err := dto.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestSaveUserSettingsRejectsInvalid(t *testing.T) {
	cc := newTestConfig(t, testConfigContents)

	dto := cc.UserSettings()
	dto.BaudRate = -1

	if err := cc.SaveUserSettings(dto, newTestLocalizer()); err == nil {
		t.Error("expected save to fail validation")
	}

	// file must be untouched
	data, _ := os.ReadFile(cc.configPath)
	if string(data) != testConfigContents {
		t.Error("config file was modified by a rejected save")
	}
}
