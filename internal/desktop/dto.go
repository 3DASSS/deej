//go:build !headless

package desktop

import "github.com/nik9play/deej/internal/mixer"

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
	Platform           string   `json:"platform"`
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

// DiscordUserDTO describes one member of the current voice channel
type DiscordUserDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DiscordStatusDTO describes the Discord integration's state for the settings GUI
type DiscordStatusDTO struct {
	Linked    bool   `json:"linked"`    // an account has been authorized
	Connected bool   `json:"connected"` // the RPC connection is up
	Channel   string `json:"channel"`   // the voice channel the user is in, if any
}

func sessionDTOs(in []mixer.SessionInfo) []SessionInfoDTO {
	out := make([]SessionInfoDTO, 0, len(in))
	for _, s := range in {
		out = append(out, SessionInfoDTO{Key: s.Key, DisplayName: s.DisplayName, IsDevice: s.IsDevice, IsInput: s.IsInput})
	}
	return out
}
