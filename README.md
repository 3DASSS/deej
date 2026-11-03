# deej

This is a fork of the [original project](https://github.com/omriharel/deej).

## Features

This fork includes the following new features:
- Automatic reconnection to the COM port
- Option to enable autorun
- Updated dependencies
- Support for adding translations into different languages (currently, Russian translation is available)


## Development

Use the Go version in `go.mod`, Node.js/npm for GUI builds, and the Wails v3 CLI.
Run from the repository root:

```sh
npm --prefix frontend ci
wails3 task frontend:build
wails3 task dev
```

Build with `wails3 task windows:build:all`, `wails3 task linux:build:all`, or
`wails3 task linux:build:headless`. Binaries/installers go to `bin/`;
`build/config.yml` and `build/tasks/` contain build/development configuration.
Linux builds are CGO-free; GUI binaries need GTK/WebKit runtime libraries.
The headless target needs neither Node.js nor frontend assets.

Run `wails3 task test`, `npm --prefix frontend run check`, and `golangci-lint run`.
Regenerate bindings with `wails3 task generate:bindings` after service/model changes.
The entry point is `cmd/deej`, core packages are in `internal/`, and Wails is
isolated in `internal/desktop`. Frontend features access generated bindings through
`frontend/src/lib/api.ts`. See [architecture](docs/architecture.md),
[build tasks](scripts/README.md), and [repository instructions](AGENTS.md).
