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

  // controlled so the tooltip can be dismissed when the click opens the dialog
  let tooltipOpen = $state(false);
</script>

<div class="flex w-28 shrink-0 flex-col items-center gap-4">
  <span class="text-sm font-semibold tabular-nums">{percent}%</span>

  <div
    class="relative h-52 w-7 shrink-0"
    role="meter"
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent}
    aria-label="{m.slider()} {slider}"
  >
    <div class="slider-track absolute inset-0 overflow-hidden rounded-full">
      <div class="slider-ticks absolute inset-x-1.5 inset-y-0 text-body/50"></div>
      <!-- never shorter than its width, so at 0% the fill is a dot resting at
           the bottom and its rounded top cap doubles as the handle -->
      <div
        class="slider-fill absolute right-0 bottom-0 left-0 overflow-hidden rounded-full transition-[height] duration-75 ease-linear"
        style:height="calc(1.75rem + {percent} / 100 * (100% - 1.75rem))"
      >
        <!-- full-track-height tick layer anchored to the bottom, so the ticks
             stay aligned with the unfilled zone while the fill clips them -->
        <div class="slider-ticks absolute inset-x-1.5 bottom-0 h-52 text-on-accent/50"></div>
        <!-- opaque cover hiding the ticks in the 28px rounded top cap -->
        <div class="slider-fill-cap absolute inset-x-0 top-0 h-6"></div>
        <!-- handle dot, centered in the top cap; grey tuned per theme so it
             reads on the black fill (light) and the white fill (dark) -->
        <div
          class="slider-handle absolute top-2 left-1/2 size-3 -translate-x-1/2 rounded-full"
        ></div>
      </div>
    </div>
  </div>

  <Tooltip.Provider delayDuration={300}>
    <Tooltip.Root bind:open={tooltipOpen} ignoreNonKeyboardFocus>
      <Tooltip.Trigger
        type="button"
        class="flex h-16 w-full flex-col items-center justify-center gap-0.5 px-3 py-1 text-xs font-normal {targets.length
          ? 'btn rounded-xl before:rounded-[calc(0.75rem-1px)]'
          : 'rounded-xl border border-dashed border-edge text-muted transition-colors hover:bg-chip'}"
        onclick={() => {
          tooltipOpen = false;
          onEdit();
        }}
      >
        {#if targets.length === 0}
          <span class="wrap-break-word">{m.unmapped()}</span>
        {:else}
          <span class="min-w-0 w-full max-w-full line-clamp-2 wrap-break-word">{targetLabel(targets[0])}</span>
          {#if targets.length > 1}
            <span class="text-muted">+{targets.length - 1}</span>
          {/if}
        {/if}
      </Tooltip.Trigger>
      {#if targets.length > 0}
        <Tooltip.Portal>
          <Tooltip.Content
            side="bottom"
            sideOffset={6}
            class="anim-popover z-50 w-max max-w-[calc(100vw-1rem)] rounded-md border border-edge bg-surface px-2.5 py-1.5 text-xs shadow-lg"
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
