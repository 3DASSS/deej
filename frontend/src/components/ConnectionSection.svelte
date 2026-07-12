<script lang="ts">
  import { onMount } from "svelte";
  import RefreshCw from "@lucide/svelte/icons/refresh-cw";
  import { SettingsDTO, SerialPortDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings }: { settings: SettingsDTO } = $props();

  let ports: SerialPortDTO[] = $state([]);

  onMount(refreshPorts);

  async function refreshPorts() {
    try {
      ports = await SettingsService.ListSerialPorts();
    } catch (err) {
      console.error("failed to list serial ports", err);
    }
  }
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("connection")}</h2>

  <div class="flex flex-wrap gap-3.5">
    <div class="flex min-w-40 flex-1 flex-col gap-1">
      <label class="label" for="com-port">{t("comPort")}</label>
      <div class="flex gap-1.5">
        <input id="com-port" type="text" list="com-ports" class="input" bind:value={settings.comPort} />
        <button class="btn px-2.5" type="button" onclick={refreshPorts} title={t("refreshPorts")} aria-label={t("refreshPorts")}>
          <RefreshCw size={14} />
        </button>
      </div>
      <datalist id="com-ports">
        <option value="auto">{t("comPortAuto")}</option>
        {#each ports as port (port.name)}
          <option value={port.name}>{port.product || (port.isUsb ? `USB ${port.vid}:${port.pid}` : "")}</option>
        {/each}
      </datalist>
    </div>

    <div class="flex min-w-40 flex-1 flex-col gap-1">
      <label class="label" for="baud-rate">{t("baudRate")}</label>
      <input id="baud-rate" type="number" min="1" list="baud-rates" class="input" bind:value={settings.baudRate} />
      <datalist id="baud-rates">
        <option value="9600"></option>
        <option value="19200"></option>
        <option value="38400"></option>
        <option value="57600"></option>
        <option value="115200"></option>
      </datalist>
    </div>
  </div>

  {#if settings.comPort === "auto"}
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-40 flex-1 flex-col gap-1">
        <label class="label" for="com-vid">{t("vid")}</label>
        <input id="com-vid" type="text" class="input" bind:value={settings.comVid} />
      </div>
      <div class="flex min-w-40 flex-1 flex-col gap-1">
        <label class="label" for="com-pid">{t("pid")}</label>
        <input id="com-pid" type="text" class="input" bind:value={settings.comPid} />
      </div>
    </div>
    <div class="hint mt-2">{t("vidPidHint")}</div>
  {/if}
</section>
