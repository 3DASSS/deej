<script lang="ts">
  import { SettingsDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";
  import FieldSelect from "./ui/FieldSelect.svelte";

  let { settings }: { settings: SettingsDTO } = $props();

  const languageItems = $derived([
    { value: "auto", label: t("languageAuto") },
    { value: "en", label: "English" },
    { value: "ru", label: "Русский" },
  ]);
</script>

<section class="card">
  <h2 class="mb-3 text-sm font-semibold">{t("language")}</h2>

  <div class="flex max-w-xs flex-col gap-1">
    <FieldSelect id="language" ariaLabel={t("language")} bind:value={settings.language} items={languageItems} />
    <div class="hint">{t("languageHint")}</div>
  </div>
</section>
