<script lang="ts">
  import { OBSSettings } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import SectionActions from "./ui/SectionActions.svelte";

  // the draft is owned by the settings dialog (this tab gets unmounted when
  // another one is shown): writing these settings makes deej reconnect, so a
  // half-typed host or password must never reach the config file
  let {
    draft = $bindable(),
    dirty,
    busy,
    onsave,
    onrevert,
  }: {
    draft: OBSSettings;
    dirty: boolean;
    busy: boolean;
    onsave: () => void;
    onrevert: () => void;
  } = $props();
</script>

<!-- a real form, so the browser runs the field constraints below before the
     save reaches the backend and comes back as an untranslated error string -->
<form
  onsubmit={(event) => {
    event.preventDefault();
    onsave();
  }}
>
  <FieldCheckbox id="obs-enabled" bind:checked={draft.enabled} label={m.obsEnabled()} />
  <div class="hint mt-2">{m.obsHint()}</div>

  {#if draft.enabled}
    <div class="hint mt-3">{m.obsWebsocketHint()}</div>
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-host">{m.obsHost()}</label>
        <input id="obs-host" type="text" class="input" bind:value={draft.host} />
      </div>
      <div class="flex min-w-24 flex-col gap-1">
        <label class="label" for="obs-port">{m.obsPort()}</label>
        <input id="obs-port" type="number" min="1" max="65535" required class="input" bind:value={draft.port} />
      </div>
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-password">{m.obsPassword()}</label>
        <input id="obs-password" type="password" class="input" bind:value={draft.password} />
      </div>
    </div>
  {/if}

  <SectionActions {dirty} {busy} {onrevert} />
</form>
