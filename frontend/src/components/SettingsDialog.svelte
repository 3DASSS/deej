<script lang="ts">
  import { onDestroy, untrack } from "svelte";
  import { prefersReducedMotion } from "svelte/motion";
  import { fly } from "svelte/transition";
  import { Dialog, Tabs } from "bits-ui";
  import X from "@lucide/svelte/icons/x";
  import Cable from "@lucide/svelte/icons/cable";
  import Cog from "@lucide/svelte/icons/cog";
  import Info from "@lucide/svelte/icons/info";
  import Layers from "@lucide/svelte/icons/layers";
  import MessageCircle from "@lucide/svelte/icons/message-circle";
  import SlidersHorizontal from "@lucide/svelte/icons/sliders-horizontal";
  import {
    AppInfoDTO,
    COMSettings,
    DiscordSettings,
    OBSSettings,
    Settings,
    SettingsService,
  } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { app, refreshSettings } from "../lib/state.svelte";
  import { m } from "../paraglide/messages";
  import AboutSection from "./AboutSection.svelte";
  import ConnectionSection from "./ConnectionSection.svelte";
  import BehaviorSection from "./BehaviorSection.svelte";
  import DiscordSection from "./DiscordSection.svelte";
  import GeneralSection from "./GeneralSection.svelte";
  import ObsSection from "./ObsSection.svelte";
  import ProfilesSection from "./ProfilesSection.svelte";
  import SimpleIcon from "./ui/SimpleIcon.svelte";

  let tab = $state("general");

  let {
    open = $bindable(false),
    initialTab = "general",
    appInfo,
  }: { open?: boolean; initialTab?: string; appInfo: AppInfoDTO | null } = $props();

  // saves are whole-document writes, so they're always rebased on the live
  // config rather than on a snapshot taken when the dialog opened - otherwise
  // a hand edit (or a slider mapping change) landing meanwhile gets clobbered
  const settings = $derived(app.settings);
  const ready = $derived(settings !== null);

  // connection, obs and discord settings make deej reconnect, so they're
  // edited as drafts and committed explicitly. the drafts live here rather
  // than in the sections because bits-ui unmounts inactive tabs, which would
  // otherwise discard in-progress edits on a tab switch
  let comDraft: COMSettings | null = $state(null);
  let obsDraft: OBSSettings | null = $state(null);
  let discordDraft: DiscordSettings | null = $state(null);

  let statusText = $state("");
  let statusKind: "ok" | "error" = $state("ok");
  let saving = $state(false);
  let statusTimer: ReturnType<typeof setTimeout>;

  const comDirty = $derived(
    !!settings && !!comDraft && JSON.stringify(comDraft) !== JSON.stringify(settings.com),
  );
  const obsDirty = $derived(
    !!settings && !!obsDraft && JSON.stringify(obsDraft) !== JSON.stringify(settings.obs),
  );
  const discordDirty = $derived(
    !!settings && !!discordDraft && JSON.stringify(discordDraft) !== JSON.stringify(settings.discord),
  );

  const tabItems = $derived([
    { value: "general", label: m.general(), Icon: Cog, dirty: false },
    { value: "profiles", label: m.profiles(), Icon: Layers, dirty: false },
    { value: "connection", label: m.connection(), Icon: Cable, dirty: comDirty },
    { value: "behavior", label: m.behavior(), Icon: SlidersHorizontal, dirty: false },
    { value: "obs", label: m.obs(), Icon: null, dirty: obsDirty },
    { value: "discord", label: m.discord(), Icon: null, dirty: discordDirty },
    { value: "about", label: m.about(), Icon: Info, dirty: false },
  ]);

  // re-arm the drafts every time the dialog opens; later config refreshes
  // deliberately leave them alone so they don't overwrite pending edits
  $effect(() => {
    if (!open) return;

    if (!ready) {
      void refreshSettings();
      return;
    }

    untrack(() => {
      tab = initialTab;
      statusText = "";
      comDraft = $state.snapshot(settings!.com);
      obsDraft = $state.snapshot(settings!.obs);
      discordDraft = $state.snapshot(settings!.discord);
    });
  });

  onDestroy(() => clearTimeout(statusTimer));

  function showStatus(text: string, kind: "ok" | "error") {
    statusText = text;
    statusKind = kind;
    clearTimeout(statusTimer);
    // errors stay put: the dialog can be closed on one, losing the edit
    if (kind === "ok") {
      statusTimer = setTimeout(() => {
        statusText = "";
      }, 6000);
    }
  }

  // patch the given fields onto the current config and write the result. A
  // rejected save leaves the section's draft untouched, so the bad value stays
  // on screen (and dirty) instead of being silently dropped or retried
  async function save(patch: Partial<Settings>) {
    if (!settings) return;

    saving = true;
    try {
      await SettingsService.SaveSettings(Object.assign($state.snapshot(settings), patch));
      showStatus(m.saved(), "ok");
    } catch (err) {
      showStatus(`${m.saveError()}: ${err}`, "error");
    } finally {
      saving = false;
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

      {#if settings && comDraft && obsDraft && discordDraft}
        <Tabs.Root bind:value={tab} orientation="vertical" class="flex min-h-0 flex-1">
          <Tabs.List class="flex w-56 shrink-0 flex-col gap-1 p-2">
            {#each tabItems as tabItem (tabItem.value)}
              <Tabs.Trigger
                value={tabItem.value}
                class="flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-muted hover:bg-chip hover:text-body data-[state=active]:bg-chip data-[state=active]:text-body"
              >
                {#if tabItem.value === "obs"}
                  <SimpleIcon name="obs" size={15} />
                {:else if tabItem.value === "discord"}
                  <SimpleIcon name="discord" size={15} />
                {:else}
                  <tabItem.Icon size={15} class="shrink-0" />
                {/if}
                {tabItem.label}
                {#if tabItem.dirty}
                  <span
                    class="ml-auto size-1.5 shrink-0 rounded-full bg-accent"
                    title={m.unsavedChanges()}
                    aria-label={m.unsavedChanges()}
                  ></span>
                {/if}
              </Tabs.Trigger>
            {/each}
          </Tabs.List>

          <div class="min-h-0  flex-1 overflow-y-auto p-4">
            <Tabs.Content value="general">
              <GeneralSection {settings} {appInfo} onsave={save} />
            </Tabs.Content>
            <Tabs.Content value="profiles">
              <ProfilesSection {settings} onsave={save} />
            </Tabs.Content>
            <Tabs.Content value="connection">
              <ConnectionSection
                bind:draft={comDraft}
                dirty={comDirty}
                busy={saving}
                onsave={() => save({ com: $state.snapshot(comDraft!) })}
                onrevert={() => (comDraft = $state.snapshot(settings!.com))}
              />
            </Tabs.Content>
            <Tabs.Content value="behavior"><BehaviorSection {settings} onsave={save} /></Tabs.Content>
            <Tabs.Content value="obs">
              <ObsSection
                bind:draft={obsDraft}
                dirty={obsDirty}
                busy={saving}
                onsave={() => save({ obs: $state.snapshot(obsDraft!) })}
                onrevert={() => (obsDraft = $state.snapshot(settings!.obs))}
              />
            </Tabs.Content>
            <Tabs.Content value="discord">
              <DiscordSection
                bind:draft={discordDraft}
                dirty={discordDirty}
                busy={saving}
                onsave={() => save({ discord: $state.snapshot(discordDraft!) })}
                onrevert={() => (discordDraft = $state.snapshot(settings!.discord))}
              />
            </Tabs.Content>
            <Tabs.Content value="about"><AboutSection {appInfo} /></Tabs.Content>
          </div>
        </Tabs.Root>
      {/if}

      {#if statusText}
        <div
          class="shrink-0 px-4 py-2.5 text-[13px] {statusKind === 'ok' ? 'text-success' : 'text-danger'}"
          transition:fly={{ y: 6, duration: prefersReducedMotion.current ? 0 : 400 }}
        >
          {statusText}
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
