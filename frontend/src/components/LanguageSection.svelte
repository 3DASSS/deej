<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("language")}</h2>

  <div class="flex max-w-xs flex-col gap-1">
    <select id="language" class="input" aria-label={t("language")} bind:value={settings.language}>
      <option value="auto">{t("languageAuto")}</option>
      <option value="en">English</option>
      <option value="ru">Русский</option>
    </select>
    <div class="hint">{t("languageHint")}</div>
  </div>
</section>
