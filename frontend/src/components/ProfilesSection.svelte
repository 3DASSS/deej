<script lang="ts">
  import Check from "@lucide/svelte/icons/check";
  import Copy from "@lucide/svelte/icons/copy";
  import Plus from "@lucide/svelte/icons/plus";
  import Trash2 from "@lucide/svelte/icons/trash-2";
  import { Profile, Settings } from "../../bindings/github.com/nik9play/deej/pkg/deej";
  import { m } from "../paraglide/messages";
  import HotkeyInput from "./ui/HotkeyInput.svelte";

  let { settings, onsave }: { settings: Settings; onsave: (patch: Partial<Settings>) => void } = $props();

  function commit(profiles: Profile[], activeProfile = settings.activeProfile) {
    onsave({ profiles, activeProfile });
  }

  function uniqueName(): string {
    const taken = new Set(settings.profiles.map((profile) => profile.name.toLowerCase()));
    for (let i = settings.profiles.length + 1; ; i++) {
      const name = m.newProfileName({ number: i });
      if (!taken.has(name.toLowerCase())) return name;
    }
  }

  function add(from?: Profile) {
    commit([
      ...$state.snapshot(settings.profiles),
      {
        name: uniqueName(),
        hotkey: "",
        sliderMapping: from ? $state.snapshot(from.sliderMapping) : [],
      },
    ]);
  }

  function remove(index: number) {
    const profiles = $state.snapshot(settings.profiles).filter((_, i) => i !== index);

    // the active profile may be the one that just went away
    const active = profiles.some((profile) => profile.name === settings.activeProfile)
      ? settings.activeProfile
      : profiles[0].name;

    commit(profiles, active);
  }

  function update(index: number, patch: Partial<Profile>) {
    const profiles = $state.snapshot(settings.profiles);
    const previousName = profiles[index].name;
    profiles[index] = { ...profiles[index], ...patch };

    // renaming the active profile keeps it active
    const active = settings.activeProfile === previousName ? profiles[index].name : settings.activeProfile;

    commit(profiles, active);
  }
</script>

<section>
  <p class="hint mb-3">{m.profilesHint()}</p>

  <div class="flex flex-col gap-2">
    {#each settings.profiles as profile, index (index)}
      <div class="card p-3 {profile.name === settings.activeProfile ? 'border-accent' : ''}">
        <div class="flex items-stretch gap-2">
          <button
            type="button"
            class="flex size-5 shrink-0 self-center items-center justify-center rounded-full border transition-colors {profile.name ===
            settings.activeProfile
              ? 'border-accent bg-accent text-surface'
              : 'border-edge text-transparent hover:border-accent'}"
            title={m.setActiveProfile()}
            aria-label={m.setActiveProfile()}
            aria-pressed={profile.name === settings.activeProfile}
            onclick={() => commit($state.snapshot(settings.profiles), profile.name)}
          >
            <Check size={12} />
          </button>
          <input
            type="text"
            class="input flex-1"
            aria-label={m.profileName()}
            value={profile.name}
            onchange={(event) => update(index, { name: event.currentTarget.value })}
          />
          <button
            type="button"
            class="btn flex items-center justify-center px-2.5"
            onclick={() => add(profile)}
            title={m.duplicateProfile()}
            aria-label={m.duplicateProfile()}
          >
            <Copy size={14} />
          </button>
          <button
            type="button"
            class="btn flex items-center justify-center px-2.5"
            disabled={settings.profiles.length < 2}
            onclick={() => remove(index)}
            title={m.deleteProfile()}
            aria-label={m.deleteProfile()}
          >
            <Trash2 size={14} />
          </button>
        </div>

        <div class="mt-2 flex items-center gap-2">
          <span class="label shrink-0">{m.profileHotkey()}</span>
          <div class="flex-1">
            <HotkeyInput
              bind:value={() => profile.hotkey, (hotkey) => update(index, { hotkey })}
            />
          </div>
        </div>
      </div>
    {/each}
  </div>

  <button type="button" class="btn mt-3 flex items-center gap-1.5" onclick={() => add()}>
    <Plus size={14} />
    {m.addProfile()}
  </button>
</section>
