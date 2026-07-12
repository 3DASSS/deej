<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2>{t("language")}</h2>

  <div class="field" style="max-width: 340px;">
    <select id="language" aria-label={t("language")} bind:value={settings.language}>
      <option value="auto">{t("languageAuto")}</option>
      <option value="en">English</option>
      <option value="ru">Русский</option>
    </select>
    <div class="hint">{t("languageHint")}</div>
  </div>
</section>
