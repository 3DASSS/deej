<script lang="ts">
  import { Dialog } from "bits-ui";
  import X from "@lucide/svelte/icons/x";
  import { AppInfoDTO, SettingsDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";
  import ConnectionSection from "./ConnectionSection.svelte";
  import BehaviorSection from "./BehaviorSection.svelte";
  import LanguageSection from "./LanguageSection.svelte";
  import ObsSection from "./ObsSection.svelte";

  let { open = $bindable(false), appInfo }: { open?: boolean; appInfo: AppInfoDTO | null } = $props();

  let settings: SettingsDTO | null = $state(null);
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
      loadSettings();
    }
  });

  async function loadSettings() {
    try {
      const loaded = await SettingsService.GetSettings();
      settings = JSON.parse(JSON.stringify(loaded));
      originalJson = JSON.stringify(settings);
    } catch (err) {
      showStatus(`${t("loadError")}: ${err}`, "error");
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
      showStatus(t("saved"), "ok");
    } catch (err) {
      showStatus(`${t("saveError")}: ${err}`, "error");
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
    <Dialog.Overlay class="fixed inset-0 z-40 bg-black/40" />
    <Dialog.Content
      class="fixed top-1/2 left-1/2 z-50 flex max-h-[88dvh] w-[min(560px,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-edge bg-surface shadow-2xl"
    >
      <div class="flex shrink-0 items-center justify-between border-b border-edge px-4 py-2.5">
        <Dialog.Title class="text-sm font-semibold">{t("settings")}</Dialog.Title>
        <Dialog.Close
          class="cursor-pointer rounded p-1 text-muted transition-colors hover:bg-chip hover:text-body"
          aria-label={t("close")}
        >
          <X size={15} />
        </Dialog.Close>
      </div>

      <div class="flex-1 space-y-3 overflow-y-auto p-4">
        {#if settings}
          <ConnectionSection {settings} />
          <BehaviorSection {settings} />
          <LanguageSection {settings} />
          <ObsSection {settings} />

          <div class="hint select-text">
            {#if appInfo?.version}{t("version")}: {appInfo.version} &middot;{/if}
            {t("configPath")}: {appInfo?.configPath}
          </div>
        {/if}
      </div>

      <div class="flex shrink-0 items-center gap-2 border-t border-edge px-4 py-2.5">
        <button class="btn btn-primary" onclick={save} disabled={!dirty || saving}>{t("save")}</button>
        <button class="btn" onclick={revert} disabled={!dirty || saving}>{t("revert")}</button>
        {#if statusText}
          <span class="ml-auto text-[13px] {statusKind === 'ok' ? 'text-success' : 'text-danger'}">{statusText}</span>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
