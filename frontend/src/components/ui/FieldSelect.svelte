<script lang="ts">
  import { Select } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";

  type Item = { value: string; label: string };

  let {
    value = $bindable(""),
    items,
    id,
    ariaLabel,
  }: { value?: string; items: Item[]; id?: string; ariaLabel?: string } = $props();

  const selectedLabel = $derived(items.find((item) => item.value === value)?.label ?? value);
</script>

<Select.Root type="single" bind:value {items}>
  <Select.Trigger {id} aria-label={ariaLabel} class="input flex items-center justify-between gap-2 text-left">
    <span class="truncate">{selectedLabel}</span>
    <ChevronDown size={14} class="shrink-0 text-muted" />
  </Select.Trigger>
  <Select.Portal>
    <Select.Content
      class="anim-popover z-60 max-h-56 w-(--bits-select-anchor-width) overflow-y-auto rounded-md border border-edge bg-card p-1 shadow-lg"
      sideOffset={4}
    >
      <Select.Viewport>
        {#each items as item (item.value)}
          <Select.Item
            value={item.value}
            label={item.label}
            class="flex items-center justify-between gap-2 rounded px-2 py-1.5 text-sm data-highlighted:bg-chip"
          >
            {#snippet children({ selected })}
              <span class="truncate">{item.label}</span>
              {#if selected}
                <Check size={14} class="shrink-0 text-accent" />
              {/if}
            {/snippet}
          </Select.Item>
        {/each}
      </Select.Viewport>
    </Select.Content>
  </Select.Portal>
</Select.Root>
