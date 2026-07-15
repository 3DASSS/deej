package deej

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/thoas/go-funk"
	"go.yaml.in/yaml/v3"
)

// SliderMappingEntry is a JSON-friendly representation of a single slider's targets
type SliderMappingEntry struct {
	Slider  int      `json:"slider"`
	Targets []string `json:"targets"`
}

// SettingsDTO is a JSON-friendly mirror of the user config file, used by the settings GUI
type SettingsDTO struct {
	ComPort        string               `json:"comPort"`
	BaudRate       int                  `json:"baudRate"`
	ComVID         string               `json:"comVid"` // 16-bit hex string, e.g. "1A86"
	ComPID         string               `json:"comPid"`
	InvertSliders  bool                 `json:"invertSliders"`
	NoiseReduction string               `json:"noiseReduction"`
	Language       string               `json:"language"`
	OBSEnabled     bool                 `json:"obsEnabled"`
	OBSHost        string               `json:"obsHost"`
	OBSPort        int                  `json:"obsPort"`
	OBSPassword    string               `json:"obsPassword"`
	SliderMapping  []SliderMappingEntry `json:"sliderMapping"`
}

// how long after a GUI-initiated config write to ignore the resulting
// filesystem event, since the save already loads and applies the config itself
const selfWriteSuppressWindow = 2 * time.Second

var validNoiseReductionLevels = []string{"", "low", "default", "high", "none"}
var validLanguages = []string{"auto", "en", "ru"}

// Validate checks the DTO for values that would produce a broken config file
func (dto *SettingsDTO) Validate() error {
	if dto.ComPort == "" {
		return fmt.Errorf("com port must not be empty")
	}

	if dto.BaudRate <= 0 {
		return fmt.Errorf("baud rate must be a positive number")
	}

	// an empty VID/PID means "use the built-in default", so only validate
	// non-empty values
	if _, err := parseHexWordOrDefault(dto.ComVID, defaultVID); err != nil {
		return fmt.Errorf("com vid: %w", err)
	}

	if _, err := parseHexWordOrDefault(dto.ComPID, defaultPID); err != nil {
		return fmt.Errorf("com pid: %w", err)
	}

	if !funk.ContainsString(validNoiseReductionLevels, dto.NoiseReduction) {
		return fmt.Errorf("invalid noise reduction level: %s", dto.NoiseReduction)
	}

	if !funk.ContainsString(validLanguages, dto.Language) {
		return fmt.Errorf("invalid language: %s", dto.Language)
	}

	if dto.OBSPort < 1 || dto.OBSPort > 65535 {
		return fmt.Errorf("obs port must be between 1 and 65535")
	}

	seenSliders := map[int]bool{}
	for _, entry := range dto.SliderMapping {
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

// parseHexWord parses a 16-bit hex string like "1A86" or "0x1A86"
func parseHexWord(value string) (uint64, error) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	parsed, err := strconv.ParseUint(trimmed, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("must be a 16-bit hex value: %s", value)
	}

	return parsed, nil
}

// parseHexWordOrDefault behaves like parseHexWord, but returns the given
// default for an empty (or whitespace-only) value. This lets the GUI leave the
// VID/PID fields blank to fall back to the built-in defaults, mirroring a
// config file that omits (or comments out) the com_vid/com_pid keys
func parseHexWordOrDefault(value string, def uint64) (uint64, error) {
	if strings.TrimSpace(value) == "" {
		return def, nil
	}

	return parseHexWord(value)
}

// UserSettings returns the current contents of the user config file as a DTO.
// It reads the user config only, so mappings merged from the internal config
// never leak into the user's config file on save
func (cc *CanonicalConfig) UserSettings() SettingsDTO {
	cc.viperLock.Lock()
	defer cc.viperLock.Unlock()

	dto := SettingsDTO{
		ComPort:        cc.userConfig.GetString(configKeyCOMPort),
		BaudRate:       cc.userConfig.GetInt(configKeyBaudRate),
		ComVID:         fmt.Sprintf("%04X", cc.userConfig.GetUint64(configKeyComVID)),
		ComPID:         fmt.Sprintf("%04X", cc.userConfig.GetUint64(configKeyComPID)),
		InvertSliders:  cc.userConfig.GetBool(configKeyInvertSliders),
		NoiseReduction: cc.userConfig.GetString(configKeyNoiseReductionLevel),
		Language:       cc.userConfig.GetString(configKeyLanguage),
		OBSEnabled:     cc.userConfig.GetBool(configKeyOBSEnabled),
		OBSHost:        cc.userConfig.GetString(configKeyOBSHost),
		OBSPort:        cc.userConfig.GetInt(configKeyOBSPort),
		OBSPassword:    cc.userConfig.GetString(configKeyOBSPassword),
		SliderMapping:  []SliderMappingEntry{},
	}

	for sliderIdxString, targets := range cc.userConfig.GetStringMapStringSlice(configKeySliderMapping) {
		sliderIdx, err := strconv.Atoi(sliderIdxString)
		if err != nil {
			continue
		}

		dto.SliderMapping = append(dto.SliderMapping, SliderMappingEntry{
			Slider:  sliderIdx,
			Targets: funk.FilterString(targets, func(s string) bool { return s != "" }),
		})
	}

	sort.Slice(dto.SliderMapping, func(i, j int) bool {
		return dto.SliderMapping[i].Slider < dto.SliderMapping[j].Slider
	})

	return dto
}

// SaveUserSettings validates the DTO, patches the user config file on disk
// (preserving comments, key order and unknown keys), and applies the new
// config immediately
func (cc *CanonicalConfig) SaveUserSettings(dto SettingsDTO, localizer *i18n.Localizer) error {
	if err := dto.Validate(); err != nil {
		return err
	}

	vid, _ := parseHexWordOrDefault(dto.ComVID, defaultVID)
	pid, _ := parseHexWordOrDefault(dto.ComPID, defaultPID)

	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		cc.logger.Warnw("Failed to read config file for saving", "error", err)
		return fmt.Errorf("read config for save: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		cc.logger.Warnw("Failed to parse config file for saving", "error", err)
		return fmt.Errorf("parse config for save: %w", err)
	}

	root := yamlEnsureDocumentMapping(&doc)

	yamlSetKey(root, configKeySliderMapping, sliderMappingNode(dto.SliderMapping))
	yamlSetKey(root, configKeyInvertSliders, yamlBoolNode(dto.InvertSliders))
	yamlSetKey(root, configKeyCOMPort, yamlStringNode(dto.ComPort))
	yamlSetKey(root, configKeyBaudRate, yamlIntNode(dto.BaudRate))
	yamlSetKey(root, "com_vid", yamlHexNode(vid))
	yamlSetKey(root, "com_pid", yamlHexNode(pid))
	yamlSetKey(root, configKeyLanguage, yamlStringNode(dto.Language))

	// don't add an empty noise_reduction key to configs that never had one
	if dto.NoiseReduction != "" || yamlFindValue(root, configKeyNoiseReductionLevel) != nil {
		yamlSetKey(root, configKeyNoiseReductionLevel, yamlStringNode(dto.NoiseReduction))
	}

	obs := yamlFindValue(root, "obs")
	if obs == nil || obs.Kind != yaml.MappingNode {
		obs = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSetKey(root, "obs", obs)
	}
	yamlSetKey(obs, "enabled", yamlBoolNode(dto.OBSEnabled))
	yamlSetKey(obs, "host", yamlStringNode(dto.OBSHost))
	yamlSetKey(obs, "port", yamlIntNode(dto.OBSPort))
	yamlSetKey(obs, "password", yamlStringNode(dto.OBSPassword))

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal config for save: %w", err)
	}

	// the write below must happen in place (truncate and write): the file
	// watcher only reacts to write events, and reacting to it twice is
	// prevented by suppressing events for the duration of the window
	cc.lastSelfWrite.Store(time.Now().UnixNano())

	if err := os.WriteFile(cc.configPath, out, 0o644); err != nil {
		cc.logger.Warnw("Failed to write config file", "error", err)
		return fmt.Errorf("write config for save: %w", err)
	}

	cc.logger.Infow("Saved user settings to config file", "path", cc.configPath)

	// apply immediately instead of relying on the watcher's debounce timing
	if err := cc.Load(localizer); err != nil {
		return fmt.Errorf("load config after save: %w", err)
	}
	cc.onConfigReloaded()

	return nil
}

func yamlStringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlIntNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

func yamlHexNode(value uint64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("0x%04X", value)}
}

func yamlBoolNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

// sliderMappingNode builds the slider_mapping value: single targets are
// written as plain scalars, multiple targets as sequences, matching the
// style of the example config
func sliderMappingNode(entries []SliderMappingEntry) *yaml.Node {
	sorted := make([]SliderMappingEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slider < sorted[j].Slider })

	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	for _, entry := range sorted {
		targets := funk.FilterString(entry.Targets, func(s string) bool { return s != "" })
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

	return mapping
}

// yamlEnsureDocumentMapping returns the document's root mapping node,
// creating the structure if the file was empty
func yamlEnsureDocumentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		return doc.Content[0]
	}

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	doc.Kind = yaml.DocumentNode
	doc.Tag = ""
	doc.Value = ""
	doc.Content = []*yaml.Node{root}

	return root
}

// yamlFindValue returns the value node for a key in a mapping node, or nil
func yamlFindValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}

// yamlSetKey replaces the value of a key in a mapping node, keeping the key
// node (and any comments attached to it) intact; missing keys are appended
func yamlSetKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			// keep comments that were attached to the old value node
			value.HeadComment = mapping.Content[i+1].HeadComment
			value.LineComment = mapping.Content[i+1].LineComment
			value.FootComment = mapping.Content[i+1].FootComment
			mapping.Content[i+1] = value
			return
		}
	}

	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
