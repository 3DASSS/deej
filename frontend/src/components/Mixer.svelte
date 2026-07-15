<script lang="ts">
  import { app } from "../lib/state.svelte";
  import { m } from "../paraglide/messages";
  import SliderColumn from "./SliderColumn.svelte";

  let { onEditTargets }: { onEditTargets: (slider: number) => void } = $props();

  const columns = $derived.by(() => {
    const mapping = app.settings?.sliderMapping ?? [];
    const maxMapped = mapping.reduce((max, entry) => Math.max(max, entry.slider + 1), 0);
    const count = Math.max(app.values.length, maxMapped, 1);

    return Array.from({ length: count }, (_, i) => ({
      slider: i,
      value: app.values[i] ?? 0,
      targets: mapping.find((entry) => entry.slider === i)?.targets ?? [],
    }));
  });
</script>

<div class="flex h-full flex-col">
  {#if !app.connected}
    <div class="shrink-0 border-b border-edge bg-chip px-4 py-1.5 text-center text-xs text-muted">
      {m.waitingForDevice()}{app.comPort ? ` (${app.comPort})` : ""}
    </div>
  {/if}

  <div
    class="flex flex-1 items-center justify-center gap-5 overflow-x-auto px-6 py-5 transition-opacity {app.connected
      ? ''
      : 'opacity-40 grayscale'}"
  >
    {#each columns as column (column.slider)}
      <SliderColumn
        slider={column.slider}
        value={column.value}
        targets={column.targets}
        onEdit={() => onEditTargets(column.slider)}
      />
    {/each}
  </div>
</div>
