package deej

import (
	"encoding/base64"
	"errors"
	"maps"
	"runtime"
	"sort"
	"strings"
	"sync"

	ps "github.com/mitchellh/go-ps"
	"go.bug.st/serial/enumerator"

	"github.com/nik9play/deej/pkg/deej/util"
)

// SettingsService exposes configuration APIs to the settings GUI frontend
type SettingsService struct {
	d *Deej

	iconMu    sync.Mutex
	iconCache map[string]string
}

func newSettingsService(d *Deej) *SettingsService {
	return &SettingsService{d: d, iconCache: make(map[string]string)}
}

// SerialPortDTO describes an available serial port
type SerialPortDTO struct {
	Name    string `json:"name"`
	IsUSB   bool   `json:"isUsb"`
	VID     string `json:"vid"`
	PID     string `json:"pid"`
	Product string `json:"product"`
}

// AppInfoDTO describes static application info for the settings GUI
type AppInfoDTO struct {
	Version            string   `json:"version"`
	ConfigPath         string   `json:"configPath"`
	ResolvedLanguage   string   `json:"resolvedLanguage"`
	SpecialTargets     []string `json:"specialTargets"`
	AutostartAvailable bool     `json:"autostartAvailable"`
}

// SessionInfoDTO describes a running audio session for target suggestions
type SessionInfoDTO struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"` // friendly name, may be empty
	IsDevice    bool   `json:"isDevice"`    // device master session, not a process
	IsInput     bool   `json:"isInput"`     // capture-side session (microphone)
}

// StatusDTO describes the live connection state for the settings GUI
type StatusDTO struct {
	Connected    bool      `json:"connected"`
	ComPort      string    `json:"comPort"`
	SliderValues []float32 `json:"sliderValues"` // 0..1, as sessions receive them
}

// GetSettings returns the current contents of the user config file
func (s *SettingsService) GetSettings() Settings {
	return s.d.config.UserSettings()
}

// SaveSettings validates and writes the given settings to the user config
// file, applying them immediately
func (s *SettingsService) SaveSettings(settings Settings) error {
	return s.d.config.SaveUserSettings(settings, s.d.localizer)
}

// GetAppInfo returns version and localization info along with the list of
// special slider targets deej supports
func (s *SettingsService) GetAppInfo() AppInfoDTO {
	return AppInfoDTO{
		Version:          s.d.version,
		ConfigPath:       s.d.config.configPath,
		ResolvedLanguage: s.d.resolvedLanguage,
		SpecialTargets: []string{
			masterSessionName,
			systemSessionName,
			inputSessionName,
			specialTargetTransformPrefix + specialTargetCurrentWindow,
			specialTargetTransformPrefix + specialTargetCurrentFullscreenWindow,
			specialTargetTransformPrefix + specialTargetAllUnmapped,
		},
		AutostartAvailable: runtime.GOOS == "windows",
	}
}

// GetAutostart reports whether deej is set to run at system startup
func (s *SettingsService) GetAutostart() bool {
	return util.GetAutostartState()
}

// SetAutostart enables or disables running deej at system startup, applying
// the change immediately
func (s *SettingsService) SetAutostart(state bool) error {
	return util.SetAutostartState(state)
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
	return s.d.sessions.sessionInfos()
}

// processIDsByName enumerates running processes and groups their PIDs by
// lowercased executable name, skipping pseudo-processes like the Windows
// "[System Process]" (pid 0)
func processIDsByName() (map[string][]int, error) {
	processes, err := ps.Processes()
	if err != nil {
		return nil, err
	}

	pidsByName := make(map[string][]int, len(processes))
	for _, process := range processes {
		name := strings.ToLower(process.Executable())
		if name == "" || strings.HasPrefix(name, "[") {
			continue
		}

		pidsByName[name] = append(pidsByName[name], process.Pid())
	}

	return pidsByName, nil
}

// GetProcesses returns the deduplicated, sorted executable names of all
// running processes
func (s *SettingsService) GetProcesses() ([]string, error) {
	pidsByName, err := processIDsByName()
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
		path, err := util.GetProcessImagePath(uint32(pid))
		if err != nil {
			continue
		}
		if _, tried := triedPaths[path]; tried {
			continue
		}
		triedPaths[path] = struct{}{}

		if pngBytes, err := util.GetFileIconPNG(path); err == nil {
			return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), true
		}
	}

	return "", len(triedPaths) > 0
}

// GetProcessIcons returns PNG data URIs for the icons of the given process
// names, keyed by the names exactly as passed. Icons are cached across calls
func (s *SettingsService) GetProcessIcons(names []string) map[string]string {
	result := make(map[string]string)

	if !util.ProcessIconsSupported {
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

	pidsByName, err := processIDsByName()
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

// ListSerialPorts enumerates the serial ports available on this machine
func (s *SettingsService) ListSerialPorts() ([]SerialPortDTO, error) {
	ports, err := enumerator.GetDetailedPortsList()
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
