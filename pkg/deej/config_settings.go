package deej

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"go.uber.org/zap"
	"go.yaml.in/yaml/v3"
)

// Settings is the single source of truth for deej's user-facing
// configuration. The yaml tags define the config file keys, the json tags
// the GUI wire format: a new setting only needs a field here (plus a default
// in defaultSettings and, if needed, a rule in Validate/sanitize) to reach
// the file, the runtime snapshot and the settings GUI
type Settings struct {
	Mapping        SliderMappings `yaml:"slider_mapping" json:"sliderMapping"`
	InvertSliders  bool           `yaml:"invert_sliders" json:"invertSliders"`
	COM            COMSettings    `yaml:"com" json:"com"`
	NoiseReduction string         `yaml:"noise_reduction,omitempty" json:"noiseReduction"`
	Language       string         `yaml:"language" json:"language"`
	OBS            OBSSettings    `yaml:"obs" json:"obs"`
}

// COMSettings describes the Arduino serial connection parameters
type COMSettings struct {
	Port     string  `yaml:"port" json:"port"`
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	VID      HexWord `yaml:"vid" json:"vid"`
	PID      HexWord `yaml:"pid" json:"pid"`
}

// UnmarshalYAML reads the com section, plus the legacy flat keys (com_port,
// baud_rate, com_vid, com_pid) that older config files use. Keys in the com
// section win over their legacy counterparts; saves only ever write the
// section
func (s *Settings) UnmarshalYAML(node *yaml.Node) error {
	var legacy struct {
		Port     *string  `yaml:"com_port"`
		BaudRate *int     `yaml:"baud_rate"`
		VID      *HexWord `yaml:"com_vid"`
		PID      *HexWord `yaml:"com_pid"`
	}
	legacyErr := node.Decode(&legacy)
	if legacyErr != nil && !isYAMLTypeError(legacyErr) {
		return legacyErr
	}

	if legacy.Port != nil {
		s.COM.Port = *legacy.Port
	}
	if legacy.BaudRate != nil {
		s.COM.BaudRate = *legacy.BaudRate
	}
	if legacy.VID != nil {
		s.COM.VID = *legacy.VID
	}
	if legacy.PID != nil {
		s.COM.PID = *legacy.PID
	}

	// the alias type drops Settings' methods, so this can't recurse; keys the
	// document does specify overwrite the legacy values applied above
	type settingsAlias Settings
	aliasErr := node.Decode((*settingsAlias)(s))
	if aliasErr != nil && !isYAMLTypeError(aliasErr) {
		return aliasErr
	}

	// surviving errors are type errors only; the caller treats those as
	// non-fatal (the affected fields keep their previous values)
	return errors.Join(legacyErr, aliasErr)
}

func isYAMLTypeError(err error) bool {
	var typeErr *yaml.TypeError
	return errors.As(err, &typeErr)
}

// OBSSettings describes the OBS websocket connection parameters
type OBSSettings struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	Password string `yaml:"password" json:"password"`
}

func defaultSettings() Settings {
	return Settings{
		Mapping:  SliderMappings{},
		Language: "auto",

		COM: COMSettings{
			Port:     "COM4",
			BaudRate: 9600,

			// ch340 chip
			VID: "1A86",
			PID: "7523",
		},

		OBS: OBSSettings{
			Host: "localhost",
			Port: 4455,
		},
	}
}

var validNoiseReductionLevels = []string{"", "low", "default", "high", "none"}
var validLanguages = []string{"auto", "en", "ru"}

// Validate checks the settings for values that would produce a broken
// config file; used for GUI saves, which must be rejected rather than fixed
func (s *Settings) Validate() error {
	if s.COM.Port == "" {
		return fmt.Errorf("com port must not be empty")
	}

	if s.COM.BaudRate <= 0 {
		return fmt.Errorf("baud rate must be a positive number")
	}

	// an empty VID/PID means "use the built-in default", so only validate
	// non-empty values
	if _, err := s.COM.VID.parse(); s.COM.VID != "" && err != nil {
		return fmt.Errorf("com vid: %w", err)
	}

	if _, err := s.COM.PID.parse(); s.COM.PID != "" && err != nil {
		return fmt.Errorf("com pid: %w", err)
	}

	if !slices.Contains(validNoiseReductionLevels, s.NoiseReduction) {
		return fmt.Errorf("invalid noise reduction level: %s", s.NoiseReduction)
	}

	if !slices.Contains(validLanguages, s.Language) {
		return fmt.Errorf("invalid language: %s", s.Language)
	}

	if s.OBS.Port < 1 || s.OBS.Port > 65535 {
		return fmt.Errorf("obs port must be between 1 and 65535")
	}

	seenSliders := map[int]bool{}
	for _, entry := range s.Mapping {
		if entry.Slider < 0 {
			return fmt.Errorf("slider index must not be negative: %d", entry.Slider)
		}
		if seenSliders[entry.Slider] {
			return fmt.Errorf("duplicate slider index: %d", entry.Slider)
		}
		seenSliders[entry.Slider] = true
	}

	return nil
}

// sanitize brings settings to a canonical, valid state, falling back to the
// default (with a warning) for any value a hand-edited file got wrong. GUI
// saves pass through it too, after Validate, for canonicalization only
func (s *Settings) sanitize(logger *zap.SugaredLogger) {
	defaults := defaultSettings()

	if s.COM.Port == "" {
		logger.Warnw("Empty com port, using default value", "defaultValue", defaults.COM.Port)
		s.COM.Port = defaults.COM.Port
	}

	if s.COM.BaudRate <= 0 {
		logger.Warnw("Invalid baud rate specified, using default value",
			"invalidValue", s.COM.BaudRate,
			"defaultValue", defaults.COM.BaudRate)
		s.COM.BaudRate = defaults.COM.BaudRate
	}

	s.COM.VID = s.COM.VID.canonicalOrDefault(logger, "com.vid", defaults.COM.VID)
	s.COM.PID = s.COM.PID.canonicalOrDefault(logger, "com.pid", defaults.COM.PID)

	if !slices.Contains(validNoiseReductionLevels, s.NoiseReduction) {
		logger.Warnw("Invalid noise reduction level, using default", "invalidValue", s.NoiseReduction)
		s.NoiseReduction = defaults.NoiseReduction
	}

	if !slices.Contains(validLanguages, s.Language) {
		logger.Warnw("Invalid language, using default", "invalidValue", s.Language, "defaultValue", defaults.Language)
		s.Language = defaults.Language
	}

	if s.OBS.Port < 1 || s.OBS.Port > 65535 {
		logger.Warnw("Invalid obs port, using default value",
			"invalidValue", s.OBS.Port,
			"defaultValue", defaults.OBS.Port)
		s.OBS.Port = defaults.OBS.Port
	}

	if s.OBS.Host == "" {
		s.OBS.Host = defaults.OBS.Host
	}

	// canonicalize the mapping: drop negative or duplicate sliders and empty
	// targets, and keep it sorted by slider index
	mapping := SliderMappings{}
	seenSliders := map[int]bool{}
	for _, entry := range s.Mapping {
		if entry.Slider < 0 || seenSliders[entry.Slider] {
			logger.Warnw("Ignoring invalid slider mapping entry", "slider", entry.Slider)
			continue
		}
		seenSliders[entry.Slider] = true

		targets := slices.DeleteFunc(slices.Clone(entry.Targets), func(t string) bool { return t == "" })
		mapping = append(mapping, SliderMappingEntry{Slider: entry.Slider, Targets: targets})
	}
	sort.Slice(mapping, func(i, j int) bool { return mapping[i].Slider < mapping[j].Slider })
	s.Mapping = mapping
}

// HexWord is a 16-bit value carried as a hex string (e.g. "1A86"), the form
// both the config file and the GUI use. An empty value means "use the
// built-in default"
type HexWord string

// Value returns the numeric form; ok is false for empty or malformed values
func (h HexWord) Value() (uint16, bool) {
	parsed, err := h.parse()
	if err != nil {
		return 0, false
	}

	return parsed, true
}

func (h HexWord) parse() (uint16, error) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(string(h))), "0x")
	parsed, err := strconv.ParseUint(trimmed, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("must be a 16-bit hex value: %s", string(h))
	}

	return uint16(parsed), nil
}

func (h HexWord) canonicalOrDefault(logger *zap.SugaredLogger, key string, def HexWord) HexWord {
	if strings.TrimSpace(string(h)) == "" {
		return def
	}

	parsed, err := h.parse()
	if err != nil {
		logger.Warnw("Invalid hex value, using default", "key", key, "invalidValue", string(h), "defaultValue", string(def))
		return def
	}

	return HexWord(fmt.Sprintf("%04X", parsed))
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

// SliderMappingEntry is one slider's targets, JSON-friendly for the GUI
type SliderMappingEntry struct {
	Slider  int      `json:"slider"`
	Targets []string `json:"targets"`
}

// SliderMappings is the slider_mapping config key. In the file it's a YAML
// mapping of slider index to a single target or a list of targets
type SliderMappings []SliderMappingEntry

// UnmarshalYAML is deliberately lenient: hand-edited entries it can't make
// sense of are skipped rather than failing the whole config load. Strictness
// for GUI saves comes from Settings.Validate instead
func (sm *SliderMappings) UnmarshalYAML(node *yaml.Node) error {
	entries := SliderMappings{}

	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode, valueNode := node.Content[i], node.Content[i+1]

			slider, err := strconv.Atoi(keyNode.Value)
			if err != nil {
				continue
			}

			targets := []string{}
			switch valueNode.Kind {
			case yaml.ScalarNode:
				if valueNode.Tag != "!!null" && valueNode.Value != "" {
					targets = append(targets, valueNode.Value)
				}
			case yaml.SequenceNode:
				for _, item := range valueNode.Content {
					if item.Kind == yaml.ScalarNode && item.Tag != "!!null" {
						targets = append(targets, item.Value)
					}
				}
			}

			entries = append(entries, SliderMappingEntry{Slider: slider, Targets: targets})
		}
	}

	*sm = entries
	return nil
}

// MarshalYAML builds the slider_mapping value: single targets are written
// as plain scalars, multiple targets as sequences, matching the style of
// the example config
func (sm SliderMappings) MarshalYAML() (any, error) {
	sorted := sm.clone()
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slider < sorted[j].Slider })

	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	for _, entry := range sorted {
		targets := slices.DeleteFunc(entry.Targets, func(t string) bool { return t == "" })
		if len(targets) == 0 {
			continue
		}

		var value *yaml.Node
		if len(targets) == 1 {
			value = yamlStringNode(targets[0])
		} else {
			value = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, target := range targets {
				value.Content = append(value.Content, yamlStringNode(target))
			}
		}

		mapping.Content = append(mapping.Content, yamlIntNode(entry.Slider), value)
	}

	return mapping, nil
}

func (sm SliderMappings) clone() SliderMappings {
	out := make(SliderMappings, len(sm))
	for i, entry := range sm {
		out[i] = SliderMappingEntry{Slider: entry.Slider, Targets: slices.Clone(entry.Targets)}
	}

	return out
}

// UserSettings returns the current contents of the user config file
func (cc *CanonicalConfig) UserSettings() Settings {
	settings := cc.Values().Settings
	settings.Mapping = settings.Mapping.clone()

	return settings
}

// SaveUserSettings validates the settings, rewrites the user config file on
// disk and applies the new config immediately. The file is fully regenerated:
// comments, key order and unknown keys are not preserved
func (cc *CanonicalConfig) SaveUserSettings(settings Settings, localizer *i18n.Localizer) error {
	if err := settings.Validate(); err != nil {
		return err
	}

	// canonicalize (blank VID/PID -> defaults, mapping sorted and filtered)
	settings.sanitize(cc.logger)

	if err := cc.saveAndReload(settings, localizer); err != nil {
		return err
	}

	cc.onConfigReloaded()

	return nil
}

func (cc *CanonicalConfig) saveAndReload(settings Settings, localizer *i18n.Localizer) error {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&settings); err != nil {
		return fmt.Errorf("marshal config for save: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("marshal config for save: %w", err)
	}
	out := buf.Bytes()

	// remember the exact content we wrote, so the file watcher can tell this
	// write apart from a hand edit and skip the redundant reload
	cc.lastSelfWrite.Store(contentHash(out))

	if err := writeFileAtomic(cc.configPath, out); err != nil {
		cc.logger.Warnw("Failed to write config file", "error", err)
		return fmt.Errorf("write config for save: %w", err)
	}

	cc.logger.Infow("Saved user settings to config file", "path", cc.configPath)

	// apply immediately instead of relying on the watcher's debounce timing
	if err := cc.loadLocked(localizer); err != nil {
		return fmt.Errorf("load config after save: %w", err)
	}

	return nil
}

// writeFileAtomic writes data to a temp file in the target's directory and
// renames it over the target, so a crash mid-write can't corrupt the config
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func yamlStringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlIntNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}
