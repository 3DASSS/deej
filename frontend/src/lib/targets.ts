import { t } from "./i18n";
import { app } from "./state.svelte";

export const OBS_PREFIX = "deej.obs:";

// special target -> i18n label/description key suffix
const specialTargetKeys: Record<string, string> = {
  master: "Master",
  system: "System",
  mic: "Mic",
  "deej.current": "Current",
  "deej.current.fullscreen": "CurrentFullscreen",
  "deej.unmapped": "Unmapped",
};

export function specialTargetLabel(target: string): string | null {
  const key = specialTargetKeys[target.toLowerCase()];
  return key ? t(`target${key}`) : null;
}

export function specialTargetDescription(target: string): string | null {
  const key = specialTargetKeys[target.toLowerCase()];
  return key ? t(`target${key}Desc`) : null;
}

// targetLabel resolves a slider target to a human-friendly display name:
// localized names for special targets, the input name for OBS targets, the
// session's display name (e.g. exe file description) when it's running, and
// the target as written in the config otherwise
export function targetLabel(target: string): string {
  const special = specialTargetLabel(target);
  if (special !== null) {
    return special;
  }

  const lower = target.toLowerCase();
  if (lower.startsWith(OBS_PREFIX)) {
    return `${target.slice(OBS_PREFIX.length)} (OBS)`;
  }

  const session = app.sessions.find((s) => s.key === lower);
  if (session) {
    return session.displayName || prettifyProcessName(target);
  }

  return target;
}

export function prettifyProcessName(name: string): string {
  const base = name.toLowerCase().endsWith(".exe") ? name.slice(0, -4) : name;
  return base.charAt(0).toUpperCase() + base.slice(1);
}
