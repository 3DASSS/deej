import { Events } from "@wailsio/runtime";
import { DiscordUserDTO, SessionInfoDTO, Settings, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
import { tickOnChange } from "./tick";

// live application state, fed by wails events from the Go side
export const app = $state({
  connected: false,
  comPort: "",
  values: [] as number[], // 0..1 per slider, as sessions receive them
  settings: null as Settings | null,
  sessions: [] as SessionInfoDTO[], // running audio sessions with friendly names
  discordUsers: [] as DiscordUserDTO[], // current voice-channel roster
  discordUserNames: {} as Record<string, string>, // names retained after users leave
});

export async function refreshSettings(): Promise<void> {
  try {
    // snapshot into a plain object so svelte's deep reactivity applies (the
    // wails bridge hands back class instances, which aren't made reactive)
    app.settings = $state.snapshot(await SettingsService.GetSettings());
  } catch (err) {
    console.error("failed to load settings", err);
  }
}

export async function refreshSessions(): Promise<void> {
  try {
    app.sessions = (await SettingsService.GetSessions()) ?? [];
  } catch (err) {
    console.error("failed to load sessions", err);
  }
}

export async function refreshDiscordUsers(): Promise<void> {
  try {
    app.discordUsers = (await SettingsService.GetDiscordUsers()) ?? [];
    app.discordUserNames = Object.assign(
      {},
      app.discordUserNames,
      Object.fromEntries(app.discordUsers.map((user) => [user.id, user.name])),
    );
  } catch {
    app.discordUsers = [];
  }
}

// init subscribes to live events (before fetching initial state, so nothing
// is missed) and returns a cleanup function for onDestroy
export function init(): () => void {
  const offs = [
    Events.On("deej:sliders", (ev) => {
      const values = (ev.data as number[]) ?? [];
      app.values = values;
      tickOnChange(values);
    }),
    Events.On("deej:state", (ev) => {
      const data = ev.data as { connected: boolean; comPort: string };
      app.connected = data.connected;
      app.comPort = data.comPort;
    }),
    Events.On("deej:config", () => {
      void refreshSettings();
    }),
    Events.On("deej:sessions", () => {
      void refreshSessions();
    }),
    Events.On("deej:discord", () => {
      void refreshDiscordUsers();
    }),
  ];

  SettingsService.GetStatus()
    .then((status) => {
      app.connected = status.connected;
      app.comPort = status.comPort;
      app.values = status.sliderValues ?? [];
    })
    .catch((err) => console.error("failed to load status", err));

  void refreshSettings();
  void refreshSessions();
  void refreshDiscordUsers();

  return () => offs.forEach((off) => off());
}
