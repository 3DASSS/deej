<script lang="ts">
  import { Checkbox } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";

  let {
    checked = $bindable(false),
    id,
    label,
    onCheckedChange,
  }: {
    checked?: boolean;
    id: string;
    label: string;
    onCheckedChange?: (checked: boolean) => void;
  } = $props();
</script>

<div class="flex items-center gap-2">
  <Checkbox.Root
    {id}
    bind:checked
    {onCheckedChange}
    class="flex size-4 shrink-0 items-center justify-center rounded border border-edge bg-field transition-colors data-[state=checked]:border-accent data-[state=checked]:bg-accent"
  >
    {#snippet children({ checked: isChecked })}
      {#if isChecked}
        <Check size={12} class="text-on-accent" />
      {/if}
    {/snippet}
  </Checkbox.Root>
  <label class="text-sm" for={id}>{label}</label>
</div>
