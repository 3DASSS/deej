# deej settings frontend

Svelte 5 + Vite frontend for the settings window, embedded into the deej binary via `go:embed` (see `embed.go`). The build scripts in [`scripts/`](../scripts) run `npm ci` and `npm run build` automatically.

## Development loop

For live-reload development of the frontend:

```
cd frontend
npm run dev
```

Then run deej with the dev server URL so the settings window loads from Vite instead of the embedded assets:

```
FRONTEND_DEVSERVER_URL=http://127.0.0.1:9245 ./build/deej-dev
```

(On Windows: `set FRONTEND_DEVSERVER_URL=http://127.0.0.1:9245` before starting `deej-dev.exe`.)

## Regenerating bindings

The TypeScript bindings in `bindings/` are generated from the Go `SettingsService` and are committed, so plain `go build` and CI never need the generator. Regenerate them after changing the service surface:

```
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117 generate bindings -ts -clean -d frontend/bindings ./pkg/deej
```

Keep the CLI version pinned to the `github.com/wailsapp/wails/v3` version in `go.mod`.
