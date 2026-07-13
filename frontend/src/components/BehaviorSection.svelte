<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import FieldSelect from "./ui/FieldSelect.svelte";

  let { settings }: { settings: SettingsDTO } = $props();

  const noiseItems = $derived([
    ...(settings.noiseReduction === "" ? [{ value: "", label: t("noiseDefault") }] : []),
    { value: "default", label: t("noiseDefault") },
    { value: "low", label: t("noiseLow") },
    { value: "high", label: t("noiseHigh") },
    { value: "none", label: t("noiseNone") },
  ]);
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("behavior")}</h2>

  <div class="mb-3">
    <FieldCheckbox
      id="invert-sliders"
      bind:checked={settings.invertSliders}
      label="{t('invertSliders')} ({t('invertSlidersHint')})"
    />
  </div>

  <div class="flex max-w-xs flex-col gap-1">
    <label class="label" for="noise-reduction">{t("noiseReduction")}</label>
    <FieldSelect id="noise-reduction" bind:value={settings.noiseReduction} items={noiseItems} />
  </div>
</section>
