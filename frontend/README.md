# deej settings frontend

Svelte 5 + Vite settings UI, embedded through `embed.go`. GUI build tasks install
missing dependencies and compile `dist/`; after lockfile changes run
`npm --prefix frontend ci` explicitly.

## Development

From the repository root:

```sh
npm --prefix frontend ci
wails3 task frontend:build
wails3 task dev
```

Wails starts Vite first, builds `bin/deej-dev` (`.exe` on Windows), and supplies
`FRONTEND_DEVSERVER_URL` and `WAILS_VITE_PORT`. Frontend edits use HMR; Go edits
rebuild the binary without rerunning the frontend production build.

For standalone Vite development, run `npm --prefix frontend run dev` and start
the development binary with `FRONTEND_DEVSERVER_URL=http://127.0.0.1:9245`.
Keep `dist/.gitkeep` so embedding works before the first production build.

## Source layout

- `src/features/mixer/`: mixer, slider columns, target dialog and target helpers.
- `src/features/settings/`: settings dialog and `sections/`.
- `src/components/`: Titlebar and shared `ui/` components.
- `src/lib/`: state, profiles, tick and `api.ts` for Go bindings/types.
- `messages/`: Paraglide catalogs; generated `src/paraglide/` is ignored.

Initialize app info, platform and locale before mount. Subscribe before fetching
initial state and clean up on close. Preserve drafts across tabs and flush edits.

## Generated bindings and checks

Bindings in `bindings/` are committed. After changing `internal/desktop` service
APIs or `internal/config` models, run from the root:

```sh
wails3 task generate:bindings
wails3 task frontend:build
npm --prefix frontend run check
```

The generator scans `cmd` and `internal` together because it cleans its output.
Import APIs only through `src/lib/api.ts`. Package moves alter `Call.ByID` IDs:
verify real GetAppInfo/GetSettings/temporary-config SaveSettings calls afterwards.
Keep Wails CLI and `@wailsio/runtime` aligned with `go.mod`.
