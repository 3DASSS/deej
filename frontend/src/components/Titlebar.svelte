<script lang="ts">
  import { onMount } from "svelte";
  import { Window } from "@wailsio/runtime";
  import Settings from "@lucide/svelte/icons/settings";
  import Minus from "@lucide/svelte/icons/minus";
  import Square from "@lucide/svelte/icons/square";
  import Copy from "@lucide/svelte/icons/copy";
  import X from "@lucide/svelte/icons/x";
  import { m } from "../paraglide/messages";
  import { app } from "../lib/state.svelte";

  let { onOpenSettings }: { onOpenSettings: () => void } = $props();

  let maximised = $state(false);

  async function refreshMaximised() {
    try {
      maximised = await Window.IsMaximised();
    } catch {
      // runtime not available (e.g. plain browser during dev)
    }
  }

  onMount(() => {
    refreshMaximised();
    window.addEventListener("resize", refreshMaximised);
    return () => window.removeEventListener("resize", refreshMaximised);
  });

  async function toggleMaximise() {
    await Window.ToggleMaximise();
    await refreshMaximised();
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -- dblclick-to-maximize is a mouse-only shortcut; the maximize button is the accessible path -->
<header
  class="flex h-9 shrink-0 items-center gap-2 border-b border-edge bg-card pl-3 [--wails-draggable:drag]"
  ondblclick={toggleMaximise}
>
  <span class="text-[13px] font-semibold">deej</span>

  <span
    class="ml-auto flex items-center gap-1.5 text-xs text-muted"
    title={app.connected ? m.connected() : m.disconnected()}
  >
    <span
      class="size-1.5 rounded-full {app.connected ? 'bg-green-500' : 'animate-pulse border border-muted'}"
    ></span>
    {#if app.connected}
      {app.comPort || m.connected()}
    {:else}
      {m.disconnected()}
    {/if}
  </span>

  <div class="flex h-full items-stretch [--wails-draggable:no-drag]">
    <button
      type="button"
      class="flex w-11 items-center justify-center text-muted transition-colors hover:bg-chip hover:text-body"
      title={m.settings()}
      aria-label={m.settings()}
      onclick={onOpenSettings}
      ondblclick={(e) => e.stopPropagation()}
    >
      <Settings size={15} />
    </button>
    <button
      type="button"
      class="flex w-11 items-center justify-center text-muted transition-colors hover:bg-chip hover:text-body"
      title={m.minimize()}
      aria-label={m.minimize()}
      onclick={() => Window.Minimise()}
      ondblclick={(e) => e.stopPropagation()}
    >
      <Minus size={15} />
    </button>
    <button
      type="button"
      class="flex w-11 items-center justify-center text-muted transition-colors hover:bg-chip hover:text-body"
      title={maximised ? m.restore() : m.maximize()}
      aria-label={maximised ? m.restore() : m.maximize()}
      onclick={toggleMaximise}
      ondblclick={(e) => e.stopPropagation()}
    >
      {#if maximised}
        <Copy size={13} />
      {:else}
        <Square size={13} />
      {/if}
    </button>
    <button
      type="button"
      class="flex w-11 items-center justify-center text-muted transition-colors hover:bg-body hover:text-surface"
      title={m.close()}
      aria-label={m.close()}
      onclick={() => Window.Close()}
      ondblclick={(e) => e.stopPropagation()}
    >
      <X size={15} />
    </button>
  </div>
</header>
