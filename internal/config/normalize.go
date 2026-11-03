package config

import (
	"fmt"
	"slices"
	"strings"
)

// findProfileName resolves a profile name case-insensitively, returning it in
// the spelling the profile actually uses
func findProfileName(profiles []Profile, name string) (string, bool) {
	for _, profile := range profiles {
		if strings.EqualFold(profile.Name, name) {
			return profile.Name, true
		}
	}

	return "", false
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

	problems = append(problems, s.normalizeDiscord()...)
	problems = append(problems, s.normalizeProfiles()...)

	return problems
}

// normalizeDiscord canonicalizes the Discord credentials. The integration
// can't do anything without both of them, so enabling it without them is
// reported rather than left as a connection that quietly never happens
func (s *Settings) normalizeDiscord() []string {
	var problems []string

	s.Discord.ClientID = strings.TrimSpace(s.Discord.ClientID)
	s.Discord.ClientSecret = strings.TrimSpace(s.Discord.ClientSecret)

	if !s.Discord.Enabled {
		return nil
	}

	switch {
	case s.Discord.ClientID == "":
		problems = append(problems, "discord client id must not be empty")

	// a client id is a snowflake; catching a pasted client *secret* here saves
	// a confusing round trip through discord's OAuth error responses
	case !isDigits(s.Discord.ClientID):
		problems = append(problems, fmt.Sprintf("discord client id must be numeric, got %q", s.Discord.ClientID))
		s.Discord.ClientID = ""
	}

	if s.Discord.ClientSecret == "" {
		problems = append(problems, "discord client secret must not be empty")
	}

	// leave the file self-consistent: enabled without usable credentials would
	// otherwise sit there looking like it should be working
	if len(problems) > 0 {
		s.Discord.Enabled = false
	}

	return problems
}

func isDigits(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// normalizeProfiles canonicalizes the profile list and the active profile.
// There is always at least one profile afterwards, and ActiveProfile always
// names one of them
func (s *Settings) normalizeProfiles() []string {
	var problems []string

	profiles := make([]Profile, 0, len(s.Profiles))
	seenNames := map[string]bool{}
	seenHotkeys := map[string]bool{}

	for i, profile := range s.Profiles {
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			generated := fmt.Sprintf("profile %d", i+1)
			problems = append(problems, fmt.Sprintf("profile %d has no name, using %q", i+1, generated))
			profile.Name = generated
		}

		if seenNames[strings.ToLower(profile.Name)] {
			problems = append(problems, fmt.Sprintf("duplicate profile name: %q", profile.Name))
			continue
		}
		seenNames[strings.ToLower(profile.Name)] = true

		profile.Hotkey = strings.TrimSpace(profile.Hotkey)
		if profile.Hotkey != "" {
			switch {
			case !validHotkey(profile.Hotkey):
				problems = append(problems, fmt.Sprintf("invalid hotkey for profile %q: %q", profile.Name, profile.Hotkey))
				profile.Hotkey = ""

			// the OS would reject the second registration anyway; dropping it
			// here keeps the file honest about what's actually bound
			case seenHotkeys[strings.ToLower(profile.Hotkey)]:
				problems = append(problems, fmt.Sprintf("duplicate hotkey %q on profile %q", profile.Hotkey, profile.Name))
				profile.Hotkey = ""

			default:
				seenHotkeys[strings.ToLower(profile.Hotkey)] = true
			}
		}

		mapping, mappingProblems := profile.SliderMapping.normalized()
		for _, problem := range mappingProblems {
			problems = append(problems, fmt.Sprintf("profile %q: %s", profile.Name, problem))
		}
		profile.SliderMapping = mapping

		profiles = append(profiles, profile)
	}

	// a config with no profiles at all (or one whose every profile was dropped
	// as a duplicate) gets an empty default, so consumers never see none
	if len(profiles) == 0 {
		profiles = append(profiles, Profile{Name: defaultProfileName, SliderMapping: SliderMappings{}})
	}
	s.Profiles = profiles

	s.ActiveProfile = strings.TrimSpace(s.ActiveProfile)
	if resolved, ok := findProfileName(profiles, s.ActiveProfile); ok {
		s.ActiveProfile = resolved
	} else {
		// an empty value just means "no preference", only a name that doesn't
		// resolve is worth reporting
		if s.ActiveProfile != "" {
			problems = append(problems, fmt.Sprintf("unknown active profile: %q", s.ActiveProfile))
		}
		s.ActiveProfile = profiles[0].Name
	}

	return problems
}

// validHotkeyModifiers mirrors the modifiers wails accepts in an accelerator
// (see application.modifierMap). The key itself is left to wails to validate,
// which reports an unsupported one when it registers the shortcut
var validHotkeyModifiers = []string{
	"cmdorctrl", "cmd", "command", "ctrl", "optionoralt", "alt", "option", "shift", "super",
}

// validHotkey reports whether a profile hotkey looks like a wails accelerator
// with at least one modifier. The modifier is not optional: a bare key would
// be grabbed system-wide, so deej would swallow it in every other application
func validHotkey(hotkey string) bool {
	components := strings.Split(hotkey, "+")
	if len(components) < 2 {
		return false
	}

	for _, modifier := range components[:len(components)-1] {
		if !slices.Contains(validHotkeyModifiers, strings.ToLower(strings.TrimSpace(modifier))) {
			return false
		}
	}

	return strings.TrimSpace(components[len(components)-1]) != ""
}
