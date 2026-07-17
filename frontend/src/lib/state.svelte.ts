import { Events } from "@wailsio/runtime";
import { SessionInfoDTO, Settings, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";

// live application state, fed by wails events from the Go side
export const app = $state({
  connected: false,
  comPort: "",
  values: [] as number[], // 0..1 per slider, as sessions receive them
  settings: null as Settings | null,
  sessions: [] as SessionInfoDTO[], // running audio sessions with friendly names
});

export async function refreshSettings(): Promise<void> {
  try {
    const loaded = await SettingsService.GetSettings();
    // clone into a plain object so svelte's deep reactivity applies
    app.settings = JSON.parse(JSON.stringify(loaded));
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

// init subscribes to live events (before fetching initial state, so nothing
// is missed) and returns a cleanup function for onDestroy
export function init(): () => void {
  const offs = [
    Events.On("deej:sliders", (ev) => {
      app.values = (ev.data as number[]) ?? [];
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

  return () => offs.forEach((off) => off());
}
