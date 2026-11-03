# AGENTS.md

This file provides repository instructions for coding agents working in this project.

## Project Overview

deej is a hardware volume mixer client written in Go that pairs with an Arduino to provide physical volume control for Windows and Linux. It communicates with Arduino hardware via serial port and controls per-application audio volumes through OS-specific audio APIs.

This is a fork of the original deej project (github.com/omriharel/deej) with automatic COM port reconnection, autorun support, updated dependencies, OBS websocket and Discord RPC integrations, i18n support, and a Wails-based settings GUI with profiles and global hotkeys.

## Build Commands

Builds are orchestrated by [Task](https://taskfile.dev) (`Taskfile.yml` plus `build/tasks/{windows,linux}.yml`) — the same build system that underpins `wails3 build`. The GUI `build`, `build:dev` and `build:all` tasks compile the embedded Svelte frontend before the Go binary, so Node.js and npm are required. They run `npm ci` only when `frontend/node_modules` is absent, then `npm run build`; after dependency or lockfile changes, run `npm ci` in `frontend/` explicitly. The Linux headless build needs neither Node.js nor frontend assets. The `*:build:dev:live` tasks skip the frontend build because Wails dev mode serves it from Vite. Run tasks with `wails3 task <name>` — the wails3 CLI embeds the Task runner and is what's installed here (the standalone `task` binary also works if present, but don't assume it). Binaries and installers go to `bin/`; frontend assets go to `frontend/dist/`.

```bash
wails3 task dev                  # Go rebuild-on-change + Vite HMR
wails3 task frontend:build       # build only the embedded frontend assets
wails3 task windows:build        # release exe (optimized, -H=windowsgui, no console)
wails3 task windows:build:dev    # dev exe (debug symbols, console window)
wails3 task windows:build:all    # release + dev exe
wails3 task windows:package      # release exe + Inno Setup installer (needs ISCC)
wails3 task windows:package:universal # installer from prebuilt amd64 + arm64 exe files
wails3 task linux:build          # release GUI binary (CGO-free: purego + GTK4/WebKitGTK)
wails3 task linux:build:dev      # dev GUI binary
wails3 task linux:build:all      # release + dev GUI binaries
wails3 task linux:build:headless # GUI-less daemon, CGO_ENABLED=0, cross-compilable
wails3 task generate:bindings    # regenerate Wails Go->TS bindings
wails3 task generate:resources   # regenerate Windows icon/manifest resources
```

The Windows `build` task runs `go generate` for winres icon/manifest `.syso` files in `cmd/deej/` when missing (idempotent `status:` check). Build tasks run from the repository root; `build/` contains configuration only.

`wails3 task dev` runs `wails3 dev` with `build/config.yml`. It starts Vite, builds the current platform's dev binary through `*:build:dev:live`, and runs it with `FRONTEND_DEVSERVER_URL` and `WAILS_VITE_PORT` set. Go changes trigger a rebuild; frontend changes use Vite HMR. The committed `frontend/dist/.gitkeep` lets Go embed the assets directory before the first frontend build.

Windows dev builds apply `-N -l` only to `github.com/nik9play/deej/...`; keep the standard library optimized to avoid the Windows arm64 `syscall.Syscall15` nosplit stack limit.

CI invokes the same Task targets directly; there is no separate shell or batch
build implementation to keep in sync.

### Cross-compilation and cgo

The **Windows** target and all **Linux** builds support cross-compilation with `CGO_ENABLED=0`. Select the architecture with `GOARCH`; all Linux compile tasks set `GOOS=linux` themselves, while the Windows tasks require `GOOS=windows` when invoked on another OS. CI builds Linux amd64 and arm64 GUI binaries on one runner. The **Linux GUI** build (`linux:build`) uses Wails' experimental `purego` backend (GTK4/WebKitGTK 6.0 by default), so compilation needs no C compiler, development headers or Docker. Target machines still need runtime libraries (`libgtk-4-1` / `libwebkitgtk-6.0-4` on Ubuntu). Use `EXTRA_TAGS=gtk3` with a Linux task to select GTK3/WebKit2GTK 4.1 instead.

deej also has a runtime headless switch (`DEEJ_NO_TRAY_ICON`) that skips the tray at startup, but that does **not** remove Wails or its GTK/WebKit runtime requirements — the `headless` build tag (see `cmd/deej/bootstrap_gui.go` / `cmd/deej/bootstrap_headless.go`) is what compiles Wails out entirely.

## Tests and Frontend Checks

Run from the repository root:

```bash
wails3 task test                 # CGO-free tests; adds -tags purego on Linux
npm --prefix frontend run check  # Svelte and TypeScript checks
```

For direct Linux tests use `CGO_ENABLED=0 go test -tags purego ./...`; CI installs GTK4/WebKitGTK runtime libraries, with no development headers. Before the first frontend check, install dependencies with `npm --prefix frontend ci` and run `wails3 task frontend:build` to generate the git-ignored Paraglide modules. After changing the Go service API or config model, regenerate bindings with `wails3 task generate:bindings`.

## Linting

Only use this command to lint the project:

```bash
golangci-lint run
```

Enabled linters: errcheck, govet, revive, staticcheck, unused

## Architecture

### Packages and dependency boundaries

- **cmd/deej/** owns CLI parsing, version linker variables, winres resources, and build-tagged GUI/headless bootstrap. Only the GUI bootstrap imports `internal/desktop`.
- **internal/app/** composes core services, localization, logging, crash reporting and lifecycle. It does not import Wails, desktop or frontend assets. Load initial settings and resolve language before creating consumers; register mixer and independent GUI subscriptions before starting serial/OBS/Discord.
- **internal/config/** owns the single `Settings` model (`model.go`, `defaults.go`, `normalize.go`, `mapping.go`, `legacy.go`) and store/watcher (`store.go`, `watch.go`). `Values()` publishes immutable snapshots atomically. Read fresh settings per operation; never retain a constructor snapshot. `SliderMappings.Get` returns copied targets. Store outcomes are localized by app callbacks.
- **internal/audio/** owns `Session`, `SessionFinder`, events and Windows Core Audio / Linux PulseAudio implementations. Session removal handlers run synchronously before resources are released. Master/system/mic identifiers belong here.
- **internal/serial/** separates connections/subscriptions (`client.go`), CRLF parsing/normalization (`protocol.go`), filtering (`noise.go`) and port enumeration (`ports.go`). It receives settings, logging, notification callbacks and verbose flag. Reload resets tracking so the next valid line re-emits every value without physical movement.
- **internal/mixer/** routes sliders to targets (`mixer.go`, `targets.go`) through injected interfaces. It reads `Values().ActiveMapping()` per event and refreshes the unmapped-session cache on reload. It owns `SessionInfo`; desktop converts it to a DTO.
- **internal/integrations/obs/** owns websocket connections and cubic/linear input-volume conversion.
- **internal/integrations/discord/** separates client lifecycle, RPC, auth/token persistence, roster, volume throttling and platform IPC. Its public volume API takes Discord-local targets; mixer parses config strings. App provides the token path and current mapped slider volumes for restoration.
- **internal/desktop/** owns Wails, the single settings window, tray, profile hotkeys, service, DTOs and GUI events. Preserve `deej:sliders`, `deej:state`, `deej:config`, `deej:sessions`, `deej:discord` payloads. Re-register shortcuts only when accelerators change. `tray.SetMenu` dispatches to the main thread internally: call it off the main thread when updating a running tray, then reapply the changed menu tree.
- **internal/platform/** owns foreground/fullscreen windows, process information/icons, autorun, signals and opening files. Low-level Windows APIs live in `internal/platform/win`.
- **internal/i18n/** owns detection/fallback and embedded `lang/active.{en,ru}.toml` catalogs.
- **internal/reconnect/** provides generation-scoped recovery for serial/OBS/Discord/PulseAudio. Start/Stop are serialized; Close must unblock Watch. Preserve dial parameters per generation and re-check config on adoption; stale errors must never end newer connections.
- **internal/atomicfile/** shares config/token writes: temporary file beside destination, mode 0600, rename and failure cleanup.
- **internal/notify/** and **internal/icon/** own notifications and embedded assets.

Configuration normalization stays private. GUI saves reject invalid values; file loads correct and log them. Preserve legacy keys, trailing-edge debounce, coalescing, serialized read-modify-write, unchanged-reload suppression and whole-file YAML regeneration.

Discord Windows IPC must use `winio.DialPipe` (overlapped); synchronous handles deadlock concurrent reads/writes. Authorize only through explicit `Link()`, never reconnect retries. Credentials are user-supplied; `discord_token.json` stays beside config, outside YAML. One event loop owns roster writes, with channel-scoped subscriptions and 30-second reconciliation. Read-loop event handoff is buffered/nonblocking because handlers issue RPC requests. Volumes dedupe and flush every 100 ms. Preserve replay/invalidation after connection, join/rejoin, channel changes and reconciliation even without slider movement. Participant conversion is cubic or linear 0-100; own input/output always use linear 0-100.

Each mixer/desktop consumer has a separate channel; sharing one distributes instead of broadcasting. Serial sends synchronously through unbuffered channels. Stop producers while consumers still receive the final serial state, then stop consumers and release audio. Repeated quit requests must not run duplicate Stop operations or close channels twice. The `headless` tag excludes Wails/desktop/frontend; `DEEJ_NO_TRAY_ICON` retains the GUI binary's runtime dependencies.

### Settings GUI (frontend/)

Svelte 5 + Vite + TypeScript, Tailwind CSS v4, bits-ui and `@lucide/svelte`. Mixer views/helpers live in `src/features/mixer/`; settings dialog and sections in `src/features/settings/`. Shared UI stays in `src/components/ui/`, Titlebar in `src/components/`. `src/lib/api.ts` is the single generated bindings/types access point; shared state, profiles and tick also live in lib. Window controls use Wails runtime directly.

Frontend output is embedded by `frontend/embed.go`. Keep `frontend/dist/.gitkeep` and Vite's recreation of it. Bindings live in `frontend/bindings/`. After public Go model/service package changes, run `wails3 task generate:bindings` scanning `cmd` and `internal` together, rebuild frontend and exercise real GetAppInfo/GetSettings/temporary-config SaveSettings calls. Fully qualified Go names determine `Call.ByID`: stale bindings can compile but fail at runtime.

Paraglide catalogs are `frontend/messages/{en,ru}.json`; configuration is `frontend/project.inlang/settings.json`. Vite generates ignored `src/paraglide/` modules; components call `m.<key>()`. Initialize app info, platform and locale before mount; `main.ts` mirrors Go language with `setLocale(..., { reload: false })`. Preserve `$state.snapshot`, subscribe-before-fetch, window cleanup, settings drafts across tabs, patch merging, saves against current config and pending-change flush on close.

### Config Format

`slider_mapping` maps slider indices (0-based) to targets inside each entry of the top-level `profiles` list. A profile has a `name`, an optional `hotkey` and its `slider_mapping`. Shared settings live at the top level, with connection settings grouped into nested `com:`, `obs:` and `discord:` sections:

```yaml
active_profile: default
profiles:
  - name: default
    slider_mapping:
      0: master           # Master volume
      1: discord.exe      # Process name (case-insensitive)
      2: deej.current     # Currently active window
      3: [spotify.exe, chrome.exe]  # Multiple targets
  - name: gaming
    hotkey: Ctrl+Alt+2    # global shortcut, switches to this profile from anywhere
    slider_mapping:
      0: master
invert_sliders: false
language: auto        # auto | en | ru
```

Only `slider_mapping` and `hotkey` are per-profile; `com`, `obs`, `discord`, `language`, `invert_sliders` and `noise_reduction` are shared. `normalize` guarantees at least one profile exists and that `active_profile` names one of them. A hotkey must have at least one modifier (a bare key would be grabbed system-wide). A config with a top-level `slider_mapping` and no `profiles:` still loads, as a single `default` profile, and is migrated on the next save.

Special targets: `master`, `system`, `mic`, `deej.current`, `deej.current.fullscreen`, `deej.unmapped`, `deej.obs:<inputName>` (OBS input volume), `deej.discord:<user>` (one person's volume in the current voice channel, matched case-insensitively against their display name, or a raw user ID), `deej.discord.input` and `deej.discord.output` (the user's own levels inside Discord).

Serial connection settings live in the `com:` section (OBS settings use an analogous `obs:` section; its `volume_conversion` defaults to true and selects cubic `position³` versus linear `position` input-volume multipliers):
```yaml
com:
  port: auto        # COM port name, or "auto" to detect by USB VID/PID
  baud_rate: 9600
  vid: 0x1A86       # USB IDs for auto-detection (default: CH340 chip)
  pid: 0x7523
```

The legacy flat keys (`com_port`, `baud_rate`, `com_vid`, `com_pid`) are still read for backwards compatibility, but new configs and GUI saves use the `com:` section only.

Discord settings live in a `discord:` section holding `enabled`, `client_id`, `client_secret` and `volume_conversion` — the OAuth token that linking produces is stored separately in `discord_token.json`, so backend writes never race the GUI's whole-document saves:
```yaml
discord:
  enabled: false
  client_id: ""      # from an application you create at discord.com/developers
  client_secret: ""
  volume_conversion: true
```

Discord `volume_conversion` applies only to participant targets (`deej.discord:<user>`): true sends `position³ × 100` to match Discord's displayed curve, while false sends `position × 100`. The input and output targets always use the linear 0–100 scale.

The config can be edited by hand (hot-reloaded) or through the settings GUI, which writes the same file.

### Data Flow

1. Arduino sends analog readings over serial as pipe-delimited values
2. `serial.Client.watch` reads serial lines; `serial.Client.handleLine` validates them against `^\d{1,4}(\|\d{1,4})*\r\n$`, normalizes values, applies inversion and filters noise
3. Slider move events are emitted to `mixer.Mixer` (and to the tray/GUI as live events)
4. `mixer.Mixer.handleSliderMoveEvent` resolves targets and adjusts volumes
