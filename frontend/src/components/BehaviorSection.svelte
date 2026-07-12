<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("behavior")}</h2>

  <div class="mb-3 flex items-center gap-2">
    <input id="invert-sliders" type="checkbox" class="size-4 accent-accent" bind:checked={settings.invertSliders} />
    <label class="text-sm" for="invert-sliders">{t("invertSliders")} ({t("invertSlidersHint")})</label>
  </div>

  <div class="flex max-w-xs flex-col gap-1">
    <label class="label" for="noise-reduction">{t("noiseReduction")}</label>
    <select id="noise-reduction" class="input" bind:value={settings.noiseReduction}>
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
