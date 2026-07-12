<script lang="ts">
  import { AppInfoDTO } from "../bindings/github.com/nik9play/deej/pkg/deej";
  import Titlebar from "./components/Titlebar.svelte";
  import SettingsDialog from "./components/SettingsDialog.svelte";

  let { appInfo }: { appInfo: AppInfoDTO | null } = $props();

  let settingsOpen = $state(false);
</script>

<Titlebar version={appInfo?.version ?? ""} onOpenSettings={() => (settingsOpen = true)} />

<main class="flex-1 overflow-hidden">
  <!-- mixer -->
</main>

<SettingsDialog bind:open={settingsOpen} {appInfo} />
