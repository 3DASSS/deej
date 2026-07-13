<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldSelect from "./ui/FieldSelect.svelte";

  let { settings }: { settings: SettingsDTO } = $props();

  const languageItems = $derived([
    { value: "auto", label: m.languageAuto() },
    { value: "en", label: "English" },
    { value: "ru", label: "Русский" },
  ]);
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{m.language()}</h2>

  <div class="flex max-w-xs flex-col gap-1">
    <FieldSelect id="language" ariaLabel={m.language()} bind:value={settings.language} items={languageItems} />
    <div class="hint">{m.languageHint()}</div>
  </div>
</section>
