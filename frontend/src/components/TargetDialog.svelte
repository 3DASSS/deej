<script lang="ts">
  import { Dialog, Tabs } from "bits-ui";
  import AppWindow from "@lucide/svelte/icons/app-window";
  import Check from "@lucide/svelte/icons/check";
  import Plus from "@lucide/svelte/icons/plus";
  import Sparkles from "@lucide/svelte/icons/sparkles";
  import Speaker from "@lucide/svelte/icons/speaker";
  import Video from "@lucide/svelte/icons/video";
  import X from "@lucide/svelte/icons/x";
  import { AppInfoDTO, SettingsDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { app, refreshSessions } from "../lib/state.svelte";
  import { m } from "../paraglide/messages";
  import { OBS_PREFIX, prettifyProcessName, specialTargetDescription, specialTargetLabel, targetLabel } from "../lib/targets";

  let {
    open = $bindable(false),
    slider,
    appInfo,
  }: { open?: boolean; slider: number; appInfo: AppInfoDTO | null } = $props();

  let targets: string[] = $state([]);
  let tab = $state("apps");
  let appSearch = $state("");
  let obsInputName = $state("");
  let obsInputs: string[] = $state([]);
  let obsError = $state(false);
  let saving = $state(false);
  let errorText = $state("");

  // covered by the "special" tab, so hidden from the apps list
  const specialSessionKeys = ["master", "system", "mic"];

  // take a fresh copy of the slider's targets every time the dialog opens
  $effect(() => {
    if (open) {
      targets = [...(app.settings?.sliderMapping.find((entry) => entry.slider === slider)?.targets ?? [])];
      tab = "apps";
      appSearch = "";
      obsInputName = "";
      errorText = "";
      void refreshSessions();
    }
  });

  // fetch OBS inputs lazily, whenever the OBS tab is shown
  $effect(() => {
    if (open && tab === "obs" && app.settings?.obsEnabled) {
      void loadObsInputs();
    }
  });

  async function loadObsInputs() {
    obsError = false;
    try {
      obsInputs = (await SettingsService.GetOBSInputs()) ?? [];
    } catch {
      obsError = true;
      obsInputs = [];
    }
  }

  const specialTargets = $derived(appInfo?.specialTargets ?? []);

  const appItems = $derived.by(() => {
    const query = appSearch.trim().toLowerCase();
    return app.sessions
      .filter((session) => !session.isDevice && !specialSessionKeys.includes(session.key))
      .filter(
        (session) =>
          query === "" ||
          session.key.includes(query) ||
          (session.displayName ?? "").toLowerCase().includes(query),
      );
  });

  const deviceItems = $derived(app.sessions.filter((session) => session.isDevice));

  // arbitrary process names are allowed - offer to add the typed text when it
  // doesn't exactly match a running session
  const freeText = $derived.by(() => {
    const value = appSearch.trim();
    if (value === "") return "";
    return app.sessions.some((session) => session.key === value.toLowerCase()) ? "" : value;
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

  function addObsInput() {
    const value = obsInputName.trim();
    if (value === "") return;
    const target = OBS_PREFIX + value;
    if (!isSelected(target)) {
      targets = [...targets, target];
    }
    obsInputName = "";
  }

  async function save() {
    if (!app.settings) return;
    saving = true;
    errorText = "";
    try {
      const dto: SettingsDTO = JSON.parse(JSON.stringify(app.settings));
      dto.sliderMapping = dto.sliderMapping.filter((entry) => entry.slider !== slider);
      if (targets.length > 0) {
        dto.sliderMapping.push({ slider, targets: [...targets] });
        dto.sliderMapping.sort((a, b) => a.slider - b.slider);
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

{#snippet itemRow(target: string, label: string, hint: string)}
  <button
    type="button"
    class="flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors hover:bg-chip"
    onclick={() => toggle(target)}
  >
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
    <Dialog.Overlay class="fixed inset-0 z-40 bg-black/40" />
    <Dialog.Content
      class="fixed top-1/2 left-1/2 z-50 flex max-h-[88dvh] w-[min(480px,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-edge bg-surface shadow-2xl"
    >
      <div class="flex shrink-0 items-center justify-between border-b border-edge px-4 py-2.5">
        <Dialog.Title class="text-sm font-semibold">{m.targetsFor()} {slider}</Dialog.Title>
        <Dialog.Close
          class="cursor-pointer rounded p-1 text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={m.close()}
        >
          <X size={15} />
        </Dialog.Close>
      </div>

      <div class="flex min-h-0 flex-1 flex-col gap-3 p-4">
        <div class="flex max-h-24 shrink-0 flex-wrap gap-1.5 overflow-y-auto">
          {#each targets as target (target)}
            <span
              class="inline-flex items-center gap-1 rounded-full border border-edge bg-chip py-0.5 pr-1.5 pl-2.5 text-xs"
              title={target}
            >
              {targetLabel(target)}
              <button
                type="button"
                class="cursor-pointer rounded-full p-0.5 text-muted hover:text-danger"
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
            {#each [{ value: "apps", label: m.tabApps(), Icon: AppWindow }, { value: "devices", label: m.tabDevices(), Icon: Speaker }, { value: "special", label: m.tabSpecial(), Icon: Sparkles }, { value: "obs", label: m.tabObs(), Icon: Video }] as tabItem (tabItem.value)}
              <Tabs.Trigger
                value={tabItem.value}
                class="-mb-px flex cursor-pointer items-center gap-1.5 border-b-2 border-transparent px-2.5 py-1.5 text-sm text-muted transition-colors hover:text-body data-[state=active]:border-accent data-[state=active]:text-body"
              >
                <tabItem.Icon size={14} />
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
            <div class="h-44 overflow-y-auto">
              {#if freeText !== ""}
                <button
                  type="button"
                  class="flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-accent transition-colors hover:bg-chip"
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
                )}
              {:else}
                {#if freeText === ""}
                  <div class="hint px-2 py-1.5">{m.noSessions()}</div>
                {/if}
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="devices" class="min-h-0 flex-1 pt-3">
            <div class="h-[13.25rem] overflow-y-auto">
              {#each deviceItems as session (session.key)}
                {@render itemRow(session.key, session.displayName || session.key, "")}
              {:else}
                <div class="hint px-2 py-1.5">{m.noDevices()}</div>
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="special" class="min-h-0 flex-1 pt-3">
            <div class="h-[13.25rem] overflow-y-auto">
              {#each specialTargets as target (target)}
                {@render itemRow(target, specialTargetLabel(target) ?? target, specialTargetDescription(target) ?? "")}
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="obs" class="flex min-h-0 flex-1 flex-col gap-2 pt-3">
            {#if !app.settings?.obsEnabled}
              <div class="hint py-1.5">{m.obsDisabled()}</div>
            {:else}
              <div class="flex shrink-0 gap-2">
                <input
                  type="text"
                  class="input flex-1"
                  placeholder={m.obsInputPlaceholder()}
                  bind:value={obsInputName}
                  onkeydown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      addObsInput();
                    }
                  }}
                />
                <button
                  type="button"
                  class="btn shrink-0"
                  onclick={addObsInput}
                  disabled={obsInputName.trim() === ""}
                  aria-label={m.addTarget()}
                >
                  <Plus size={14} />
                </button>
              </div>
              <div class="h-44 overflow-y-auto">
                {#if obsError}
                  <div class="hint px-2 py-1.5">{m.obsNotConnected()}</div>
                {:else}
                  {#each obsInputs as inputName (inputName)}
                    {@render itemRow(OBS_PREFIX + inputName, inputName, "")}
                  {:else}
                    <div class="hint px-2 py-1.5">{m.noObsInputs()}</div>
                  {/each}
                {/if}
              </div>
            {/if}
          </Tabs.Content>
        </Tabs.Root>
      </div>

      <div class="flex shrink-0 items-center gap-2 border-t border-edge px-4 py-2.5">
        <button class="btn btn-primary" onclick={save} disabled={saving}>{m.ok()}</button>
        <button class="btn" onclick={() => (open = false)} disabled={saving}>{m.cancel()}</button>
        {#if errorText}
          <span class="ml-auto text-[13px] text-danger">{errorText}</span>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
