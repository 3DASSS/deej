<script lang="ts">
  import { onMount } from "svelte";
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
  <h2>{t("connection")}</h2>

  <div class="field-row">
    <div class="field">
      <label for="com-port">{t("comPort")}</label>
      <div style="display: flex; gap: 6px;">
        <input id="com-port" type="text" list="com-ports" bind:value={settings.comPort} />
        <button class="icon" type="button" onclick={refreshPorts} title={t("refreshPorts")}>&#x27F3;</button>
      </div>
      <datalist id="com-ports">
        <option value="auto">{t("comPortAuto")}</option>
        {#each ports as port (port.name)}
          <option value={port.name}>{port.product || (port.isUsb ? `USB ${port.vid}:${port.pid}` : "")}</option>
        {/each}
      </datalist>
    </div>

    <div class="field">
      <label for="baud-rate">{t("baudRate")}</label>
      <input id="baud-rate" type="number" min="1" list="baud-rates" bind:value={settings.baudRate} />
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
    <div class="field-row">
      <div class="field">
        <label for="com-vid">{t("vid")}</label>
        <input id="com-vid" type="text" bind:value={settings.comVid} />
      </div>
      <div class="field">
        <label for="com-pid">{t("pid")}</label>
        <input id="com-pid" type="text" bind:value={settings.comPid} />
      </div>
    </div>
    <div class="hint">{t("vidPidHint")}</div>
  {/if}
</section>
