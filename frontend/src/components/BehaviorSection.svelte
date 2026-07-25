<script lang="ts">
  import { Settings } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import FieldSelect from "./ui/FieldSelect.svelte";

  // these settings are cheap to apply, so they're written as soon as they
  // change instead of behind a save button
  let { settings, onsave }: { settings: Settings; onsave: (patch: Partial<Settings>) => void } = $props();

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
      label="{m.invertSliders()} ({m.invertSlidersHint()})"
      bind:checked={() => settings.invertSliders, (invertSliders) => onsave({ invertSliders })}
    />
  </div>

  <div class="flex max-w-xs flex-col gap-1">
    <label class="label" for="noise-reduction">{m.noiseReduction()}</label>
    <FieldSelect
      id="noise-reduction"
      items={noiseItems}
      bind:value={() => settings.noiseReduction, (noiseReduction) => onsave({ noiseReduction })}
    />
  </div>
</section>
