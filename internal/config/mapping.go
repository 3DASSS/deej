package config

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// ActiveMapping returns the slider mapping deej is currently driving. It's the
// only mapping lookup on the hot path, so it stays a plain scan over the (very
// short) profile list
func (s *Settings) ActiveMapping() SliderMappings {
	for i := range s.Profiles {
		if s.Profiles[i].Name == s.ActiveProfile {
			return s.Profiles[i].SliderMapping
		}
	}

	// normalized settings always resolve above; fall back to the first profile
	// so an unnormalized snapshot still does something sensible
	if len(s.Profiles) > 0 {
		return s.Profiles[0].SliderMapping
	}

	return nil
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

// Get returns the targets mapped to a slider index. The mapping is small
// (one entry per physical slider) and sanitized, so a linear scan is fine
func (sm SliderMappings) Get(slider int) ([]string, bool) {
	for _, entry := range sm {
		if entry.Slider == slider {
			return slices.Clone(entry.Targets), true
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

// normalized returns the canonical form of a mapping and the list of values
// that were invalid: negative and duplicate slider indexes are reported and
// dropped, empty targets (and entries left with none, so the snapshot matches
// what a save writes) are dropped silently, and the result is sorted by
// slider index
func (sm SliderMappings) normalized() (SliderMappings, []string) {
	var problems []string

	mapping := SliderMappings{}
	seenSliders := map[int]bool{}

	for _, entry := range sm {
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

	return mapping, problems
}
