<script lang="ts">
  import X from "@lucide/svelte/icons/x";
  import { m } from "../../paraglide/messages";

  let {
    id,
    value = $bindable(""),
    disabled = false,
  }: { id?: string; value: string; disabled?: boolean } = $props();

  let capturing = $state(false);

  // keys the browser names differently from wails (application/keys.go).
  // wails lowercases what it parses, so these are display spellings
  const namedKeys: Record<string, string> = {
    ArrowUp: "Up",
    ArrowDown: "Down",
    ArrowLeft: "Left",
    ArrowRight: "Right",
    PageUp: "Page Up",
    PageDown: "Page Down",
    NumpadEnter: "Enter",
    NumLock: "NumLock",
  };

  // event.code, not event.key: with shift held, Digit2 reports "@" and KeyA
  // reports "A", neither of which survives a layout change
  function keyName(code: string): string | null {
    if (namedKeys[code]) return namedKeys[code];
    if (/^Key[A-Z]$/.test(code)) return code.slice(3);
    if (/^Digit[0-9]$/.test(code)) return code.slice(5);
    if (/^F([1-9]|1[0-9]|2[0-4])$/.test(code)) return code;
    if (["Home", "End", "Space", "Tab", "Enter", "Backspace", "Delete", "Escape"].includes(code)) {
      return code;
    }
    return null;
  }

  function onkeydown(event: KeyboardEvent) {
    if (disabled) return;

    const modifiers = [
      event.ctrlKey && "Ctrl",
      event.altKey && "Alt",
      event.shiftKey && "Shift",
      event.metaKey && "Super",
    ].filter(Boolean);

    // an unmodified tab still moves focus, so the field can be left by keyboard
    if (modifiers.length === 0 && event.key === "Tab") return;

    event.preventDefault();
    // the dialog closes on a document-level escape handler, and every other
    // key would reach the dialog's own shortcuts while capturing
    event.stopPropagation();

    if (modifiers.length === 0) {
      // escape leaves capture mode, backspace/delete clear the binding
      if (event.key === "Escape") (event.currentTarget as HTMLElement).blur();
      if (event.key === "Backspace" || event.key === "Delete") value = "";
      return;
    }

    const key = keyName(event.code);
    if (!key) return;

    // a bare key is never accepted: it would be grabbed system-wide, so deej
    // would swallow it in every other application (the go side rejects those)
    value = [...modifiers, key].join("+");
  }
</script>

<div class="flex gap-1.5">
  <!-- readonly, not disabled: it still has to take focus to capture keys -->
  <input
    {id}
    type="text"
    class="input flex-1 {capturing ? 'ring-1 ring-accent' : ''}"
    readonly
    {disabled}
    value={value || (capturing ? m.hotkeyPressKeys() : m.hotkeyNone())}
    placeholder={m.hotkeyNone()}
    title={m.hotkeyHint()}
    onfocus={() => (capturing = true)}
    onblur={() => (capturing = false)}
    {onkeydown}
  />
  <button
    type="button"
    class="btn px-2.5"
    {disabled}
    onclick={() => (value = "")}
    title={m.hotkeyClear()}
    aria-label={m.hotkeyClear()}
  >
    <X size={14} />
  </button>
</div>
