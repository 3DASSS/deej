<script lang="ts">
  import { onMount } from "svelte";
  import { AppInfoDTO, SettingsDTO, SettingsService } from "../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "./lib/i18n";
  import ConnectionSection from "./components/ConnectionSection.svelte";
  import BehaviorSection from "./components/BehaviorSection.svelte";
  import LanguageSection from "./components/LanguageSection.svelte";
  import ObsSection from "./components/ObsSection.svelte";
  import MappingEditor from "./components/MappingEditor.svelte";

  let { appInfo }: { appInfo: AppInfoDTO | null } = $props();

  let settings: SettingsDTO | null = $state(null);
  let originalJson = $state("");
  let statusText = $state("");
  let statusKind: "ok" | "error" = $state("ok");
  let saving = $state(false);
  let statusTimer: ReturnType<typeof setTimeout>;

  const dirty = $derived(settings !== null && JSON.stringify(settings) !== originalJson);

  onMount(loadSettings);

  async function loadSettings() {
    try {
      const loaded = await SettingsService.GetSettings();
      // clone into a plain object so svelte's deep reactivity applies
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

<h1>{t("title")}</h1>

{#if settings}
  <ConnectionSection {settings} />
  <BehaviorSection {settings} />
  <LanguageSection {settings} />
  <ObsSection {settings} />
  <MappingEditor {settings} {appInfo} />

  <div class="meta">
    {#if appInfo?.version}{t("version")}: {appInfo.version} &middot;{/if}
    {t("configPath")}: {appInfo?.configPath}
  </div>

  <div class="footer">
    <button class="primary" onclick={save} disabled={!dirty || saving}>{t("save")}</button>
    <button onclick={revert} disabled={!dirty || saving}>{t("revert")}</button>
    {#if statusText}
      <span class="status {statusKind}">{statusText}</span>
    {/if}
  </div>
{:else if statusText}
  <p class="status error">{statusText}</p>
{/if}
