<script lang="ts">
  import { Settings } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import FieldSelect from "./ui/FieldSelect.svelte";

  let { settings }: { settings: Settings } = $props();

  const noiseItems = $derived([
    ...(settings.noiseReduction === "" ? [{ value: "", label: m.noiseDefault() }] : []),
    { value: "default", label: m.noiseDefault() },
    { value: "low", label: m.noiseLow() },
    { value: "high", label: m.noiseHigh() },
    { value: "none", label: m.noiseNone() },
  ]);
</script>

<section>
  <div class="mb-3">
    <FieldCheckbox
      id="invert-sliders"
      bind:checked={settings.invertSliders}
      label="{m.invertSliders()} ({m.invertSlidersHint()})"
    />
  </div>

  <div class="flex max-w-xs flex-col gap-1">
    <label class="label" for="noise-reduction">{m.noiseReduction()}</label>
    <FieldSelect id="noise-reduction" bind:value={settings.noiseReduction} items={noiseItems} />
  </div>
</section>
