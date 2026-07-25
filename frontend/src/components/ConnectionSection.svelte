<script lang="ts">
  import { onMount } from "svelte";
  import RefreshCw from "@lucide/svelte/icons/refresh-cw";
  import { COMSettings, SerialPortDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCombobox from "./ui/FieldCombobox.svelte";
  import SectionActions from "./ui/SectionActions.svelte";

  // the draft is owned by the settings dialog (this tab gets unmounted when
  // another one is shown): writing these settings makes deej reconnect, so a
  // half-typed port must never reach the config file
  let {
    draft = $bindable(),
    dirty,
    busy,
    onsave,
    onrevert,
  }: {
    draft: COMSettings;
    dirty: boolean;
    busy: boolean;
    onsave: () => void;
    onrevert: () => void;
  } = $props();

  let ports: SerialPortDTO[] = $state([]);

  onMount(refreshPorts);

  async function refreshPorts() {
    try {
      ports = await SettingsService.ListSerialPorts();
    } catch (err) {
      console.error("failed to list serial ports", err);
    }
  }

  const portItems = $derived([
    { value: "auto", hint: m.comPortAuto() },
    ...ports.map((port) => ({
      value: port.name,
      hint: port.product || (port.isUsb ? `USB ${port.vid}:${port.pid}` : ""),
    })),
  ]);

  const baudItems = [
    { value: "9600" },
    { value: "19200" },
    { value: "38400" },
    { value: "57600" },
    { value: "115200" },
  ];
</script>

<!-- a real form, so the browser runs the field constraints below before the
     save reaches the backend and comes back as an untranslated error string -->
<form
  onsubmit={(event) => {
    event.preventDefault();
    onsave();
  }}
>
  <div class="flex flex-col gap-1">
    <label class="label" for="com-port">{m.comPort()}</label>
    <div class="flex gap-1.5">
      <div class="flex-1">
        <FieldCombobox id="com-port" bind:value={draft.port} items={portItems} required />
      </div>
      <button class="btn px-2.5" type="button" onclick={refreshPorts} title={m.refreshPorts()} aria-label={m.refreshPorts()}>
        <RefreshCw size={14} />
      </button>
    </div>
  </div>

  <div class="mt-3 flex flex-col gap-1">
    <label class="label" for="baud-rate">{m.baudRate()}</label>
    <FieldCombobox
      id="baud-rate"
      items={baudItems}
      required
      pattern={"[0-9]*[1-9][0-9]*"}
      bind:value={() => String(draft.baudRate || ""), (v) => (draft.baudRate = parseInt(v, 10) || 0)}
    />
  </div>

  {#if draft.port === "auto"}
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-40 flex-1 flex-col gap-1">
        <label class="label" for="com-vid">{m.vid()}</label>
        <input
          id="com-vid"
          type="text"
          class="input"
          pattern={"\\s*(0[xX])?[0-9A-Fa-f]{1,4}\\s*"}
          title={m.vidPidHint()}
          bind:value={draft.vid}
        />
      </div>
      <div class="flex min-w-40 flex-1 flex-col gap-1">
        <label class="label" for="com-pid">{m.pid()}</label>
        <input
          id="com-pid"
          type="text"
          class="input"
          pattern={"\\s*(0[xX])?[0-9A-Fa-f]{1,4}\\s*"}
          title={m.vidPidHint()}
          bind:value={draft.pid}
        />
      </div>
    </div>
    <div class="hint mt-2">{m.vidPidHint()}</div>
  {/if}

  <SectionActions {dirty} {busy} {onrevert} />
</form>
