import { m } from "../paraglide/messages";
import { app } from "./state.svelte";

export const OBS_PREFIX = "deej.obs:";
export const DISCORD_PREFIX = "deej.discord:";
export const DISCORD_INPUT = "deej.discord.input";
export const DISCORD_OUTPUT = "deej.discord.output";

// special target -> localized label/description message functions
const specialTargets: Record<string, { label: () => string; desc: () => string }> = {
  master: { label: m.targetMaster, desc: m.targetMasterDesc },
  system: { label: m.targetSystem, desc: m.targetSystemDesc },
  mic: { label: m.targetMic, desc: m.targetMicDesc },
  "deej.current": { label: m.targetCurrent, desc: m.targetCurrentDesc },
  "deej.current.fullscreen": { label: m.targetCurrentFullscreen, desc: m.targetCurrentFullscreenDesc },
  "deej.unmapped": { label: m.targetUnmapped, desc: m.targetUnmappedDesc },
  [DISCORD_INPUT]: { label: m.targetDiscordInput, desc: m.targetDiscordInputDesc },
  [DISCORD_OUTPUT]: { label: m.targetDiscordOutput, desc: m.targetDiscordOutputDesc },
};

export function specialTargetLabel(target: string): string | null {
  const entry = specialTargets[target.toLowerCase()];
  return entry ? entry.label() : null;
}

export function specialTargetDescription(target: string): string | null {
  const entry = specialTargets[target.toLowerCase()];
  return entry ? entry.desc() : null;
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
  if (lower.startsWith(DISCORD_PREFIX)) {
    const id = target.slice(DISCORD_PREFIX.length);
    return `${app.discordUserNames[id] ?? id} (Discord)`;
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
