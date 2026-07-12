<script lang="ts">
  import { onMount } from "svelte";
  import { AppInfoDTO, SettingsDTO, SettingsService } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { t } from "../lib/i18n";

  let { settings, appInfo }: { settings: SettingsDTO; appInfo: AppInfoDTO | null } = $props();

  let sessionNames: string[] = $state([]);
  let newTargets: Record<number, string> = $state({});

  onMount(async () => {
    try {
      sessionNames = await SettingsService.GetSessionNames();
    } catch (err) {
      console.error("failed to load session names", err);
    }
  });

  const suggestions = $derived.by(() => {
    const special = appInfo?.specialTargets ?? [];
    return [...special, ...sessionNames.filter((name) => !special.includes(name))];
  });

  const rows = $derived([...settings.sliderMapping].sort((a, b) => a.slider - b.slider));

  function addSlider() {
    const used = new Set(settings.sliderMapping.map((entry) => entry.slider));
    let index = 0;
    while (used.has(index)) {
      index++;
    }
    settings.sliderMapping.push({ slider: index, targets: [] });
  }

  function removeSlider(slider: number) {
    settings.sliderMapping = settings.sliderMapping.filter((entry) => entry.slider !== slider);
  }

  function commitTarget(slider: number) {
    const value = (newTargets[slider] ?? "").trim();
    if (!value) return;

    const entry = settings.sliderMapping.find((e) => e.slider === slider);
    if (entry && !entry.targets.includes(value)) {
      entry.targets.push(value);
    }
    newTargets[slider] = "";
  }

  function removeTarget(slider: number, target: string) {
    const entry = settings.sliderMapping.find((e) => e.slider === slider);
    if (entry) {
      entry.targets = entry.targets.filter((existing) => existing !== target);
    }
  }
</script>

<section class="card">
  <h2>{t("sliderMapping")}</h2>
  <div class="hint" style="margin-bottom: 8px;">{t("sliderMappingHint")}</div>

  {#each rows as entry (entry.slider)}
    <div class="mapping-row">
      <span class="mapping-index">{t("slider")} {entry.slider}</span>

      <div class="mapping-targets">
        {#each entry.targets as target (target)}
          <span class="chip">
            {target}
            <button type="button" title={t("removeTarget")} onclick={() => removeTarget(entry.slider, target)}>&#x2715;</button>
          </span>
        {/each}
        {#if entry.targets.length === 0}
          <span class="empty-note">{t("noTargets")}</span>
        {/if}
        <input
          type="text"
          list="target-suggestions"
          placeholder={t("addTargetPlaceholder")}
          bind:value={newTargets[entry.slider]}
          onchange={() => commitTarget(entry.slider)}
          onkeydown={(e) => e.key === "Enter" && commitTarget(entry.slider)}
        />
      </div>

      <button class="icon" type="button" title={t("removeSlider")} onclick={() => removeSlider(entry.slider)}>&#x2715;</button>
    </div>
  {/each}

  <datalist id="target-suggestions">
    {#each suggestions as suggestion (suggestion)}
      <option value={suggestion}></option>
    {/each}
  </datalist>

  <div style="margin-top: 10px;">
    <button type="button" onclick={addSlider}>{t("addSlider")}</button>
  </div>
</section>
