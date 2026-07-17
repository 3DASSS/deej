package deej

import (
	"errors"
	"sort"

	"go.bug.st/serial/enumerator"
)

// SettingsService exposes configuration APIs to the settings GUI frontend
type SettingsService struct {
	d *Deej
}

func newSettingsService(d *Deej) *SettingsService {
	return &SettingsService{d: d}
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
	Version          string   `json:"version"`
	ConfigPath       string   `json:"configPath"`
	ResolvedLanguage string   `json:"resolvedLanguage"`
	SpecialTargets   []string `json:"specialTargets"`
}

// SessionInfoDTO describes a running audio session for target suggestions
type SessionInfoDTO struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"` // friendly name, may be empty
	IsDevice    bool   `json:"isDevice"`    // device master session, not a process
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
	}
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
