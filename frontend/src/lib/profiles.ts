import type { Profile, Settings, SliderMappings } from "../../bindings/github.com/nik9play/deej/pkg/deej";

// mirrors Settings.ActiveMapping on the go side: normalized settings always
// resolve by name, the first-profile fallback only covers the brief moment
// before the first config arrives
export function activeProfile(settings: Settings | null): Profile | null {
  if (!settings) return null;

  return settings.profiles.find((profile) => profile.name === settings.activeProfile) ?? settings.profiles[0] ?? null;
}

export function activeMapping(settings: Settings | null): SliderMappings {
  return activeProfile(settings)?.sliderMapping ?? [];
}
