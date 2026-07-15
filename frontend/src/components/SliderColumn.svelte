<script lang="ts">
  import { Tooltip } from "bits-ui";
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

<div class="flex w-28 shrink-0 flex-col items-center gap-3">
  <span class="text-sm font-semibold tabular-nums">{percent}%</span>

  <div
    class="relative h-52 w-2 shrink-0 overflow-hidden rounded-full bg-track"
    role="meter"
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent}
    aria-label="{m.slider()} {slider}"
  >
    <div
      class="absolute right-0 bottom-0 left-0 rounded-full bg-linear-to-b from-violet-500 to-indigo-600 transition-[height] duration-100 ease-linear"
      style:height="{percent}%"
    ></div>
  </div>

  <Tooltip.Provider delayDuration={300}>
    <Tooltip.Root>
      <Tooltip.Trigger
        type="button"
        class="flex w-full h-16 flex-col items-center justify-center gap-0.5 rounded-lg border px-3 py-1 text-xs transition-colors hover:border-accent {targets.length
          ? 'border-edge bg-chip'
          : 'border-dashed border-edge text-muted'}"
        onclick={onEdit}
      >
        {#if targets.length === 0}
          <span class="wrap-break-word">{m.unmapped()}</span>
        {:else}
          <span class="line-clamp-2 wrap-break-word">{targetLabel(targets[0])}</span>
          {#if targets.length > 1}
            <span class="text-muted">+{targets.length - 1}</span>
          {/if}
        {/if}
      </Tooltip.Trigger>
      {#if targets.length > 0}
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={6}
            class="anim-popover z-50 max-w-56 rounded-md border border-edge bg-surface px-2.5 py-1.5 text-xs shadow-lg"
          >
            <div class="flex flex-col gap-0.5">
              {#each targets as target (target)}
                <span class="wrap-break-word">{targetLabel(target)}</span>
              {/each}
            </div>
          </Tooltip.Content>
        </Tooltip.Portal>
      {/if}
    </Tooltip.Root>
  </Tooltip.Provider>
</div>
