<script lang="ts">
  import { Combobox } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import ChevronsUpDown from "@lucide/svelte/icons/chevrons-up-down";

  type Item = { value: string; hint?: string };

  // a single-select combobox that also accepts free text: whatever is typed
  // becomes the bound value, suggestions just fill it in faster
  let {
    value = $bindable(""),
    items,
    id,
    placeholder = "",
  }: { value?: string; items: Item[]; id?: string; placeholder?: string } = $props();

  let search = $state("");
  let inputEl: HTMLInputElement | null = $state(null);

  const filtered = $derived.by(() => {
    const query = search.trim().toLowerCase();
    return query === ""
      ? items
      : items.filter(
          (item) => item.value.toLowerCase().includes(query) || (item.hint ?? "").toLowerCase().includes(query),
        );
  });

  // reflect the bound value into the input when it changes from outside
  // (initial load, revert) while the field isn't being edited
  $effect(() => {
    const current = value;
    if (inputEl && inputEl.value !== current && document.activeElement !== inputEl) {
      inputEl.value = current;
    }
  });
</script>

<Combobox.Root
  type="single"
  value={value}
  onValueChange={(selected) => {
    value = selected;
    search = "";
  }}
  onOpenChange={(open) => {
    if (open) search = "";
  }}
>
  <div class="relative">
    <Combobox.Input
      bind:ref={inputEl}
      {id}
      class="input pr-8"
      {placeholder}
      oninput={(e) => {
        value = e.currentTarget.value;
        search = e.currentTarget.value;
      }}
    />
    <Combobox.Trigger class="absolute top-1/2 right-2 -translate-y-1/2 text-muted" tabindex={-1}>
      <ChevronsUpDown size={14} />
    </Combobox.Trigger>
  </div>
  <Combobox.Portal>
    <Combobox.Content
      class="anim-popover z-60 max-h-56 w-(--bits-combobox-anchor-width) overflow-y-auto rounded-md border border-edge bg-card p-1 shadow-lg"
      sideOffset={4}
    >
      {#each filtered as item (item.value)}
        <Combobox.Item
          value={item.value}
          label={item.value}
          class="flex items-center justify-between gap-2 rounded px-2 py-1.5 text-sm data-highlighted:bg-chip"
        >
          {#snippet children({ selected })}
            <span class="truncate">{item.value}</span>
            <span class="flex shrink-0 items-center gap-1.5">
              {#if item.hint}
                <span class="hint">{item.hint}</span>
              {/if}
              {#if selected}
                <Check size={14} class="text-accent" />
              {/if}
            </span>
          {/snippet}
        </Combobox.Item>
      {/each}
    </Combobox.Content>
  </Combobox.Portal>
</Combobox.Root>
