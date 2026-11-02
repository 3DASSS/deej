# AGENTS.md

This file provides repository instructions for coding agents working in this project.

## Project Overview

deej is a hardware volume mixer client written in Go that pairs with an Arduino to provide physical volume control for Windows and Linux. It communicates with Arduino hardware via serial port and controls per-application audio volumes through OS-specific audio APIs.

This is a fork of the original deej project (github.com/omriharel/deej) with automatic COM port reconnection, autorun support, updated dependencies, OBS websocket and Discord RPC integrations, i18n support, and a Wails-based settings GUI with profiles and global hotkeys.

## Build Commands

Builds are orchestrated by [Task](https://taskfile.dev) (`Taskfile.yml` plus `Taskfile.{windows,linux}.yml`) — the same build system that underpins `wails3 build`. The GUI `build`, `build:dev` and `build:all` tasks compile the embedded Svelte frontend before the Go binary, so Node.js and npm are required. They run `npm ci` only when `frontend/node_modules` is absent, then `npm run build`; after dependency or lockfile changes, run `npm ci` in `frontend/` explicitly. The Linux headless build needs neither Node.js nor frontend assets. The `*:build:dev:live` tasks skip the frontend build because Wails dev mode serves it from Vite. Run tasks with `wails3 task <name>` — the wails3 CLI embeds the Task runner and is what's installed here (the standalone `task` binary also works if present, but don't assume it). Binaries go to `build/`; frontend assets go to `frontend/dist/`.

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

The Windows `build` task runs `go generate` for the winres icon/manifest `.syso` files when they're missing (idempotent `status:` check).

`wails3 task dev` runs `wails3 dev` with `build/config.yml`. It starts Vite, builds the current platform's dev binary through `*:build:dev:live`, and runs it with `FRONTEND_DEVSERVER_URL` and `WAILS_VITE_PORT` set. Go changes trigger a rebuild; frontend changes use Vite HMR. The committed `frontend/dist/.gitkeep` lets Go embed the assets directory before the first frontend build.

Windows dev builds apply `-N -l` only to `github.com/nik9play/deej/...`; keep the standard library optimized to avoid the Windows arm64 `syscall.Syscall15` nosplit stack limit.

CI invokes the same Task targets directly; there is no separate shell or batch
build implementation to keep in sync.

### Cross-compilation and cgo

The **Windows** target and all **Linux** builds support cross-compilation with `CGO_ENABLED=0`. Select the architecture with `GOARCH`; all Linux compile tasks set `GOOS=linux` themselves, while the Windows tasks require `GOOS=windows` when invoked on another OS. CI builds Linux amd64 and arm64 GUI binaries on one runner. The **Linux GUI** build (`linux:build`) uses Wails' experimental `purego` backend (GTK4/WebKitGTK 6.0 by default), so compilation needs no C compiler, development headers or Docker. Target machines still need runtime libraries (`libgtk-4-1` / `libwebkitgtk-6.0-4` on Ubuntu). Use `EXTRA_TAGS=gtk3` with a Linux task to select GTK3/WebKit2GTK 4.1 instead.

deej also has a runtime headless switch (`DEEJ_NO_TRAY_ICON`) that skips the tray at startup, but that does **not** remove Wails or its GTK/WebKit runtime requirements — the `headless` build tag (see `tray.go` / `tray_headless.go`) is what compiles Wails out entirely.

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

### Core Components (pkg/deej/)

- **deej.go** - Main application orchestrator. Creates and manages all components (config, serial, sessions, OBS, Discord, tray), handles i18n setup, runs the main event loop
- **serial.go** - Arduino communication via serial port (`go.bug.st/serial`). Parses slider values (format: `val1|val2|val3\r\n`), auto-detects COM ports by VID/PID, handles reconnection via `pkg/reconnect`
- **session_map.go** - Maps slider IDs to audio sessions. Handles special targets like `master`, `system`, `mic`, `deej.current`, `deej.unmapped`, OBS inputs (`deej.obs:<inputName>`) and Discord targets. Reads `config.Values().ActiveMapping()` fresh on every slider event rather than caching the mapping. Config reloads rebuild the cached unmapped-session list and reset serial slider tracking, so the next valid serial line re-emits all slider values with the new mapping, even without a physical slider move
- **config.go** - Config lifecycle: YAML loading (`go.yaml.in/yaml/v3`) with hot-reload via a self-managed fsnotify watcher (trailing-edge debounce), plus GUI save (`SaveUserSettings` → `normalize` → atomic write → reload). Publishes an immutable `*Settings` snapshot atomically on every (re)load (read via `Values()`); reload signals to consumers are buffered and coalesced. A reload that produces an unchanged snapshot skips the toast/notify (so a GUI save's own file event, and comment-only hand edits, are no-ops)
- **config_model.go** - The config schema. `Settings` is the single source of truth (yaml tags = file keys, json tags = GUI wire format). Adding a setting = one field here (+ default in `defaultSettings`, + a rule in `normalize` if it needs validation) + a frontend control. `normalize` is the single rule set behind both policies: it canonicalizes in place and returns the list of invalid values — GUI saves reject when that list is non-empty, file loads apply the fixes and log them. `applyLegacyKeys` reads the legacy flat keys — the com ones (`com_port`, `baud_rate`, `com_vid`, `com_pid`) and the pre-profiles top-level `slider_mapping` — before the main decode, so an explicit `com:`/`profiles:` section wins; saves only ever write the new form. Slider mappings live on `Profile`, one per named profile, with `Settings.ActiveMapping()` resolving the active one; `SliderMappings` owns `get`, `clone` and `normalized`. Saves fully regenerate the file (comments, key order and unknown keys are not preserved)
- **settings_service.go** - Wails service (`SettingsService`) exposing config, serial-port enumeration, live status, and audio-session info to the settings frontend
- **obs.go** - OBS websocket integration (`goobs`) for controlling OBS input volumes; reconnects via `pkg/reconnect`
- **discord.go** + **discord_ipc_{windows,other}.go** - Discord integration over the client's local RPC connection, for per-user voice volumes and the user's own input/output levels. The IPC frame codec (8-byte LE header + JSON) and the OAuth token exchange are hand-rolled on stdlib. The transport is the one platform split: on Windows the pipe **must** be opened with `winio.DialPipe` (overlapped), not `os.OpenFile` — a synchronous handle serializes I/O, so the read loop's blocking `ReadFile` deadlocks any concurrent write and every command hangs forever; elsewhere it's a plain unix socket. Reconnects via `pkg/reconnect`. Discord gates the `rpc.voice.*` scopes to an application's owner, so credentials are user-supplied (`discord.client_id`/`client_secret`) rather than shipped. The reconnect loop only ever does the silent path; the interactive `AUTHORIZE` lives in `Link()` behind a GUI button, so retries can't spawn consent popups. The resulting token is persisted in `discord_token.json` next to the config, deliberately not inside it. Volume commands are throttled (dedupe + 100ms flush) to stay under Discord's RPC rate limit during a slider sweep. The voice-channel roster is kept live by `VOICE_STATE_CREATE/UPDATE/DELETE` subscriptions (re-pointed on `VOICE_CHANNEL_SELECT`, since they're channel-scoped) with a 30s reconcile as the safety net for dropped events; a single `eventLoop` goroutine owns every roster write, and roster changes reach the GUI as a `deej:discord` event rather than by polling. Events are handed to that loop through a buffered channel and dropped when it's full — the read loop must never block, because the event handler issues requests that same loop has to answer
- **tray.go** - System tray icon and settings window, built on **Wails v3** (`wailsapp/wails/v3`). Hosts the frontend assets, registers the Wails service, and pushes live events to the settings window (`deej:sliders`, `deej:state`, `deej:config`, `deej:sessions`, `deej:discord`). Also owns the Profiles submenu (rebuilt on every config reload) and the per-profile global hotkeys (`app.GlobalShortcut`, re-registered only when the accelerator set actually changes). Lives only in the non-headless build, so headless daemons have no hotkeys

### Settings GUI (frontend/)

A **Svelte 5 + Vite + TypeScript** single-page app, styled with **Tailwind CSS v4** and **bits-ui** components (icons from `@lucide/svelte`). It renders a live mixer plus a settings dialog with a frameless titlebar. Built to `frontend/dist` and embedded into the Go binary via `frontend/embed.go`. Wails-generated Go↔TS bindings live in `frontend/bindings/`. Frontend i18n is separate from the Go i18n and uses **Paraglide JS** (`@inlang/paraglide-js`): message catalogs live in `frontend/messages/{en,ru}.json`, configured by `frontend/project.inlang/settings.json`, and the Vite plugin compiles them to typed message functions under `frontend/src/paraglide/` (git-ignored, regenerated on build). Components call `m.<key>()` from `../paraglide/messages`; `main.ts` mirrors the Go-resolved language into Paraglide's in-memory locale via `setLocale(..., { reload: false })` (the `globalVariable` strategy).

### Platform-Specific Code

Audio session management is platform-specific:
- **session_finder_windows.go** - Uses Windows Core Audio API (go-wca) to enumerate and control audio sessions
- **session_finder_linux.go** - Uses PulseAudio (jfreymuth/pulse)
- **session_windows.go / session_linux.go** - Platform-specific session implementations

Window detection for `deej.current` targets:
- **pkg/deej/util/util_windows.go** - Win32 API calls for foreground/fullscreen window detection
- **pkg/win/syscalls_windows.go** - Low-level Windows syscall definitions

### Supporting Packages

- **pkg/reconnect/** - Generic reconnection lifecycle manager shared by the serial, OBS, Discord, and PulseAudio connections (dial → watch → reconnect on failure). See the package doc for the callback contracts
- **pkg/notify/** - Toast notifications (Windows) and libnotify (Linux)
- **pkg/icon/** - Embedded tray icon assets (`assets/tray-icon.ico` on Windows, `assets/logo.png` on Linux)

### Internationalization

Go-side i18n uses `nicksnyder/go-i18n` with TOML message catalogs embedded from `pkg/deej/lang/` (`active.en.toml`, `active.ru.toml`). Language resolves from config, falling back to auto-detection; the resolved language is exposed to the GUI.

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
2. `SerialIO.watch` reads serial lines; `SerialIO.handleLine` validates them against `^\d{1,4}(\|\d{1,4})*\r\n$`, normalizes values, applies inversion and filters noise
3. Slider move events are emitted to `sessionMap` (and to the tray/GUI as live events)
4. `sessionMap.handleSliderMoveEvent` resolves targets and adjusts volumes
