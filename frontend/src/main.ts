import { mount } from "svelte";
import App from "./App.svelte";
import { initI18n } from "./lib/i18n";
import { AppInfoDTO, SettingsService } from "../bindings/github.com/nik9play/deej/pkg/deej";
import "./app.css";

let appInfo: AppInfoDTO | null = null;
try {
  appInfo = await SettingsService.GetAppInfo();
  initI18n(appInfo.resolvedLanguage);
} catch (err) {
  console.error("failed to load app info", err);
}

document.title = "deej";

mount(App, {
  target: document.getElementById("app")!,
  props: { appInfo },
});
