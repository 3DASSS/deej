# deej Architecture Refactoring Plan

## Goal and scope

Split the current `pkg/deej` into packages with explicit dependencies, move the
entry point to `cmd/deej`, group the frontend by feature, and separate build
configuration from build output. Keep a single Go module.

Preserve user-facing behavior: configuration format and migrations, CLI, slider
targets, volume curves, GUI events, reconnection, profiles, hotkeys, notifications,
and both GUI and headless modes. Handle dependency upgrades and new features in
separate changes.

Each stage must leave the project buildable. Until package extraction is complete,
`pkg/deej` remains the temporary composition point. Remove transitional adapters
when extracting `app`.

## Target structure

```text
cmd/deej/                     entry point, GUI/headless bootstrap, winres and .syso
internal/
  app/                        lifecycle, core composition, logging, crash reporting
  config/                     model, normalization, YAML, persistence, hot reload
  audio/                      audio session interfaces and Windows/Linux backends
  serial/                     serial transport, protocol and noise filtering
  mixer/                      slider -> targets -> volume routing
  integrations/
    obs/                      OBS client
    discord/                  RPC, auth, roster, volume, IPC transport
  desktop/                    Wails, window, tray, hotkeys, service, DTOs, GUI events
  platform/                   processes, windows, autorun, opening external files
    win/                      low-level Windows APIs
  i18n/                       Go localization and embedded TOML catalogs
  reconnect/                  shared connection recovery lifecycle
  atomicfile/                 shared atomic writes for config and Discord tokens
  notify/                     system notifications
  icon/                       embedded icons and their assets
frontend/src/
  features/mixer/             Mixer, SliderColumn, TargetDialog, targets.ts
  features/settings/          SettingsDialog and sections/
  components/                 Titlebar and shared UI components
  lib/                        api.ts, state.svelte.ts, profiles.ts, tick.ts
build/
  config.yml                  Wails development configuration
  tasks/                      windows.yml and linux.yml
bin/                          binaries and installers
```

Keep tests beside their implementations. Keep `_windows.go` and `_linux.go` files
in the same package as their shared interfaces.

## Dependency rules

- `cmd/deej` connects the `app` core to `desktop`. Import `desktop` only from the
  GUI bootstrap; the headless bootstrap must not import it.
- `app` manages the core and does not import Wails, `desktop`, or frontend assets.
- `config`, `audio`, `serial`, OBS, and Discord do not import `app`, `mixer`, or
  `desktop`. Give `mixer` its dependencies through narrow interfaces.
- Pass dependencies to constructors: settings access and change subscriptions,
  logging, notifications, event sources, or callbacks. Gradually remove `*Deej`;
  do not replace it with another universal dependency container with the same
  fields.
- `config.Settings` remains the single settings model for YAML, runtime, and GUI.
  Do not introduce a separate GUI settings model.
- Participant types belong to Discord; audio session information belongs to
  audio/mixer. The GUI service converts these types to DTOs while preserving
  existing JSON fields.
- Define interfaces at their consumers when needed to separate dependencies.
  A global event bus and a shared DTO package are unnecessary.
- Provide access to the current settings snapshot through a getter/provider,
  rather than capturing one snapshot in a constructor. Each connection generation
  retains its dial parameters; preserve checks for config changes during dial and
  adoption. Serialize reconnect `Start/Stop`, ensure `Close` unblocks `Watch`, and
  prevent failures from an old generation from terminating a newer connection.
- Adapt tests to new package boundaries in the same stage as production code.
  Replace fixtures that access `CanonicalConfig.current`, `Deej`, or `baseSession`
  directly with fake providers/sessions. Do not export private fields for tests.
- Move a GUI service or its public types as one coordinated change: generator
  scope, bindings, TypeScript imports, and embedded frontend. Wails bindings use
  numeric `Call.ByID` identifiers derived from the fully qualified Go method name;
  successful compilation with stale bindings does not prove that the API works.
  After each such move, exercise real `GetAppInfo` and `GetSettings` calls and
  save a temporary test configuration through `SaveSettings`.

## Stage 0. Establish the baseline

1. Run the existing Go tests, frontend check, and lint. Record any pre-existing
   failures separately from refactoring results.
2. Verify Windows GUI, Linux GUI, and Linux headless builds using current Task
   targets.
3. Record manual verification scenarios: loading legacy config, saving from the
   GUI, hand edits, profile switching, reconnection, and shutdown.

Outcome: a known baseline and verification scenarios for subsequent stages.

## Stage 1. Move the entry point and build files

1. Move `pkg/deej/cmd` to `cmd/deej`, including `winres`, the `go:generate`
   directive, and architecture-specific `.syso` files.
2. Update all Go build paths, resource existence checks, and winres icon paths.
3. Move platform Taskfiles to `build/tasks/`, preserving public task names and
   execution from the repository root.
4. Use `bin/` for build output. Update Taskfiles, Inno Setup, CI rename/upload
   steps, and `.gitignore` together. Keep build configuration in `build/`.
   Account for the existing `build/*` ignore rule: unignore `build/tasks/` itself
   before adding exceptions for its files. Check `git check-ignore`, ensure the
   Taskfiles are tracked, and verify that tasks work in a clean checkout.
5. Preserve linker injection into `main.gitCommit`, `main.versionTag`, and
   `main.buildType`, the `production` tag, and the release flag `-H=windowsgui`.
   Apply Windows development flags `-N -l` only to
   `github.com/nik9play/deej/...`; verify the arm64 development build and version
   display in the GUI.
6. Keep `*:build:dev:live` free of frontend builds. Preserve
   `frontend/dist/.gitkeep` and its recreation by the Vite plugin. Preserve the
   Vite-first startup order, delay, and devserver environment variables in
   `build/config.yml`. Add `bin` to the development watcher's ignored directories.
   Verify a cold `wails3 dev` start, HMR, and Go rebuilds without rerunning npm build.

Outcome: GUI and headless builds use `cmd/deej`; the installer and CI use the new
paths. Go package boundaries are unchanged at this stage.

## Stage 2. Extract supporting packages

1. Move `reconnect`, `notify`, `icon`, and `win` from `pkg` to their respective
   locations under `internal`. Fix imports and installer/winres asset paths.
2. Extract `writeFileAtomic` from `config.go` into `internal/atomicfile` before
   separating config and Discord: `DiscordClient.storeToken` also uses it.
   Preserve temporary files in the destination directory, mode 0600, rename, and
   cleanup on failure. Both consumers use the same implementation.
3. Break up `pkg/deej/util`: move process, window, autorun, and external file
   opening functions to `platform`; move noise filtering alongside the serial
   protocol when extracting it.
4. Replace simple filesystem and math wrappers with local standard-library calls
   where a separate abstraction adds no value.
5. Extract Go localization into `internal/i18n`; move the TOML catalogs together
   with the embedding code. Preserve language detection and fallback.

Outcome: the extracted packages do not depend on `pkg/deej`; embedded assets work
on both operating systems.

## Stage 3. Extract configuration

1. Split `config_model.go` into `model.go`, `defaults.go`, `normalize.go`,
   `mapping.go`, and `legacy.go` within `internal/config`.
2. Split `config.go` into `store.go` and `watch.go`.
3. Expose only the necessary operations, including safe slider target lookup.
   Keep normalization, migration, and internal copying private to the package.
4. Report configuration loading outcomes through a small callback. Localize
   notifications outside the store.
5. Before moving the first GUI-visible types, introduce `frontend/src/lib/api.ts`
   as the bindings access point, including imports in `main.ts`, `App.svelte`, and
   state. Then move Settings, immediately update the generator scope, regenerate
   bindings, and rebuild the frontend. Do not defer this work to stage 8.
6. Rework serial/mixer/Discord fixtures that currently populate
   `CanonicalConfig.current` directly. Keep normalization, YAML, and watcher tests
   in config; also move the OBS default-value test from `obs_test.go` to config.

Preserve immutable snapshots, atomic writes, serialized read-modify-write cycles,
trailing-edge debounce, coalescing, and skipping unchanged reloads. GUI saves
reject normalization errors; file loads apply corrections and log them.

Outcome: config is independent of the GUI and orchestrator; existing model tests
are migrated and legacy YAML compatibility is verified.

## Stage 4. Extract audio and serial

1. Move `Session`, `SessionFinder`, events, and platform implementations into
   `internal/audio`. Place master/system/mic identifiers at the audio level so
   the package does not depend on the mixer.
2. Extract serial into `internal/serial`: `client.go` for connections and
   subscriptions, `protocol.go` for parsing/normalization, and `noise.go` for
   filtering.
3. Replace serial's `*Deej` dependency with settings access, logging,
   notifications, and the verbose flag. Expose port enumeration through serial
   for the GUI service as well.
4. Move tests and update calls from the temporary orchestrator. Fake audio
   sessions implement the public `audio.Session` interface without embedding
   private `baseSession`; serial tests receive a fake config provider.

Preserve synchronous handling of session removal before resources are released,
handler registration before discovery, the CRLF protocol, inversion, and value
re-emission after reload even without physical slider movement.

Outcome: audio and serial operate without knowledge of the orchestrator or GUI.

## Stage 5. Extract OBS and Discord

1. Move OBS into `internal/integrations/obs`; remove its `*Deej` dependency.
2. Split Discord into `client.go`, `rpc.go`, `auth.go`, `roster.go`, `volume.go`,
   and platform-specific `ipc_*.go` files within one package.
3. Move participant/status types from the GUI service into Discord; keep GUI DTO
   conversion in the service.
4. Give Discord the token-file path and a callback providing current Discord
   targets and slider positions for volume restoration. Keep the adapter in
   `pkg/deej` temporarily, then move it into `app`.
5. The public Discord volume API accepts targets within Discord. Parsing config
   strings such as `deej.discord:*` belongs to the mixer.

Preserve `winio.DialPipe`, a single owner for roster writes, nonblocking event
handoff from the read loop, reconciliation, deduplication, and the 100 ms flush.
Authorization runs only through an explicit Link action; the token remains beside
the config file, outside YAML. Preserve linear/cubic volume conversion for OBS
and Discord participants; Discord input/output always use the linear scale.

Preserve replay of current values after connection, participant join/rejoin,
voice-channel changes, and reconciliation. Keep `lastSent` invalidation at the
existing points so deduplication cannot suppress volume restoration without
slider movement. Codec/roster/throttle tests belong to Discord; the routing test
in `discord_test.go` belongs to mixer, while assembling current mapped slider
values belongs to the app adapter. Until those packages are extracted, keep these
tests with the temporary orchestrator and update their dependencies.

Outcome: both integrations are independent of mixer, serial, and GUI; current
volume restoration behavior is preserved.

## Stage 6. Extract the mixer

1. Move `session_map.go` into `internal/mixer`: `mixer.go` for event handling and
   session storage, and `targets.go` for target resolution.
2. Inject slider event access, config, finder, OBS/Discord control, and foreground
   window detection instead of `*Deej`.
3. Return session information using a type owned by the core, without depending
   on the GUI service's `SessionInfoDTO`.
4. Preserve fresh `ActiveMapping()` lookup on every slider event and refresh the
   unmapped-session cache after config changes.

Outcome: the mixer can be tested with fake event sources and volume controllers.
Verify special target routing and profile switching without slider movement,
using the actual reload/reset/event sequence.

## Stage 7. Separate app and desktop

1. Move the `deej.go` core, logging, and crash reporting into `internal/app`.
   Define explicit initialization, startup, and shutdown operations.
2. Split `tray.go` into `desktop.go`, `tray.go`, `hotkeys.go`, and `events.go`.
   Move `settings_service.go` and extract `dto.go`.
3. Give desktop the required core services/operations; app must not import
   desktop. Compose the GUI shell and headless startup in separate `cmd/deej`
   bootstrap files selected by build tags.
   Immediately after moving SettingsService, update the generator scope and API
   facade, regenerate bindings, rebuild the embedded frontend, and exercise real
   service calls. Preserving method names and JSON fields is insufficient for
   `Call.ByID` compatibility.
4. Exclude GUI files from headless builds. Verify that the headless binary's
   dependency graph contains neither Wails nor frontend assets.
5. Coordinate core and Wails shutdown: release audio sessions, stop reconnect and
   watcher loops, and close the GUI before exiting the process.
6. Preserve the distinct runtime behavior of `DEEJ_NO_TRAY_ICON`.

Make startup order explicit: load the initial config and resolve the language;
register mixer/finder handlers and separate subscriptions; ensure all consumers
are ready; only then start serial/OBS/Discord. In GUI mode, preserve subscriptions
inside ApplicationStarted before starting the runtime. Give each consumer its
own channel: sharing one channel between mixer and desktop would distribute
rather than broadcast events. Serial currently delivers events synchronously
through unbuffered channels.

During shutdown, keep consumers receiving events until producers have stopped,
including the state event emitted when serial closes; then terminate consumer
goroutines. Define ownership of each subscription, cancellation, and channel
closure. Repeated exit requests must not run concurrent Stop operations or close
channels twice. Do not leave senders blocked when the GUI closes or startup fails.

Preserve `deej:*` event names and payloads, a single settings window, and hotkey
updates only when the accelerator set changes. Preserve the execution context
required by each Wails API individually: `tray.SetMenu` switches to the main
thread itself. When updating a running GUI, call it off the main thread without
an `InvokeSync`/`InvokeAsync` wrapper that moves the call there. Reapply the menu
to the tray after changing its tree. Remove transitional adapters and the emptied
`pkg/deej` package.

Outcome: app is independent of the GUI; headless builds require neither Node nor
a frontend build.

## Stage 8. Regroup the frontend and verify bindings

1. Move Mixer, SliderColumn, TargetDialog, and targets.ts into `features/mixer/`;
   move SettingsDialog and its sections into `features/settings/`.
2. Keep shared elements in `components/ui` and Titlebar in `components`.
3. Use the `lib/api.ts` introduced in stage 3 for Go bindings and their types.
   Update relative imports for components, UI, lib, and Paraglide after moving
   files. Window control through the Wails runtime remains a GUI responsibility.
4. Verify the final `generate:bindings` setup: include the remaining `pkg` during
   migration, then `cmd`/`internal` after completion. The generator cleans its
   output by default; scan all required packages together so a later invocation
   does not delete an earlier package's bindings.
5. Verify generated events, removal of stale generated modules at old package
   paths, and the frontend build.
6. Preserve AppInfo/locale/platform initialization before mount; conversion of
   generated models through `$state.snapshot`; subscribe-before-initial-fetch
   ordering; and cleanup when closing the window. Preserve COM/OBS/Discord drafts
   across tabs, patch merging, saving against current config, and flushing pending
   changes on close.

Outcome: components are grouped by feature, Settings remains generated from Go,
and frontend checks and the production build pass.

## Verification and completion

After each stage, run the tests and builds relevant to that change. Add tests for
changed boundaries: config migration/persistence, mapping changes, Discord volume
restoration, and lifecycle. Verify mechanical moves with existing tests and
compilation.

Final verification:

- Run `wails3 task test` on Windows and Linux; Linux uses `purego`.
- Run `golangci-lint run`, the project's only lint command.
- Run `wails3 task frontend:build` and `npm --prefix frontend run check`.
  Install frontend dependencies with `npm --prefix frontend ci` before the first
  check.
- Build Windows release/dev, Linux GUI release/dev, and Linux headless for
  amd64/arm64 through the existing public Task targets. Also build Linux GUI with
  `EXTRA_TAGS=gtk3`.
- Build the Windows installer when ISCC is available, including universal
  packaging.
- Verify a clean headless build without Node or frontend assets; verify Wails dev
  startup and HMR.
- Manually verify serial reconnection, profiles and hotkeys, config hot reload,
  OBS/Discord, notifications, settings window open/close, and process shutdown.
- Verify Discord join/rejoin and voice-channel changes without slider movement:
  current volume is restored and deduplication does not suppress it.
- Exercise GUI scenarios: draft -> another tab -> back; hand-edit config while
  settings are open; rapid changes -> close; validation failure -> correction and
  retry; reopen the window; start in en/ru with correct platform styling and no
  duplicate event subscriptions.
- Verify startup/shutdown with active serial and an unavailable device: GUI Quit
  and process signals must not leave blocked sends or duplicate Stop operations.
- Verify legacy config and the token file. Preserve user-data paths relative to
  the executable or explicitly selected config, rather than the repository root.
- Search source, generated bindings, Taskfiles, installer, CI, and documentation
  for stale paths. Verify embed paths and `.syso` generation from scratch.
- Update README, AGENTS.md, CLAUDE.md, docs/CONTRIBUTING.md, frontend/scripts
  READMEs, and architecture documentation for the new packages and paths.

Deliver stages as separate commits or small PRs. Within a large stage, split files
inside the current package first, then change dependencies and move the package.
This makes diffs easier to review and regressions easier to locate.
