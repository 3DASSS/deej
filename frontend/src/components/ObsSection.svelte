<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2>{t("obs")}</h2>

  <div class="checkbox-field">
    <input id="obs-enabled" type="checkbox" bind:checked={settings.obsEnabled} />
    <label for="obs-enabled">{t("obsEnabled")}</label>
  </div>
  <div class="hint" style="margin-bottom: 12px;">{t("obsHint")}</div>

  {#if settings.obsEnabled}
    <div class="field-row">
      <div class="field">
        <label for="obs-host">{t("obsHost")}</label>
        <input id="obs-host" type="text" bind:value={settings.obsHost} />
      </div>
      <div class="field">
        <label for="obs-port">{t("obsPort")}</label>
        <input id="obs-port" type="number" min="1" max="65535" bind:value={settings.obsPort} />
      </div>
      <div class="field">
        <label for="obs-password">{t("obsPassword")}</label>
        <input id="obs-password" type="password" bind:value={settings.obsPassword} />
      </div>
    </div>
  {/if}
</section>
