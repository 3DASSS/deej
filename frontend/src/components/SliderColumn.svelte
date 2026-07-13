<script lang="ts">
  import { m } from "../paraglide/messages";
  import { targetLabel } from "../lib/targets";

  let {
    slider,
    value,
    targets,
    onEdit,
  }: { slider: number; value: number; targets: string[]; onEdit: () => void } = $props();

  const percent = $derived(Math.round(value * 100));
</script>

<div class="flex w-18 shrink-0 flex-col items-center gap-2.5">
  <span class="text-sm font-semibold tabular-nums">{percent}%</span>

  <div
    class="relative w-2 flex-1 overflow-hidden rounded-full bg-track"
    role="meter"
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent}
    aria-label="{m.slider()} {slider}"
  >
    <div
      class="absolute right-0 bottom-0 left-0 rounded-full bg-accent transition-[height] duration-100 ease-linear"
      style:height="{percent}%"
    ></div>
  </div>

  <button
    type="button"
    class="max-w-full cursor-pointer rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:border-accent {targets.length
      ? 'border-edge bg-chip'
      : 'border-dashed border-edge text-muted'}"
    title={targets.join(", ") || m.unmapped()}
    onclick={onEdit}
  >
    <span class="block truncate">
      {#if targets.length === 0}
        {m.unmapped()}
      {:else if targets.length === 1}
        {targetLabel(targets[0])}
      {:else}
        {targetLabel(targets[0])} +{targets.length - 1}
      {/if}
    </span>
  </button>
</div>
