package config

import (
	"errors"

	"go.yaml.in/yaml/v3"
)

// applyLegacyKeys fills settings from the flat keys older config files used:
// the com ones (com_port, baud_rate, com_vid, com_pid) and the top-level
// slider_mapping that predates profiles. Loads call it before the main decode,
// so an explicit com:/profiles: section overrides these. Saves only ever write
// the new form, so this is read-only compatibility
func applyLegacyKeys(data []byte, s *Settings) {
	var legacy struct {
		Port          *string         `yaml:"com_port"`
		BaudRate      *int            `yaml:"baud_rate"`
		VID           *HexWord        `yaml:"com_vid"`
		PID           *HexWord        `yaml:"com_pid"`
		SliderMapping *SliderMappings `yaml:"slider_mapping"`
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

	// a pre-profiles config: read the single mapping as the default profile.
	// yaml replaces a slice wholesale, so a profiles: section in the same file
	// still wins when the main decode runs
	if legacy.SliderMapping != nil {
		s.Profiles = []Profile{{Name: defaultProfileName, SliderMapping: *legacy.SliderMapping}}
	}
}

func isYAMLTypeError(err error) bool {
	var typeErr *yaml.TypeError
	return errors.As(err, &typeErr)
}
