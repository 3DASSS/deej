import en from "./en.json";
import ru from "./ru.json";

type Dictionary = Record<string, string>;

let dict: Dictionary = en;

// initI18n must be called before the app is mounted
export function initI18n(language: string): void {
  if (language.toLowerCase().startsWith("ru")) {
    dict = ru;
  } else {
    dict = en;
  }
}

export function t(key: string): string {
  return dict[key] ?? key;
}
