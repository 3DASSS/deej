<script lang="ts">
  import { onMount } from "svelte";
  import { Window } from "@wailsio/runtime";
  import { Select } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import ChevronDown from "@lucide/svelte/icons/chevron-down";
  import Layers from "@lucide/svelte/icons/layers";
  import Settings from "@lucide/svelte/icons/settings";
  import Minus from "@lucide/svelte/icons/minus";
  import Square from "@lucide/svelte/icons/square";
  import Copy from "@lucide/svelte/icons/copy";
  import X from "@lucide/svelte/icons/x";
  import { SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import { app } from "../lib/state.svelte";

  let { onOpenSettings, onOpenProfiles }: { onOpenSettings: () => void; onOpenProfiles: () => void } = $props();

  let maximised = $state(false);
  let profilesOpen = $state(false);

  const profiles = $derived(app.settings?.profiles ?? []);
  const profileItems = $derived(profiles.map((profile) => ({ value: profile.name, label: profile.name })));

  async function setProfile(name: string) {
    if (!name || name === app.settings?.activeProfile) return;
    try {
      await SettingsService.SetActiveProfile(name);
    } catch (err) {
      console.error("failed to switch profile", err);
    }
  }

  function manage() {
    profilesOpen = false;
    onOpenProfiles();
  }

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

  {#if profiles.length > 0}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="[--wails-draggable:no-drag]" ondblclick={(e) => e.stopPropagation()}>
      <Select.Root
        type="single"
        items={profileItems}
        bind:open={profilesOpen}
        value={app.settings?.activeProfile ?? ""}
        onValueChange={setProfile}
      >
        <Select.Trigger
          class="flex max-w-40 items-center gap-1.5 rounded px-2 py-1 text-xs text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={m.switchProfile()}
          title={m.switchProfile()}
        >
          <Layers size={13} class="shrink-0" />
          <span class="truncate">{app.settings?.activeProfile}</span>
          <ChevronDown size={13} class="shrink-0" />
        </Select.Trigger>
        <Select.Portal>
          <Select.Content
            class="anim-popover z-60 max-h-56 min-w-40 overflow-y-auto rounded-md border border-edge bg-card p-1 shadow-lg"
            align="start"
            sideOffset={12}
          >
            <Select.Viewport>
              {#each profiles as profile (profile.name)}
                <Select.Item
                  value={profile.name}
                  label={profile.name}
                  class="flex items-center justify-between gap-3 rounded px-2 py-1.5 text-sm data-highlighted:bg-chip"
                >
                  {#snippet children({ selected })}
                    <span class="min-w-0 truncate">{profile.name}</span>
                    <span class="flex shrink-0 items-center gap-2">
                      {#if profile.hotkey}
                        <span class="rounded border border-edge px-1.5 py-0.5 text-[10px] leading-none text-muted">
                          {profile.hotkey}
                        </span>
                      {/if}
                      {#if selected}
                        <Check size={14} class="text-accent" />
                      {/if}
                    </span>
                  {/snippet}
                </Select.Item>
              {/each}
            </Select.Viewport>

            <div class="my-1 border-t border-edge"></div>
            <button
              type="button"
              class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-muted transition-colors hover:bg-chip hover:text-body"
              onclick={manage}
            >
              <Settings size={14} class="shrink-0" />
              <span class="truncate">{m.manageProfiles()}</span>
            </button>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
    </div>
  {/if}

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
