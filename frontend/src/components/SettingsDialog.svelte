<script lang="ts">
  import { Dialog, Tabs } from "bits-ui";
  import X from "@lucide/svelte/icons/x";
  import Cable from "@lucide/svelte/icons/cable";
  import Cog from "@lucide/svelte/icons/cog";
  import SlidersHorizontal from "@lucide/svelte/icons/sliders-horizontal";
  import Video from "@lucide/svelte/icons/video";
  import { AppInfoDTO, Settings, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import ConnectionSection from "./ConnectionSection.svelte";
  import BehaviorSection from "./BehaviorSection.svelte";
  import GeneralSection from "./GeneralSection.svelte";
  import ObsSection from "./ObsSection.svelte";

  let tab = $state("general");

  let {
    open = $bindable(false),
    initialTab = "general",
    appInfo,
  }: { open?: boolean; initialTab?: string; appInfo: AppInfoDTO | null } = $props();

  let settings: Settings | null = $state(null);
  let originalJson = $state("");
  let statusText = $state("");
  let statusKind: "ok" | "error" = $state("ok");
  let saving = $state(false);
  let statusTimer: ReturnType<typeof setTimeout>;

  const dirty = $derived(settings !== null && JSON.stringify(settings) !== originalJson);

  // take a fresh copy every time the dialog opens, so live config refreshes
  // never clobber in-progress edits
  $effect(() => {
    if (open) {
      settings = null;
      statusText = "";
      tab = initialTab;
      loadSettings();
    }
  });

  async function loadSettings() {
    try {
      const loaded = await SettingsService.GetSettings();
      settings = JSON.parse(JSON.stringify(loaded));
      originalJson = JSON.stringify(settings);
    } catch (err) {
      showStatus(`${m.loadError()}: ${err}`, "error");
    }
  }

  function showStatus(text: string, kind: "ok" | "error") {
    statusText = text;
    statusKind = kind;
    clearTimeout(statusTimer);
    statusTimer = setTimeout(() => {
      statusText = "";
    }, 6000);
  }

  async function save() {
    if (!settings) return;
    saving = true;
    try {
      await SettingsService.SaveSettings(settings);
      originalJson = JSON.stringify(settings);
      showStatus(m.saved(), "ok");
    } catch (err) {
      showStatus(`${m.saveError()}: ${err}`, "error");
    } finally {
      saving = false;
    }
  }

  function revert() {
    if (originalJson) {
      settings = JSON.parse(originalJson);
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="anim-overlay fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
    <Dialog.Content
      class="dialog anim-dialog fixed top-1/2 left-1/2 z-50 flex h-[min(600px,88dvh)] w-[min(720px,94vw)] -translate-x-1/2 -translate-y-1/2 flex-col"
    >
      <div class="flex shrink-0 items-center justify-between border-b border-edge px-4 py-2.5">
        <Dialog.Title class="text-sm font-semibold">{m.settings()}</Dialog.Title>
        <Dialog.Close
          class="rounded p-1 text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={m.close()}
        >
          <X size={15} />
        </Dialog.Close>
      </div>

      {#if settings}
        <Tabs.Root bind:value={tab} orientation="vertical" class="flex min-h-0 flex-1">
          <Tabs.List class="flex w-56 shrink-0 flex-col gap-1 p-2">
            {#each [{ value: "general", label: m.general(), Icon: Cog }, { value: "connection", label: m.connection(), Icon: Cable }, { value: "behavior", label: m.behavior(), Icon: SlidersHorizontal }, { value: "obs", label: m.obs(), Icon: Video }] as tabItem (tabItem.value)}
              <Tabs.Trigger
                value={tabItem.value}
                class="flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-muted transition-colors hover:bg-chip hover:text-body data-[state=active]:bg-chip data-[state=active]:text-body"
              >
                <tabItem.Icon size={15} class="shrink-0" />
                {tabItem.label}
              </Tabs.Trigger>
            {/each}
          </Tabs.List>

          <div class="min-h-0  flex-1 overflow-y-auto p-4">
            <Tabs.Content value="general"><GeneralSection {settings} {appInfo} /></Tabs.Content>
            <Tabs.Content value="connection"><ConnectionSection {settings} /></Tabs.Content>
            <Tabs.Content value="behavior"><BehaviorSection {settings} /></Tabs.Content>
            <Tabs.Content value="obs"><ObsSection {settings} /></Tabs.Content>
          </div>
        </Tabs.Root>

        <div class="hint shrink-0 truncate border-t border-edge px-4 py-2 select-text">
          {#if appInfo?.version}{m.version()}: {appInfo.version} &middot;{/if}
          {m.configPath()}: {appInfo?.configPath}
        </div>
      {/if}

      <div class="flex shrink-0 items-center justify-end gap-2 px-4 py-2.5">
        {#if statusText}
          <span class="mr-auto text-[13px] {statusKind === 'ok' ? 'text-success' : 'text-danger'}">{statusText}</span>
        {/if}
        <button class="btn" onclick={revert} disabled={!dirty || saving}>{m.revert()}</button>
        <button class="btn btn-primary" onclick={save} disabled={!dirty || saving}>{m.save()}</button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
