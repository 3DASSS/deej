<script lang="ts">
  import { AppInfoDTO } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";

  let { appInfo }: { appInfo: AppInfoDTO | null } = $props();
</script>

<section class="flex flex-col gap-4">
  <div class="flex flex-col gap-1">
    <span class="label">{m.version()}</span>
    <span class="text-sm select-text">{appInfo?.version || "—"}</span>
  </div>

  <div class="flex flex-col gap-1">
    <span class="label">{m.configPath()}</span>
    <!-- long path, and the point of showing it is to copy it out -->
    <span class="text-sm break-all select-text">{appInfo?.configPath || "—"}</span>
    <span class="hint">{m.configPathHint()}</span>
  </div>
</section>
