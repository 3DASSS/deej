<script lang="ts">
  import { onMount } from "svelte";
  import { AppInfoDTO, Settings, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import FieldSelect from "./ui/FieldSelect.svelte";

  let { settings, appInfo }: { settings: Settings; appInfo: AppInfoDTO | null } = $props();

  const languageItems = $derived([
    { value: "auto", label: m.languageAuto() },
    { value: "en", label: "English" },
    { value: "ru", label: "Русский" },
  ]);

  // autostart lives in the OS (registry), not in the config file, so it's
  // read on mount and applied immediately on toggle rather than on save
  let autostart = $state(false);
  let autostartError = $state("");

  onMount(async () => {
    if (!appInfo?.autostartAvailable) return;
    try {
      autostart = await SettingsService.GetAutostart();
    } catch (err) {
      console.error("failed to read autostart state", err);
    }
  });

  async function applyAutostart(checked: boolean) {
    autostartError = "";
    try {
      await SettingsService.SetAutostart(checked);
    } catch (err) {
      autostart = !checked;
      autostartError = `${err}`;
    }
  }
</script>

<section class="flex flex-col gap-4">
  <div class="flex max-w-xs flex-col gap-1">
    <label class="label" for="language">{m.language()}</label>
    <FieldSelect id="language" ariaLabel={m.language()} bind:value={settings.language} items={languageItems} />
    <div class="hint">{m.languageHint()}</div>
  </div>

  {#if appInfo?.autostartAvailable}
    <div class="flex flex-col gap-1">
      <FieldCheckbox
        id="autostart"
        bind:checked={autostart}
        onCheckedChange={(checked) => void applyAutostart(checked)}
        label={m.autostart()}
      />
      {#if autostartError}
        <div class="text-xs text-danger">{autostartError}</div>
      {:else}
        <div class="hint">{m.autostartHint()}</div>
      {/if}
    </div>
  {/if}
</section>
