<script lang="ts">
  import { Combobox, Dialog } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import ChevronsUpDown from "@lucide/svelte/icons/chevrons-up-down";
  import X from "@lucide/svelte/icons/x";
  import { AppInfoDTO, SettingsDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { app } from "../lib/state.svelte";
  import { t } from "../lib/i18n";

  let {
    open = $bindable(false),
    slider,
    appInfo,
  }: { open?: boolean; slider: number; appInfo: AppInfoDTO | null } = $props();

  let targets: string[] = $state([]);
  let search = $state("");
  let sessionNames: string[] = $state([]);
  let saving = $state(false);
  let errorText = $state("");
  let searchInput: HTMLInputElement | null = $state(null);

  // take a fresh copy of the slider's targets every time the dialog opens
  $effect(() => {
    if (open) {
      targets = [...(app.settings?.sliderMapping.find((entry) => entry.slider === slider)?.targets ?? [])];
      search = "";
      errorText = "";
      SettingsService.GetSessionNames()
        .then((names) => (sessionNames = names))
        .catch((err) => console.error("failed to load session names", err));
    }
  });

  const suggestions = $derived.by(() => {
    const special = appInfo?.specialTargets ?? [];
    const all = [...special, ...sessionNames.filter((name) => !special.includes(name))];
    const query = search.trim().toLowerCase();
    return query === "" ? all : all.filter((name) => name.toLowerCase().includes(query));
  });

  function clearSearch() {
    search = "";
    if (searchInput) {
      searchInput.value = "";
    }
  }

  // commit free text that isn't in the suggestion list (arbitrary process names)
  function onInputKeydown(event: KeyboardEvent) {
    if (event.key !== "Enter") return;

    const value = search.trim();
    if (value !== "" && suggestions.length === 0) {
      event.preventDefault();
      if (!targets.includes(value)) {
        targets.push(value);
      }
      clearSearch();
    }
  }

  function removeTarget(target: string) {
    targets = targets.filter((existing) => existing !== target);
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
      errorText = `${t("saveError")}: ${err}`;
    } finally {
      saving = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-40 bg-black/40" />
    <Dialog.Content
      class="fixed top-1/2 left-1/2 z-50 flex max-h-[80dvh] w-[min(440px,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-edge bg-surface shadow-2xl"
    >
      <div class="flex shrink-0 items-center justify-between border-b border-edge px-4 py-2.5">
        <Dialog.Title class="text-sm font-semibold">{t("targetsFor")} {slider}</Dialog.Title>
        <Dialog.Close
          class="cursor-pointer rounded p-1 text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={t("close")}
        >
          <X size={15} />
        </Dialog.Close>
      </div>

      <div class="flex-1 space-y-3 overflow-y-auto p-4">
        {#if targets.length > 0}
          <div class="flex flex-wrap gap-1.5">
            {#each targets as target (target)}
              <span class="inline-flex items-center gap-1 rounded-full border border-edge bg-chip py-0.5 pr-1.5 pl-2.5 text-xs">
                {target}
                <button
                  type="button"
                  class="cursor-pointer rounded-full p-0.5 text-muted hover:text-danger"
                  title={t("removeTarget")}
                  aria-label={t("removeTarget")}
                  onclick={() => removeTarget(target)}
                >
                  <X size={12} />
                </button>
              </span>
            {/each}
          </div>
        {:else}
          <div class="hint italic">{t("noTargets")}</div>
        {/if}

        <Combobox.Root type="multiple" bind:value={targets}>
          <div class="relative">
            <Combobox.Input
              bind:ref={searchInput}
              class="input pr-8"
              placeholder={t("addTargetPlaceholder")}
              oninput={(e) => (search = e.currentTarget.value)}
              onkeydown={onInputKeydown}
            />
            <Combobox.Trigger
              class="absolute top-1/2 right-2 -translate-y-1/2 cursor-pointer text-muted"
              aria-label={t("addTarget")}
            >
              <ChevronsUpDown size={14} />
            </Combobox.Trigger>
          </div>
          <Combobox.Portal>
            <Combobox.Content
              class="z-60 max-h-56 w-(--bits-combobox-anchor-width) overflow-y-auto rounded-md border border-edge bg-card p-1 shadow-lg"
              sideOffset={4}
            >
              {#each suggestions as suggestion (suggestion)}
                <Combobox.Item
                  value={suggestion}
                  label={suggestion}
                  class="flex cursor-pointer items-center justify-between rounded px-2 py-1.5 text-sm data-highlighted:bg-chip"
                >
                  {#snippet children({ selected })}
                    <span class="truncate">{suggestion}</span>
                    {#if selected}
                      <Check size={14} class="shrink-0 text-accent" />
                    {/if}
                  {/snippet}
                </Combobox.Item>
              {:else}
                <div class="hint px-2 py-1.5">{t("pressEnterToAdd")}</div>
              {/each}
            </Combobox.Content>
          </Combobox.Portal>
        </Combobox.Root>
      </div>

      <div class="flex shrink-0 items-center gap-2 border-t border-edge px-4 py-2.5">
        <button class="btn btn-primary" onclick={save} disabled={saving}>{t("ok")}</button>
        <button class="btn" onclick={() => (open = false)} disabled={saving}>{t("cancel")}</button>
        {#if errorText}
          <span class="ml-auto text-[13px] text-danger">{errorText}</span>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
