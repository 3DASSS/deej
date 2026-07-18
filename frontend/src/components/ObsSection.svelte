<script lang="ts">
  import { Settings } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";

  let { settings }: { settings: Settings } = $props();
</script>

<section>
  <FieldCheckbox id="obs-enabled" bind:checked={settings.obs.enabled} label={m.obsEnabled()} />
  <div class="hint mt-2">{m.obsHint()}</div>

  {#if settings.obs.enabled}
    <div class="hint mt-3">{m.obsWebsocketHint()}</div>
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-host">{m.obsHost()}</label>
        <input id="obs-host" type="text" class="input" bind:value={settings.obs.host} />
      </div>
      <div class="flex min-w-24 flex-col gap-1">
        <label class="label" for="obs-port">{m.obsPort()}</label>
        <input id="obs-port" type="number" min="1" max="65535" class="input" bind:value={settings.obs.port} />
      </div>
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="obs-password">{m.obsPassword()}</label>
        <input id="obs-password" type="password" class="input" bind:value={settings.obs.password} />
      </div>
    </div>
  {/if}
</section>
