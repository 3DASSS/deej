<script lang="ts">
  import { onMount } from "svelte";
  import { AppInfoDTO } from "../bindings/github.com/nik9play/deej/pkg/deej";
  import { init } from "./lib/state.svelte";
  import Titlebar from "./components/Titlebar.svelte";
  import Mixer from "./components/Mixer.svelte";
  import SettingsDialog from "./components/SettingsDialog.svelte";
  import TargetDialog from "./components/TargetDialog.svelte";

  let { appInfo }: { appInfo: AppInfoDTO | null } = $props();

  let settingsOpen = $state(false);
  let targetDialogOpen = $state(false);
  let targetSlider = $state(0);

  onMount(() => init());

  function editTargets(slider: number) {
    targetSlider = slider;
    targetDialogOpen = true;
  }
</script>

<Titlebar version={appInfo?.version ?? ""} onOpenSettings={() => (settingsOpen = true)} />

<main class="flex-1 overflow-hidden">
  <Mixer onEditTargets={editTargets} />
</main>

<SettingsDialog bind:open={settingsOpen} {appInfo} />
<TargetDialog bind:open={targetDialogOpen} slider={targetSlider} {appInfo} />
