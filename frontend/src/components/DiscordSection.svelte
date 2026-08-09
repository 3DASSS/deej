<script lang="ts">
  import { onDestroy } from "svelte";
  import CircleCheck from "@lucide/svelte/icons/circle-check";
  import CircleDashed from "@lucide/svelte/icons/circle-dashed";
  import Check from "@lucide/svelte/icons/check";
  import Copy from "@lucide/svelte/icons/copy";
  import { ParaglideMessage } from "@inlang/paraglide-js-svelte";
  import { Browser, Clipboard } from "@wailsio/runtime";
  import {
    DiscordSettings,
    DiscordStatusDTO,
    SettingsService,
  } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import FieldCheckbox from "./ui/FieldCheckbox.svelte";
  import SectionActions from "./ui/SectionActions.svelte";

  // same contract as the OBS tab: these settings make deej reconnect, so the
  // draft is owned by the settings dialog and committed explicitly
  let {
    draft = $bindable(),
    dirty,
    busy,
    onsave,
    onrevert,
  }: {
    draft: DiscordSettings;
    dirty: boolean;
    busy: boolean;
    onsave: () => void;
    onrevert: () => void;
  } = $props();

  let status: DiscordStatusDTO | null = $state(null);
  let linking = $state(false);
  let errorText = $state("");
  let localhostCopied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout>;
  const developerPortalUrl = "https://discord.com/developers/applications";
  const localhostRedirect = "http://localhost";

  // the connection comes up on its own once an account is linked, so the
  // section polls rather than waiting for the user to reopen it. this tab is
  // unmounted while another one is shown, so the timer only runs when visible
  const timer = setInterval(refreshStatus, 2000);
  onDestroy(() => {
    clearInterval(timer);
    clearTimeout(copyTimer);
  });

  void refreshStatus();

  async function refreshStatus() {
    try {
      status = await SettingsService.GetDiscordStatus();
    } catch {
      status = null;
    }
  }

  async function link() {
    linking = true;
    errorText = "";
    try {
      await SettingsService.LinkDiscord();
      await refreshStatus();
    } catch (err) {
      errorText = `${m.discordLinkError()}: ${err}`;
    } finally {
      linking = false;
    }
  }

  async function unlink() {
    errorText = "";
    try {
      await SettingsService.UnlinkDiscord();
      await refreshStatus();
    } catch (err) {
      errorText = `${err}`;
    }
  }

  function openDeveloperPortal(event: MouseEvent) {
    event.preventDefault();
    void Browser.OpenURL(developerPortalUrl);
  }

  async function copyLocalhost() {
    try {
      await Clipboard.SetText(localhostRedirect);
      localhostCopied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (localhostCopied = false), 1500);
    } catch {
      localhostCopied = false;
    }
  }

  // linking talks to the credentials deej has on disk, not to the ones being
  // typed, so it stays out of reach until the draft is committed
  const canLink = $derived(!dirty && !!draft.clientId && !!draft.clientSecret);
</script>

<form
  onsubmit={(event) => {
    event.preventDefault();
    onsave();
  }}
>
  <FieldCheckbox id="discord-enabled" bind:checked={draft.enabled} label={m.discordEnabled()} />
  <div class="hint mt-2">{m.discordHint()}</div>

  {#if draft.enabled}
    <div class="mt-3">
      <FieldCheckbox
        id="discord-volume-conversion"
        bind:checked={draft.volumeConversion}
        label={m.discordVolumeConversion()}
      />
      <div class="hint mt-1">{m.discordVolumeConversionHint()}</div>
    </div>

    <div class="hint mt-3">
      <ParaglideMessage message={m.discordSetupHint}>
        {#snippet developerPortal({ children })}
          <a
            href={developerPortalUrl}
            target="_blank"
            rel="noreferrer"
            class="cursor-pointer font-medium text-body underline underline-offset-2 active:text-muted"
            onclick={openDeveloperPortal}
          >{@render children?.()}</a>
        {/snippet}
        {#snippet localhost({ children })}
          <button
            type="button"
            class="inline-flex cursor-pointer items-center gap-1 font-mono font-medium text-body active:text-muted"
            onclick={copyLocalhost}
            title={m.copyLocalhost()}
            aria-label={m.copyLocalhost()}
          >
            {@render children?.()}
            {#if localhostCopied}
              <Check size={12} aria-hidden="true" />
            {:else}
              <Copy size={12} aria-hidden="true" />
            {/if}
          </button>
        {/snippet}
      </ParaglideMessage>
    </div>
    <div class="mt-3 flex flex-wrap gap-3.5">
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="discord-client-id">{m.discordClientId()}</label>
        <input
          id="discord-client-id"
          type="text"
          inputmode="numeric"
          pattern="\d*"
          class="input"
          bind:value={draft.clientId}
        />
      </div>
      <div class="flex min-w-36 flex-1 flex-col gap-1">
        <label class="label" for="discord-client-secret">{m.discordClientSecret()}</label>
        <input id="discord-client-secret" type="password" class="input" bind:value={draft.clientSecret} />
      </div>
    </div>

    <div class="mt-4 flex flex-wrap items-center gap-2">
      {#if status?.linked}
        <button class="btn" type="button" onclick={link} disabled={!canLink || linking || busy}>
          {m.discordRelink()}
        </button>
        <button class="btn" type="button" onclick={unlink} disabled={linking || busy}>
          {m.discordUnlink()}
        </button>
      {:else}
        <button class="btn btn-primary" type="button" onclick={link} disabled={!canLink || linking || busy}>
          {m.discordLink()}
        </button>
      {/if}

      <span class="hint flex items-center gap-1.5">
        {#if linking}
          {m.discordLinking()}
        {:else if !status?.linked}
          <CircleDashed size={14} class="shrink-0" />
          {m.discordNotLinked()}
        {:else if status.connected}
          <CircleCheck size={14} class="shrink-0 text-success" />
          {status.channel ? m.discordVoiceChannel({ channel: status.channel }) : m.discordConnected()}
        {:else}
          <CircleDashed size={14} class="shrink-0" />
          {m.discordConnecting()}
        {/if}
      </span>
    </div>

    {#if dirty}
      <div class="hint mt-2">{m.discordSaveFirst()}</div>
    {/if}

    {#if errorText}
      <div class="mt-2 text-[13px] text-danger">{errorText}</div>
    {/if}
  {/if}

  <SectionActions {dirty} {busy} {onrevert} />
</form>
