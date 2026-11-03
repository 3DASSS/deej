package config

// defaultProfileName is the profile a config without any gets, and the one a
// legacy top-level slider_mapping is migrated into
const defaultProfileName = "default"

func defaultSettings() Settings {
	return Settings{
		// the active profile is left to normalize, which resolves an empty
		// value to the first profile - so a config that only has profiles:
		// (and no active_profile:) isn't reported as pointing at a missing one
		Profiles: []Profile{{Name: defaultProfileName, SliderMapping: SliderMappings{}}},
		Language: "auto",

		COM: COMSettings{
			Port:     "auto",
			BaudRate: 9600,

			// ch340 chip
			VID: "1A86",
			PID: "7523",
		},

		OBS: OBSSettings{
			Host:             "localhost",
			Port:             4455,
			VolumeConversion: true,
		},
		Discord: DiscordSettings{
			// Preserve Discord's displayed volume curve unless explicitly disabled.
			VolumeConversion: true,
		},
	}
}
