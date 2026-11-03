# Architecture refactor verification

No commits are created as part of this working-tree refactor. The input plan is
`architecture-refactor-plan.md`; this document records evidence separately.

## Baseline before package moves

- Windows Go tests: passed (`wails3 task test`). A first attempt overlapped npm ci
  and encountered a transient removed node_modules path; rerun after install passed.
- Frontend production build and Svelte/TypeScript check: passed.
- Windows GUI release, Linux GUI release and Linux headless release builds: passed
  through their existing Task targets.
- Lint: blocked before refactoring because installed golangci-lint was built with
  Go 1.25 while `go.mod` targets Go 1.26.10. This is an existing tool mismatch.

## Package checks

- Configuration migration, normalization, YAML round-trip, watcher/reload and
  settings validation tests moved with config and passed.
- Added checks for non-mutating settings saves, safe mapping lookup and idempotent
  watcher stop; these passed.
- Atomic writes tested for destination replacement, owner permissions on Unix,
  and temporary-file cleanup after failed rename; passed on Windows (Unix mode
  assertion requires Unix execution).
- Shared reconnector tests and concurrent lifecycle/idempotency test passed.
- Supporting packages compile on Windows: platform, i18n, notify and icon.

## Final automated verification

- Windows `wails3 task test`: passed.
- Linux Ubuntu 24.04 / WSL `wails3 task test`: passed with Go 1.26.10 and Wails 3.1.0.
- Windows core/app/desktop/integration race checks and Linux
  `CGO_ENABLED=1 go test -race -tags purego ./internal/...`: passed.
- `golangci-lint run`: 0 issues using a Go 1.26-compatible golangci-lint 2.15.0
  built under the temporary tools directory. The globally installed 2.5.0 remains
  unchanged; application dependencies and lockfiles are unchanged.
- Bindings regenerated together from `./cmd/... ./internal/...`; obsolete
  `pkg/deej` bindings removed. Frontend production build and Svelte/TypeScript
  checks passed. Generated bindings are accessed through `lib/api.ts`.
- Windows-only dispatcher test parses every numeric ID from the generated
  SettingsService module, resolves each against Wails' actual Bindings registry,
  and calls GetAppInfo, GetSettings and SaveSettings through its JSON dispatcher.
  Legacy YAML is loaded and saved to a temporary config; invalid save followed by
  a corrected retry is covered. This test is Windows-only because constructing a
  standalone Wails App before its GTK backend is initialized fails on Linux.
- Public Task builds: Windows release/dev; Linux GUI release/dev and headless;
  amd64 and arm64; Linux GUI GTK3 release/dev on both architectures: passed.
  Release metadata, production/windowsgui flags and package-scoped Windows dev
  flags were inspected. Windows arm64 dev build passed.
- Windows resources regenerated from `cmd/deej/winres`; icon embeds and both
  architecture resources resolve. Platform Taskfiles are not ignored; a temporary
  Git index verified they are included with the source changes. No actual index
  staging or commits were performed.
- Ordinary and universal Inno Setup targets passed. Installers were built but not
  installed. Build output is under `bin/`, build configuration under `build/`.
- Linux headless Task build passed with `frontend/dist` temporarily absent and the
  Node installation directory removed from PATH. Assets were restored afterwards.
  Its dependency graph includes neither Wails, desktop nor frontend.
- `git diff --check` and stale-path searches passed (the supplied historical
  refactoring plan intentionally still describes the old package paths).

Final task/test/lint logs are in ignored `bin/verification/`. Preserved installers
are `bin/deej-setup-amd64.exe` and `bin/deej-setup-universal.exe`; portable Windows
outputs are `bin/deej-windows-{amd64,arm64}{,-dev}.exe`.

## Runtime smoke verification

All runtime tests used temporary config files with empty slider mappings and an
unavailable serial port; the user's installed deej instance/config was untouched.
The GUI smoke binary temporarily added automatic settings-window opening and a
quit shortcut to make the test window reachable. These instrumentation changes
were restored before the final build; no test-only behavior remains in source.

- Windows embedded frontend: mixer loaded; English and Russian startup locale,
  Windows styling, version and selected config path displayed correctly.
- SettingsService calls ran through the actual embedded webview. Toggling slider
  inversion persisted the temporary configuration in the canonical YAML format.
- OBS draft survived switching to About and back; Revert cleared the draft.
  Settings dialog closed and reopened successfully.
- A hand edit changed the active profile while settings stayed open. The GUI
  updated, the tray menu was reapplied without deadlock, and a newly configured
  global hotkey switched back and persisted the profile.
- The test GUI's quit action reached the normal RequestStop path: serial, OBS,
  Discord and the audio finder stopped and the process exited.
- Linux headless runtime connected to PulseAudio, retried an unavailable device,
  handled SIGTERM, stopped integrations and exited with status 0.
- Wails dev started Vite first, then built and ran `bin/deej-dev.exe` with the
  devserver environment. A CSS edit emitted a Vite HMR update. A Go source edit
  rebuilt/restarted using `windows:build:dev:live`, without npm production build.
  Test edits were restored; owned dev/Vite processes and temporary bin config
  were cleaned up. Browser-visible HMR rendering was not separately inspected.

## Remaining device/service scenarios

These require physical hardware, authenticated integrations or a Linux GUI
runtime and are not claimed as passed:

- Real Arduino disconnect/reconnect and volume changes; serial Quit while a real
  active device is delivering values (covered with fake ports/events in tests).
- Real OBS connection and Discord authorization/token persistence; participant
  join/rejoin and channel changes. Framed RPC tests cover restoration, throttle,
  curves, dedupe invalidation and stale in-flight generation handling.
- Linux GTK4/GTK3 GUI rendering: WSL lacks WebKitGTK/JavaScriptCore runtime libs.
- Full GUI matrix: whole-window close/reopen from tray, rapid edits followed by
  whole-window close, notification appearance, duplicate-subscription inspection
  and native tray Quit with an active serial device.
- A literal fresh Git checkout was not created because the requested changes are
  intentionally uncommitted. Taskfile inclusion/ignore rules and clean headless
  conditions were checked separately.

## Follow-up: Windows toast COM apartment conflict

A missing-config startup toast exposed RPC_E_CHANGED_MODE (RoInitialize) when
sent from a thread already initialized as STA. Windows notifications now use one
lazy, process-lifetime goroutine locked to an OS thread; registration and every
Push are serialized there because the library caches initialization globally.

- Regression tests establish STA on the caller, initialize MTA on the worker,
  verify thread identity/apartment across later requests, and exercise concurrent
  calls and error propagation. `go test -race ./internal/notify` passed.
- `wails3 task test` and `golangci-lint run` passed.
- The Windows GUI release binary was started without computer use with an
  explicitly missing temporary config. The missing-config/fatal path ran and
  exited promptly; no RoInitialize/toast error was logged. Visual toast appearance
  was not inspected. Logs are `bin/verification/notify-fix-*.log`.
