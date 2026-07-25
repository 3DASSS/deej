<script lang="ts">
  import { m } from "../../paraglide/messages";

  // save/revert pair for sections that commit on demand instead of applying
  // immediately. save submits the enclosing form, so the browser validates the
  // section's fields first
  let {
    dirty,
    busy = false,
    onrevert,
  }: { dirty: boolean; busy?: boolean; onrevert: () => void } = $props();
</script>

<div class="mt-4 flex items-center gap-2 border-t border-edge pt-3">
  <button class="btn btn-primary" type="submit" disabled={!dirty || busy}>{m.save()}</button>
  <button class="btn" type="button" onclick={onrevert} disabled={!dirty || busy}>{m.revert()}</button>
</div>
