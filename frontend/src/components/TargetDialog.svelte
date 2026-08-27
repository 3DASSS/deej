<script lang="ts">
  import { Events } from "@wailsio/runtime";
  import { Dialog, Tabs } from "bits-ui";
  import AppWindow from "@lucide/svelte/icons/app-window";
  import Check from "@lucide/svelte/icons/check";
  import Mic from "@lucide/svelte/icons/mic";
  import Plus from "@lucide/svelte/icons/plus";
  import Sparkles from "@lucide/svelte/icons/sparkles";
  import Speaker from "@lucide/svelte/icons/speaker";
  import User from "@lucide/svelte/icons/user";
  import Volume2 from "@lucide/svelte/icons/volume-2";
  import X from "@lucide/svelte/icons/x";
  import {
    AppInfoDTO,
    Settings,
    SettingsService,
  } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { app, refreshDiscordUsers, refreshSessions } from "../lib/state.svelte";
  import { activeMapping, activeProfile } from "../lib/profiles";
  import { m } from "../paraglide/messages";
  import {
    DISCORD_INPUT,
    DISCORD_OUTPUT,
    DISCORD_PREFIX,
    OBS_PREFIX,
    prettifyProcessName,
    specialTargetDescription,
    specialTargetLabel,
    targetLabel,
  } from "../lib/targets";
  import SimpleIcon from "./ui/SimpleIcon.svelte";

  let {
    open = $bindable(false),
    slider,
    appInfo,
    onOpenObsSettings,
    onOpenDiscordSettings,
  }: {
    open?: boolean;
    slider: number;
    appInfo: AppInfoDTO | null;
    onOpenObsSettings?: () => void;
    onOpenDiscordSettings?: () => void;
  } = $props();

  let targets: string[] = $state([]);
  let tab = $state("apps");
  let appSearch = $state("");
  let deviceSearch = $state("");
  let obsSearch = $state("");
  let obsInputs: string[] = $state([]);
  let obsError = $state(false);
  let discordSearch = $state("");
  let discordConnected = $state(false);
  let processes: string[] = $state([]);
  let icons: Record<string, string> = $state({});
  let saving = $state(false);
  let errorText = $state("");

  // names already sent to GetProcessIcons, to avoid re-requesting on every
  // sessions refresh
  const requestedIcons = new Set<string>();

  // covered by the "special" tab, so hidden from the apps list
  const specialSessionKeys = ["master", "system", "mic"];

  // sessions shown on the apps tab (devices and special targets have their
  // own tabs); also the set of sessions that get process icons
  const appSessions = $derived(
    app.sessions.filter((session) => !session.isDevice && !specialSessionKeys.includes(session.key)),
  );

  // take a fresh copy of the slider's targets every time the dialog opens
  $effect(() => {
    if (open) {
      targets = [...(activeMapping(app.settings).find((entry) => entry.slider === slider)?.targets ?? [])];
      tab = "apps";
      appSearch = "";
      deviceSearch = "";
      obsSearch = "";
      discordSearch = "";
      errorText = "";
      void refreshSessions();
      void loadProcesses();
    }
  });

  // fetch OBS inputs lazily, whenever the OBS tab is shown
  $effect(() => {
    if (open && tab === "obs" && app.settings?.obs.enabled) {
      void loadObsInputs();
    }
  });

  async function loadProcesses() {
    try {
      processes = (await SettingsService.GetProcesses()) ?? [];
    } catch {
      processes = [];
    }
  }

  // fetch icons
  $effect(() => {
    if (!open) return;
    void loadIcons([...appSessions.map((session) => session.key), ...processes]);
  });

  async function loadIcons(names: string[]) {
    const wanted = names.filter((name) => !requestedIcons.has(name));
    if (wanted.length === 0) return;
    wanted.forEach((name) => requestedIcons.add(name));
    try {
      const result = await SettingsService.GetProcessIcons(wanted);
      for (const [name, icon] of Object.entries(result ?? {})) {
        if (icon) icons[name] = icon;
      }
    } catch {
      // no icons
    }
  }

  async function loadObsInputs() {
    obsError = false;
    try {
      obsInputs = (await SettingsService.GetOBSInputs()) ?? [];
    } catch {
      obsError = true;
      obsInputs = [];
    }
  }

  $effect(() => {
    if (!open || tab !== "discord" || !app.settings?.discord.enabled) return;

    void loadDiscordUsers();
    const off = Events.On("deej:discord", () => void loadDiscordUsers());

    return () => off();
  });

  async function loadDiscordUsers() {
    try {
      const status = await SettingsService.GetDiscordStatus();
      discordConnected = status.connected;
      if (discordConnected) await refreshDiscordUsers();
    } catch {
      discordConnected = false;
      app.discordUsers = [];
    }
  }

  const specialTargets = $derived(appInfo?.specialTargets ?? []);

  const appItems = $derived.by(() => {
    const query = appSearch.trim().toLowerCase();
    return appSessions.filter(
      (session) =>
        query === "" ||
        session.key.includes(query) ||
        (session.displayName ?? "").toLowerCase().includes(query),
    );
  });

  // running processes without an audio session, offered as dimmed
  // suggestions - a mapping takes effect once the app starts playing audio
  const processItems = $derived.by(() => {
    const query = appSearch.trim().toLowerCase();
    const sessionKeys = new Set(app.sessions.map((session) => session.key));
    return processes
      .filter((name) => !sessionKeys.has(name) && !specialSessionKeys.includes(name))
      .filter((name) => query === "" || name.includes(query));
  });

  const deviceItems = $derived.by(() => {
    const query = deviceSearch.trim().toLowerCase();
    return app.sessions
      .filter((session) => session.isDevice)
      .filter(
        (session) =>
          query === "" ||
          session.key.includes(query) ||
          (session.displayName ?? "").toLowerCase().includes(query),
      );
  });

  const obsItems = $derived.by(() => {
    const query = obsSearch.trim().toLowerCase();
    return obsInputs.filter((inputName) => query === "" || inputName.toLowerCase().includes(query));
  });

  // OBS input names are arbitrary too - offer to add the typed text when it
  // doesn't exactly match a known input
  const obsFreeText = $derived.by(() => {
    const value = obsSearch.trim();
    if (value === "") return "";
    return obsInputs.includes(value) ? "" : value;
  });

  const discordItems = $derived.by(() => {
    const query = discordSearch.trim().toLowerCase();
    return app.discordUsers.filter((user) => query === "" || user.name.toLowerCase().includes(query));
  });

  // someone who isn't in the voice channel right now can still be mapped -
  // the name resolves once they join
  const discordFreeText = $derived.by(() => {
    const value = discordSearch.trim();
    if (value === "") return "";
    return app.discordUsers.some((user) => user.name === value) ? "" : value;
  });

  // arbitrary process names are allowed - offer to add the typed text when it
  // doesn't exactly match a running session or process
  const freeText = $derived.by(() => {
    const value = appSearch.trim();
    if (value === "") return "";
    const lower = value.toLowerCase();
    if (app.sessions.some((session) => session.key === lower)) return "";
    return processes.includes(lower) ? "" : value;
  });

  function isSelected(target: string): boolean {
    const lower = target.toLowerCase();
    return targets.some((existing) => existing.toLowerCase() === lower);
  }

  function toggle(target: string) {
    if (isSelected(target)) {
      removeTarget(target);
    } else {
      targets = [...targets, target];
    }
  }

  function removeTarget(target: string) {
    const lower = target.toLowerCase();
    targets = targets.filter((existing) => existing.toLowerCase() !== lower);
  }

  function addFreeText() {
    if (freeText === "") return;
    if (!isSelected(freeText)) {
      targets = [...targets, freeText];
    }
    appSearch = "";
  }

  function addObsFreeText() {
    if (obsFreeText === "") return;
    const target = OBS_PREFIX + obsFreeText;
    if (!isSelected(target)) {
      targets = [...targets, target];
    }
    obsSearch = "";
  }

  function addDiscordFreeText() {
    if (discordFreeText === "") return;
    const target = DISCORD_PREFIX + discordFreeText;
    if (!isSelected(target)) {
      targets = [...targets, target];
    }
    discordSearch = "";
  }

  async function save() {
    if (!app.settings) return;
    saving = true;
    errorText = "";
    try {
      const dto: Settings = $state.snapshot(app.settings);
      // targets belong to the active profile; the others are left alone
      const profile = activeProfile(dto);
      if (!profile) return;
      profile.sliderMapping = profile.sliderMapping.filter((entry) => entry.slider !== slider);
      if (targets.length > 0) {
        profile.sliderMapping.push({ slider, targets: [...targets] });
        profile.sliderMapping.sort((a, b) => a.slider - b.slider);
      }
      await SettingsService.SaveSettings(dto);
      // local state refreshes via the deej:config event round-trip
      open = false;
    } catch (err) {
      errorText = `${m.saveError()}: ${err}`;
    } finally {
      saving = false;
    }
  }
</script>

{#snippet obsSettingsLink()}
  <button
    type="button"
    class="cursor-pointer text-body underline underline-offset-2 hover:text-muted"
    onclick={onOpenObsSettings}
  >
    {m.openSettings()}
  </button>
{/snippet}

{#snippet discordSettingsLink()}
  <button
    type="button"
    class="cursor-pointer text-body underline underline-offset-2 hover:text-muted"
    onclick={onOpenDiscordSettings}
  >
    {m.openSettings()}
  </button>
{/snippet}

{#snippet itemRow(target: string, label: string, hint: string, dimmed: boolean = false, icon: string | typeof Speaker | undefined = undefined)}
  <button
    type="button"
    class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-chip {dimmed && !isSelected(target) ? 'opacity-60 hover:opacity-100' : ''}"
    onclick={() => toggle(target)}
  >
    {#if typeof icon === "string"}
      <img src={icon} alt="" class="size-4 shrink-0" draggable="false" />
    {:else if icon}
      {@const RowIcon = icon}
      <RowIcon size={16} class="shrink-0 text-muted" />
    {/if}
    <span class="flex min-w-0 flex-1 flex-col">
      <span class="truncate">{label}</span>
      {#if hint}
        <span class="hint truncate">{hint}</span>
      {/if}
    </span>
    {#if isSelected(target)}
      <Check size={14} class="shrink-0 text-accent" />
    {/if}
  </button>
{/snippet}

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="anim-overlay fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
    <Dialog.Content
      class="dialog anim-dialog fixed top-1/2 left-1/2 z-50 flex h-[min(600px,88dvh)] w-[min(620px,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col"
    >
      <div class="flex shrink-0 items-center justify-between border-b border-edge px-4 py-2.5">
        <Dialog.Title class="text-sm font-semibold">{m.targetsFor({ number: slider + 1 })}</Dialog.Title>
        <Dialog.Close
          class="rounded p-1 text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={m.close()}
        >
          <X size={15} />
        </Dialog.Close>
      </div>

      <div class="flex min-h-0 flex-1 flex-col gap-3 p-4">
        <div class="flex max-h-24 shrink-0 flex-wrap gap-1.5 overflow-y-auto">
          {#each targets as target (target)}
            <span
              class="inline-flex max-w-full items-center gap-1 rounded-full border border-edge bg-chip py-0.5 pr-1.5 pl-2.5 text-xs"
              title={target}
            >
              <span class="min-w-0 wrap-break-word">{targetLabel(target)}</span>
              <button
                type="button"
                class="rounded-full p-0.5 text-muted hover:text-danger"
                title={m.removeTarget()}
                aria-label={m.removeTarget()}
                onclick={() => removeTarget(target)}
              >
                <X size={12} />
              </button>
            </span>
          {:else}
            <span class="hint italic">{m.noTargets()}</span>
          {/each}
        </div>

        <Tabs.Root bind:value={tab} class="flex min-h-0 flex-1 flex-col">
          <Tabs.List class="flex shrink-0 gap-1 border-b border-edge">
            {#each [{ value: "apps", label: m.tabApps(), Icon: AppWindow }, { value: "devices", label: m.tabDevices(), Icon: Speaker }, { value: "special", label: m.tabSpecial(), Icon: Sparkles }, { value: "obs", label: m.tabObs(), Icon: null }, { value: "discord", label: m.tabDiscord(), Icon: null }] as tabItem (tabItem.value)}
              <Tabs.Trigger
                value={tabItem.value}
                class="-mb-px flex items-center gap-1.5 border-b-2 border-transparent px-2.5 py-1.5 text-sm text-muted hover:text-body data-[state=active]:border-accent data-[state=active]:text-body"
              >
                {#if tabItem.value === "obs"}
                  <SimpleIcon name="obs" size={14} />
                {:else if tabItem.value === "discord"}
                  <SimpleIcon name="discord" size={14} />
                {:else}
                  <tabItem.Icon size={14} />
                {/if}
                {tabItem.label}
              </Tabs.Trigger>
            {/each}
          </Tabs.List>

          <Tabs.Content value="apps" class="flex min-h-0 flex-1 flex-col gap-2 pt-3">
            <input
              type="text"
              class="input shrink-0"
              placeholder={m.searchApps()}
              bind:value={appSearch}
              onkeydown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  addFreeText();
                }
              }}
            />
            <div class="min-h-0 flex-1 overflow-y-auto">
              {#if freeText !== ""}
                <button
                  type="button"
                  class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-accent hover:bg-chip"
                  onclick={addFreeText}
                >
                  <Plus size={14} class="shrink-0" />
                  <span class="truncate">{m.addTarget()}: "{freeText}"</span>
                </button>
              {/if}
              {#each appItems as session (session.key)}
                {@render itemRow(
                  session.key,
                  session.displayName || prettifyProcessName(session.key),
                  session.displayName ? session.key : "",
                  false,
                  icons[session.key] ?? AppWindow,
                )}
              {:else}
                {#if freeText === "" && processItems.length === 0}
                  <div class="hint px-2 py-1.5">{m.noSessions()}</div>
                {/if}
              {/each}
              {#if processItems.length > 0}
                <div class="hint px-2 pt-3 pb-1 text-xs font-medium tracking-wide uppercase">
                  {m.otherProcesses()}
                </div>
                {#each processItems as name (name)}
                  {@render itemRow(
                    name,
                    prettifyProcessName(name),
                    name.toLowerCase().endsWith(".exe") ? name : "",
                    true,
                    icons[name] ?? AppWindow,
                  )}
                {/each}
              {/if}
            </div>
          </Tabs.Content>

          <Tabs.Content value="devices" class="flex min-h-0 flex-1 flex-col gap-2 pt-3">
            <input type="text" class="input shrink-0" placeholder={m.searchDevices()} bind:value={deviceSearch} />
            <div class="min-h-0 flex-1 overflow-y-auto">
              {#each deviceItems as session (session.key)}
                {@render itemRow(session.key, session.displayName || session.key, "", false, session.isInput ? Mic : Speaker)}
              {:else}
                <div class="hint px-2 py-1.5">{m.noDevices()}</div>
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="special" class="flex min-h-0 flex-1 flex-col pt-3">
            <div class="min-h-0 flex-1 overflow-y-auto">
              {#each specialTargets as target (target)}
                {@render itemRow(target, specialTargetLabel(target) ?? target, specialTargetDescription(target) ?? "")}
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="obs" class="flex min-h-0 flex-1 flex-col gap-2 pt-3">
            {#if !app.settings?.obs.enabled}
              <div class="hint py-1.5">
                {m.obsDisabled()}
                {@render obsSettingsLink()}
              </div>
            {:else}
              <input
                type="text"
                class="input shrink-0"
                placeholder={m.searchObsInputs()}
                bind:value={obsSearch}
                onkeydown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    addObsFreeText();
                  }
                }}
              />
              <div class="min-h-0 flex-1 overflow-y-auto">
                {#if obsFreeText !== ""}
                  <button
                    type="button"
                    class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-accent hover:bg-chip"
                    onclick={addObsFreeText}
                  >
                    <Plus size={14} class="shrink-0" />
                    <span class="truncate">{m.addTarget()}: "{obsFreeText}"</span>
                  </button>
                {/if}
                {#if obsError}
                  <div class="hint px-2 py-1.5">
                    {m.obsNotConnected()}
                    {@render obsSettingsLink()}
                  </div>
                {:else}
                  {#each obsItems as inputName (inputName)}
                    {@render itemRow(OBS_PREFIX + inputName, inputName, "")}
                  {:else}
                    {#if obsFreeText === ""}
                      <div class="hint px-2 py-1.5">{m.noObsInputs()}</div>
                    {/if}
                  {/each}
                {/if}
              </div>
            {/if}
          </Tabs.Content>

          <Tabs.Content value="discord" class="flex min-h-0 flex-1 flex-col gap-2 pt-3">
            {#if !app.settings?.discord.enabled}
              <div class="hint py-1.5">
                {m.discordDisabled()}
                {@render discordSettingsLink()}
              </div>
            {:else}
              <input
                type="text"
                class="input shrink-0"
                placeholder={m.searchDiscordUsers()}
                bind:value={discordSearch}
                onkeydown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    addDiscordFreeText();
                  }
                }}
              />
              <div class="min-h-0 flex-1 overflow-y-auto">
                {#if discordFreeText !== ""}
                  <button
                    type="button"
                    class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-accent hover:bg-chip"
                    onclick={addDiscordFreeText}
                  >
                    <Plus size={14} class="shrink-0" />
                    <span class="truncate">{m.addTarget()}: "{discordFreeText}"</span>
                  </button>
                {/if}

                <!-- the user's own levels don't depend on a voice channel, so
                     they're offered whether or not the roster loaded -->
                {@render itemRow(DISCORD_INPUT, specialTargetLabel(DISCORD_INPUT) ?? DISCORD_INPUT, specialTargetDescription(DISCORD_INPUT) ?? "", false, Mic)}
                {@render itemRow(DISCORD_OUTPUT, specialTargetLabel(DISCORD_OUTPUT) ?? DISCORD_OUTPUT, specialTargetDescription(DISCORD_OUTPUT) ?? "", false, Volume2)}

                {#if !discordConnected}
                  <div class="hint px-2 pt-3 pb-1.5">
                    {m.discordNotConnected()}
                    {@render discordSettingsLink()}
                  </div>
                {:else}
                  {#each discordItems as user (user.id)}
                    {@render itemRow(DISCORD_PREFIX + user.id, user.name, "", false, User)}
                  {:else}
                    {#if discordFreeText === ""}
                      <div class="hint px-2 pt-3 pb-1.5">{m.discordNoVoiceChannel()}</div>
                    {/if}
                  {/each}
                {/if}
              </div>
            {/if}
          </Tabs.Content>
        </Tabs.Root>
      </div>

      <div class="flex shrink-0 items-center justify-end gap-2 border-t border-edge px-4 py-2.5">
        {#if errorText}
          <span class="mr-auto text-[13px] text-danger">{errorText}</span>
        {/if}
        <button class="btn" onclick={() => (open = false)} disabled={saving}>{m.cancel()}</button>
        <button class="btn btn-primary" onclick={save} disabled={saving}>{m.save()}</button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
