<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("obs")}</h2>

  <div class="flex items-center gap-2">
    <input id="obs-enabled" type="checkbox" class="size-4 accent-accent" bind:checked={settings.obsEnabled} />
    <label class="text-sm" for="obs-enabled">{t("obsEnabled")}</label>
  </div>
  <div class="hint mt-2">{t("obsHint")}</div>

  {#if settings.obsEnabled}
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-host">{t("obsHost")}</label>
        <input id="obs-host" type="text" class="input" bind:value={settings.obsHost} />
      </div>
      <div class="flex min-w-24 flex-col gap-1">
        <label class="label" for="obs-port">{t("obsPort")}</label>
        <input id="obs-port" type="number" min="1" max="65535" class="input" bind:value={settings.obsPort} />
      </div>
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-password">{t("obsPassword")}</label>
        <input id="obs-password" type="password" class="input" bind:value={settings.obsPassword} />
      </div>
    </div>
  {/if}
</section>
