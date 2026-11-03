<script lang="ts">
  import { onMount } from "svelte";
  import { AppInfoDTO } from "./lib/api";
  import { init } from "./lib/state.svelte";
  import Titlebar from "./components/Titlebar.svelte";
  import Mixer from "./features/mixer/Mixer.svelte";
  import SettingsDialog from "./features/settings/SettingsDialog.svelte";
  import TargetDialog from "./features/mixer/TargetDialog.svelte";

  let { appInfo }: { appInfo: AppInfoDTO | null } = $props();

  let settingsOpen = $state(false);
  let settingsTab = $state("general");
  let targetDialogOpen = $state(false);
  let targetSlider = $state(0);

  onMount(() => init());

  function editTargets(slider: number) {
    targetSlider = slider;
    targetDialogOpen = true;
  }

  function openSettings(tab = "general") {
    settingsTab = tab;
    settingsOpen = true;
  }
</script>

<Titlebar
  onOpenSettings={() => openSettings()}
  onOpenProfiles={() => openSettings("profiles")}
  onOpenConnection={() => openSettings("connection")}
/>

<main class="flex-1 overflow-hidden">
  <Mixer onEditTargets={editTargets} />
</main>

<SettingsDialog bind:open={settingsOpen} initialTab={settingsTab} {appInfo} />
<TargetDialog
  bind:open={targetDialogOpen}
  slider={targetSlider}
  {appInfo}
  onOpenObsSettings={() => {
    targetDialogOpen = false;
    openSettings("obs");
  }}
  onOpenDiscordSettings={() => {
    targetDialogOpen = false;
    openSettings("discord");
  }}
/>
