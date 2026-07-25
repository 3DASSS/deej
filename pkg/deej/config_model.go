package deej

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
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
	SliderMapping  SliderMappings `yaml:"slider_mapping" json:"sliderMapping"`
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

// applyLegacyKeys fills COM settings from the legacy flat keys (com_port,
// baud_rate, com_vid, com_pid) that older config files used. Loads call it
// before the main decode, so an explicit com: section overrides these. Saves
// only ever write the com: section, so this is read-only compatibility
func applyLegacyKeys(data []byte, s *Settings) {
	var legacy struct {
		Port     *string  `yaml:"com_port"`
		BaudRate *int     `yaml:"baud_rate"`
		VID      *HexWord `yaml:"com_vid"`
		PID      *HexWord `yaml:"com_pid"`
	}

	// a type error still decodes the well-typed keys; only a structural parse
	// error (which the main decode reports too) means nothing usable came back
	if err := yaml.Unmarshal(data, &legacy); err != nil && !isYAMLTypeError(err) {
		return
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
		SliderMapping: SliderMappings{},
		Language:      "auto",

		COM: COMSettings{
			Port:     "auto",
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

// normalize canonicalizes the settings in place and returns the list of
// values that were invalid and had to be replaced with a default (or
// dropped). It is the single rule set behind both config policies: GUI saves
// reject when the returned list is non-empty; config-file loads apply the
// fixes and log the list. Silent canonicalizations (blank VID/PID -> default,
// empty OBS host -> default, dropping empty targets, sorting the mapping) are
// not reported
func (s *Settings) normalize() []string {
	var problems []string
	defaults := defaultSettings()

	if s.COM.Port == "" {
		problems = append(problems, "com port must not be empty")
		s.COM.Port = defaults.COM.Port
	}

	if s.COM.BaudRate <= 0 {
		problems = append(problems, fmt.Sprintf("baud rate must be positive, got %d", s.COM.BaudRate))
		s.COM.BaudRate = defaults.COM.BaudRate
	}

	// a blank VID/PID means "use the built-in default" and is not a problem;
	// only a non-blank value that isn't valid hex is
	if vid, ok := s.COM.VID.canonical(defaults.COM.VID); ok {
		s.COM.VID = vid
	} else {
		problems = append(problems, fmt.Sprintf("com vid %q is not a 16-bit hex value", string(s.COM.VID)))
		s.COM.VID = vid
	}

	if pid, ok := s.COM.PID.canonical(defaults.COM.PID); ok {
		s.COM.PID = pid
	} else {
		problems = append(problems, fmt.Sprintf("com pid %q is not a 16-bit hex value", string(s.COM.PID)))
		s.COM.PID = pid
	}

	if !slices.Contains(validNoiseReductionLevels, s.NoiseReduction) {
		problems = append(problems, fmt.Sprintf("invalid noise reduction level: %q", s.NoiseReduction))
		s.NoiseReduction = defaults.NoiseReduction
	}

	if !slices.Contains(validLanguages, s.Language) {
		problems = append(problems, fmt.Sprintf("invalid language: %q", s.Language))
		s.Language = defaults.Language
	}

	if s.OBS.Port < 1 || s.OBS.Port > 65535 {
		problems = append(problems, fmt.Sprintf("obs port must be between 1 and 65535, got %d", s.OBS.Port))
		s.OBS.Port = defaults.OBS.Port
	}

	if s.OBS.Host == "" {
		s.OBS.Host = defaults.OBS.Host
	}

	// canonicalize the mapping: report and drop negative/duplicate sliders,
	// silently drop empty targets (and entries left with none, so the snapshot
	// matches what a save writes), keep it sorted by slider index
	mapping := SliderMappings{}
	seenSliders := map[int]bool{}
	for _, entry := range s.SliderMapping {
		if entry.Slider < 0 {
			problems = append(problems, fmt.Sprintf("slider index must not be negative: %d", entry.Slider))
			continue
		}
		if seenSliders[entry.Slider] {
			problems = append(problems, fmt.Sprintf("duplicate slider index: %d", entry.Slider))
			continue
		}
		seenSliders[entry.Slider] = true

		targets := slices.DeleteFunc(slices.Clone(entry.Targets), func(t string) bool { return t == "" })
		if len(targets) == 0 {
			continue
		}

		mapping = append(mapping, SliderMappingEntry{Slider: entry.Slider, Targets: targets})
	}
	slices.SortFunc(mapping, func(a, b SliderMappingEntry) int { return cmp.Compare(a.Slider, b.Slider) })
	s.SliderMapping = mapping

	return problems
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
// for GUI saves comes from Settings.normalize instead
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
// the example config. Every save runs Settings.normalize first, so the
// entries are already sorted, deduped and free of empty targets
func (sm SliderMappings) MarshalYAML() (any, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	for _, entry := range sm {
		if len(entry.Targets) == 0 {
			continue
		}

		var value *yaml.Node
		if len(entry.Targets) == 1 {
			value = yamlStringNode(entry.Targets[0])
		} else {
			value = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, target := range entry.Targets {
				value.Content = append(value.Content, yamlStringNode(target))
			}
		}

		mapping.Content = append(mapping.Content, yamlIntNode(entry.Slider), value)
	}

	return mapping, nil
}

// get returns the targets mapped to a slider index. The mapping is small
// (one entry per physical slider) and sanitized, so a linear scan is fine
func (sm SliderMappings) get(slider int) ([]string, bool) {
	for _, entry := range sm {
		if entry.Slider == slider {
			return entry.Targets, true
		}
	}

	return nil, false
}

// String is a compact form for logs
func (sm SliderMappings) String() string {
	targets := 0
	for _, entry := range sm {
		targets += len(entry.Targets)
	}

	return fmt.Sprintf("<%d sliders mapped to %d targets>", len(sm), targets)
}

func (sm SliderMappings) clone() SliderMappings {
	out := make(SliderMappings, len(sm))
	for i, entry := range sm {
		out[i] = SliderMappingEntry{Slider: entry.Slider, Targets: slices.Clone(entry.Targets)}
	}

	return out
}

func yamlStringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlIntNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}
