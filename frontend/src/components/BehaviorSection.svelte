<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2>{t("behavior")}</h2>

  <div class="checkbox-field">
    <input id="invert-sliders" type="checkbox" bind:checked={settings.invertSliders} />
    <label for="invert-sliders">{t("invertSliders")} ({t("invertSlidersHint")})</label>
  </div>

  <div class="field" style="max-width: 340px;">
    <label for="noise-reduction">{t("noiseReduction")}</label>
    <select id="noise-reduction" bind:value={settings.noiseReduction}>
      {#if settings.noiseReduction === ""}
        <option value="">{t("noiseDefault")}</option>
      {/if}
      <option value="default">{t("noiseDefault")}</option>
      <option value="low">{t("noiseLow")}</option>
      <option value="high">{t("noiseHigh")}</option>
      <option value="none">{t("noiseNone")}</option>
    </select>
  </div>
</section>
