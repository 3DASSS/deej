import { mount } from "svelte";
import App from "./App.svelte";
import { setLocale } from "./paraglide/runtime";
import { AppInfoDTO, SettingsService } from "../bindings/github.com/nik9play/deej/pkg/deej";
import "./app.css";

let appInfo: AppInfoDTO | null = null;
try {
  appInfo = await SettingsService.GetAppInfo();
  // The Go host resolves the language; mirror it into paraglide's in-memory
  // locale before mount. reload: false — there's no server or in-app reload.
  const locale = appInfo.resolvedLanguage.toLowerCase().startsWith("ru") ? "ru" : "en";
  setLocale(locale, { reload: false });
} catch (err) {
  console.error("failed to load app info", err);
}

document.title = "deej";

mount(App, {
  target: document.getElementById("app")!,
  props: { appInfo },
});
