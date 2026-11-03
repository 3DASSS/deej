package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Settings is the single source of truth for deej's user-facing
// configuration. The yaml tags define the config file keys, the json tags
// the GUI wire format: a new setting only needs a field here (plus a default
// in defaultSettings and, if needed, a rule in normalize) to reach the file,
// the runtime snapshot and the settings GUI
type Settings struct {
	ActiveProfile  string          `yaml:"active_profile" json:"activeProfile"`
	Profiles       []Profile       `yaml:"profiles" json:"profiles"`
	InvertSliders  bool            `yaml:"invert_sliders" json:"invertSliders"`
	COM            COMSettings     `yaml:"com" json:"com"`
	NoiseReduction string          `yaml:"noise_reduction,omitempty" json:"noiseReduction"`
	Language       string          `yaml:"language" json:"language"`
	OBS            OBSSettings     `yaml:"obs" json:"obs"`
	Discord        DiscordSettings `yaml:"discord" json:"discord"`
}

// Profile is one named slider layout. Only the mapping and its hotkey are
// per-profile; everything else (com, obs, language, ...) stays global.
// normalize guarantees at least one profile exists, so consumers can always
// resolve a mapping
type Profile struct {
	Name          string         `yaml:"name" json:"name"`
	Hotkey        string         `yaml:"hotkey,omitempty" json:"hotkey"`
	SliderMapping SliderMappings `yaml:"slider_mapping" json:"sliderMapping"`
}

// clone returns a deep copy, safe to hand out and mutate without touching the
// live snapshot
func (s Settings) clone() Settings {
	out := s

	out.Profiles = make([]Profile, len(s.Profiles))
	for i, profile := range s.Profiles {
		profile.SliderMapping = profile.SliderMapping.clone()
		out.Profiles[i] = profile
	}

	return out
}

// COMSettings describes the Arduino serial connection parameters
type COMSettings struct {
	Port     string  `yaml:"port" json:"port"`
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	VID      HexWord `yaml:"vid" json:"vid"`
	PID      HexWord `yaml:"pid" json:"pid"`
}

// OBSSettings describes the OBS websocket connection parameters
type OBSSettings struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Host             string `yaml:"host" json:"host"`
	Port             int    `yaml:"port" json:"port"`
	Password         string `yaml:"password" json:"password"`
	VolumeConversion bool   `yaml:"volume_conversion" json:"volumeConversion"`
}

// DiscordSettings describes the Discord RPC integration. deej can't ship
// credentials of its own: Discord gates the rpc.voice.* scopes to an
// application's owner, so the user creates their own application and pastes
// its ID and secret here. The OAuth token that comes out of linking is not
// kept here - it lives in its own file next to the config (see discord.go)
type DiscordSettings struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	ClientID         string `yaml:"client_id" json:"clientId"`
	ClientSecret     string `yaml:"client_secret" json:"clientSecret"`
	VolumeConversion bool   `yaml:"volume_conversion" json:"volumeConversion"`
}

// HexWord is a 16-bit value carried as a hex string (e.g. "1A86"), the form
// both the config file and the GUI use. An empty value means "use the
// built-in default"
type HexWord string

// Value returns the numeric form; ok is false for empty or malformed values
func (h HexWord) Value() (uint16, bool) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(string(h))), "0x")

	parsed, err := strconv.ParseUint(trimmed, 16, 16)
	if err != nil {
		return 0, false
	}

	return uint16(parsed), true
}

// canonical returns the canonical 4-digit hex form. A blank value maps to the
// default (blank means "use the built-in default") with ok=true. ok is false
// only for a non-blank value that isn't valid hex; the default is returned in
// that case too, so callers always get a usable value
func (h HexWord) canonical(def HexWord) (HexWord, bool) {
	if strings.TrimSpace(string(h)) == "" {
		return def, true
	}

	parsed, ok := h.Value()
	if !ok {
		return def, false
	}

	return HexWord(fmt.Sprintf("%04X", parsed)), true
}

// UnmarshalYAML accepts both YAML integers (0x1A86, 6790) and hex strings
// ("1A86", "0x1A86")
func (h *HexWord) UnmarshalYAML(node *yaml.Node) error {
	var number uint64
	if err := node.Decode(&number); err == nil {
		if number > 0xFFFF {
			return fmt.Errorf("must be a 16-bit value: %s", node.Value)
		}

		*h = HexWord(fmt.Sprintf("%04X", number))
		return nil
	}

	var str string
	if err := node.Decode(&str); err != nil {
		return err
	}

	*h = HexWord(str)
	return nil
}

// MarshalYAML writes the value as a 0x-prefixed YAML integer
func (h HexWord) MarshalYAML() (any, error) {
	value, ok := h.Value()
	if !ok {
		// sanitized settings never hit this; keep whatever the value is
		return string(h), nil
	}

	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("0x%04X", value)}, nil
}
