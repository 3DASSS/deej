//go:build !headless

package desktop

import (
	"encoding/base64"
	"errors"
	"maps"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/serial"

	"github.com/nik9play/deej/internal/platform"
)

// SettingsService exposes configuration APIs to the settings GUI frontend
type SettingsService struct {
	d *Desktop

	iconMu    sync.Mutex
	iconCache map[string]string
}

func newSettingsService(d *Desktop) *SettingsService {
	return &SettingsService{d: d, iconCache: make(map[string]string)}
}

// GetSettings returns the current contents of the user config file
func (s *SettingsService) GetSettings() config.Settings {
	return s.d.config.UserSettings()
}

// SaveSettings validates and writes the given settings to the user config
// file, applying them immediately
func (s *SettingsService) SaveSettings(settings config.Settings) error {
	return s.d.config.SaveUserSettings(settings)
}

// SetActiveProfile switches to the named profile. It's a targeted write, so a
// switch can't clobber a hand edit that landed since the window was opened
func (s *SettingsService) SetActiveProfile(name string) error {
	return s.d.config.SetActiveProfile(name)
}

// GetAppInfo returns version and localization info along with the list of
// special slider targets deej supports
func (s *SettingsService) GetAppInfo() AppInfoDTO {
	return AppInfoDTO{
		Version:          s.d.version,
		ConfigPath:       s.d.config.Path(),
		ResolvedLanguage: s.d.resolvedLanguage,
		Platform:         runtime.GOOS,
		SpecialTargets: []string{
			audio.MasterSessionName,
			audio.SystemSessionName,
			audio.InputSessionName,
			"deej.current",
			"deej.current.fullscreen",
			"deej.unmapped",
		},
		AutostartAvailable: runtime.GOOS == "windows",
	}
}

// GetAutostart reports whether deej is set to run at system startup
func (s *SettingsService) GetAutostart() bool {
	return platform.GetAutostartState()
}

// SetAutostart enables or disables running deej at system startup, applying
// the change immediately
func (s *SettingsService) SetAutostart(state bool) error {
	return platform.SetAutostartState(state)
}

// GetStatus returns the current serial connection state and slider values,
// so the settings window doesn't have to wait for the first live event
func (s *SettingsService) GetStatus() StatusDTO {
	return StatusDTO{
		Connected:    s.d.serial.GetState(),
		ComPort:      s.d.serial.CurrentComPort(),
		SliderValues: s.d.serial.CurrentSliderPercentValues(),
	}
}

// GetSessions returns the current audio sessions with friendly display
// names, for slider mapping suggestions
func (s *SettingsService) GetSessions() []SessionInfoDTO {
	return sessionDTOs(s.d.sessions.SessionInfos())
}

// GetProcesses returns the deduplicated, sorted executable names of all
// running processes
func (s *SettingsService) GetProcesses() ([]string, error) {
	pidsByName, err := platform.ProcessIDsByName()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(pidsByName))
	for name := range pidsByName {
		names = append(names, name)
	}

	sort.Strings(names)

	return names, nil
}

// processIconDataURI extracts the icon shared by the processes with the
// given PIDs, as a PNG data URI.
func processIconDataURI(pids []int) (icon string, conclusive bool) {
	triedPaths := make(map[string]struct{})

	for _, pid := range pids {
		path, err := platform.GetProcessImagePath(uint32(pid))
		if err != nil {
			continue
		}
		if _, tried := triedPaths[path]; tried {
			continue
		}
		triedPaths[path] = struct{}{}

		if pngBytes, err := platform.GetFileIconPNG(path); err == nil {
			return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), true
		}
	}

	return "", len(triedPaths) > 0
}

// GetProcessIcons returns PNG data URIs for the icons of the given process
// names, keyed by the names exactly as passed. Icons are cached across calls
func (s *SettingsService) GetProcessIcons(names []string) map[string]string {
	result := make(map[string]string)

	if !platform.ProcessIconsSupported {
		return result
	}

	missing := make(map[string]struct{})

	s.iconMu.Lock()
	for _, name := range names {
		key := strings.ToLower(name)
		if icon, ok := s.iconCache[key]; ok {
			if icon != "" {
				result[name] = icon
			}
		} else {
			missing[key] = struct{}{}
		}
	}
	s.iconMu.Unlock()

	if len(missing) == 0 {
		return result
	}

	pidsByName, err := platform.ProcessIDsByName()
	if err != nil {
		return result
	}

	extracted := make(map[string]string, len(missing))
	for key := range missing {
		icon, conclusive := processIconDataURI(pidsByName[key])
		if conclusive {
			extracted[key] = icon
		}
	}

	s.iconMu.Lock()
	maps.Copy(s.iconCache, extracted)
	s.iconMu.Unlock()

	for _, name := range names {
		if icon := extracted[strings.ToLower(name)]; icon != "" {
			result[name] = icon
		}
	}

	return result
}

// GetOBSInputs returns the input names of the connected OBS instance, for
// slider mapping suggestions
func (s *SettingsService) GetOBSInputs() ([]string, error) {
	if s.d.obs == nil {
		return nil, errors.New("OBS integration is not initialized")
	}

	return s.d.obs.ListInputs()
}

// GetDiscordStatus returns the Discord integration's live state
func (s *SettingsService) GetDiscordStatus() DiscordStatusDTO {
	if s.d.discord == nil {
		return DiscordStatusDTO{}
	}

	status := s.d.discord.Status()

	return DiscordStatusDTO{Linked: status.Linked, Connected: status.Connected, Channel: status.Channel}
}

// GetDiscordUsers returns the members of the voice channel the user is
// currently in, for slider mapping suggestions. The roster is kept current by
// RPC events, so this serves it as-is; the GUI is told to re-read it by the
// deej:discord event rather than by polling
func (s *SettingsService) GetDiscordUsers() ([]DiscordUserDTO, error) {
	if s.d.discord == nil {
		return nil, errors.New("discord integration is not initialized")
	}

	users := s.d.discord.VoiceUsers()
	result := make([]DiscordUserDTO, 0, len(users))
	for _, u := range users {
		result = append(result, DiscordUserDTO{ID: u.ID, Name: u.Name})
	}
	return result, nil
}

// LinkDiscord runs the interactive authorization. Discord shows the user a
// consent dialog, so this blocks until they answer it (or time out)
func (s *SettingsService) LinkDiscord() error {
	if s.d.discord == nil {
		return errors.New("discord integration is not initialized")
	}

	return s.d.discord.Link()
}

// UnlinkDiscord forgets the stored authorization
func (s *SettingsService) UnlinkDiscord() error {
	if s.d.discord == nil {
		return errors.New("discord integration is not initialized")
	}

	return s.d.discord.Unlink()
}

// ListSerialPorts enumerates the serial ports available on this machine
func (s *SettingsService) ListSerialPorts() ([]SerialPortDTO, error) {
	ports, err := serial.ListPorts()
	if err != nil {
		return nil, err
	}

	result := make([]SerialPortDTO, 0, len(ports))
	for _, port := range ports {
		result = append(result, SerialPortDTO{
			Name:    port.Name,
			IsUSB:   port.IsUSB,
			VID:     port.VID,
			PID:     port.PID,
			Product: port.Product,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result, nil
}
