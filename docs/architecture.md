# Architecture

The application remains one Go module. `cmd/deej` owns CLI flags, build metadata,
Windows resources and GUI/headless bootstrap. Binaries and installers are output
to `bin/`; `build/config.yml` and `build/tasks/` are source configuration.

## Core and desktop

`internal/app` composes configuration, audio discovery, serial transport, mixer,
OBS and Discord. Dependencies are passed to constructors through consumer-owned
interfaces and callbacks. Config, audio, serial and integrations have no dependency
on app, mixer or desktop. App does not import Wails or frontend assets.

`internal/config.Settings` is the only settings model for YAML, runtime and GUI.
The store publishes immutable snapshots; consumers obtain current settings per
operation. Normalization, migrations and copying remain private. GUI saves reject
invalid settings, file loads correct them. Atomic writes, hot reload debounce,
coalescing and unchanged-reload suppression are shared behavior across profiles
and settings saves. `internal/atomicfile` is also used for Discord token storage.

`internal/audio` owns audio session interfaces and synchronous lifetime events.
`internal/serial` owns protocol, inversion/noise filtering and port recovery.
`internal/mixer` resolves configuration targets and controls audio/integrations
using narrow interfaces. Audio session information belongs to mixer/audio;
Discord roster/status information belongs to Discord. Desktop converts core
information into DTOs without introducing another settings model.

OBS and Discord live under `internal/integrations/`. Mixer parses target prefixes;
Discord receives integration-local keys. App assembles the current mapping and
slider positions for Discord restoration after reconnects, participant changes
and reconciliation. Tokens stay beside the selected config and outside YAML.

`internal/desktop` owns Wails, service bindings, window, tray, events and profile
hotkeys. GUI bootstrap imports it; headless bootstrap does not. The `headless`
build tag removes Wails and embedded frontend dependencies. The runtime
`DEEJ_NO_TRAY_ICON` option skips the tray while retaining GUI-binary dependencies.

Platform process/window/autorun/file-opening operations live in
`internal/platform`, with low-level Windows APIs in `internal/platform/win`.
`internal/i18n`, `notify`, `icon` and `reconnect` own translations, notifications,
assets and connection recovery respectively. Windows toast registration and sends are
serialized on a dedicated locked OS thread: the toast library caches WinRT
initialization globally, while COM apartments belong to threads. Do not run
its Push on Wails' STA thread or move later sends between Go worker threads.

## Startup and shutdown

1. Load initial configuration and resolve the language.
2. Register mixer and finder handlers before discovery. Register independent GUI
   subscriptions before starting producers; GUI setup occurs in ApplicationStarted.
3. Start serial, OBS, Discord and configuration watching.
4. Coalesce repeated quit requests. Stop producers while consumers continue to
   receive synchronous slider and final serial state events.
5. Stop consumer loops, release audio/finder resources, close GUI and sync logs.

Each event consumer has its own subscription channel. Sharing a channel would
split delivery instead of broadcasting. Connection Start/Stop operations are
serialized. Each generation retains its dial parameters, has its own failure
channel, and closes its transport to unblock Watch. Late failures from an old
connection cannot tear down the current one.

## Frontend and generated API

Mixer components and target helpers live in `frontend/src/features/mixer`;
settings dialog/sections live in `features/settings`. Shared UI stays in
`components/ui`, Titlebar in `components`, and state/profiles/tick in `lib`.
`lib/api.ts` centralizes generated Go API/type imports.

Generate all bindings together with `wails3 task generate:bindings`, then rebuild
frontend assets. Numeric `Call.ByID` identifiers depend on fully qualified Go
method names, so a package move requires regenerated bindings and real service
calls even when TypeScript compiles. Preserve app-info/locale/platform setup
before mount, subscribe-before-fetch ordering, snapshot conversion and cleanup.

See [AGENTS.md](../AGENTS.md) for contracts and [verification evidence](architecture-refactor-verification.md)
for completed checks and remaining manual scenarios.
