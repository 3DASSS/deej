## Build automation

Builds are defined in `Taskfile.yml` and `build/tasks/{windows,linux}.yml` and can be run through either
the Wails CLI (`wails3 task <name>`) or the standalone Task CLI (`task <name>`).
Run `wails3 task --list` from the repository root to see all available tasks.
Build configuration stays in `build/`; binaries/installers go to `bin/`.
The entry point and winres inputs are in `cmd/deej/`; embedded icons live in
`internal/icon/assets/`.

The files remaining under `scripts/` are inputs to build tasks rather than
executable build wrappers:

- [`windows/installer.iss`](./windows/installer.iss) defines the Inno Setup package.
- [`misc/release-notes.txt`](./misc/release-notes.txt) is a release-note template.

Common commands:

```sh
wails3 task build:all                    # release + development binaries for this OS
wails3 task windows:package              # Windows release binary + single-arch installer
wails3 task windows:package:universal    # installer from prebuilt amd64 + arm64 binaries
wails3 task generate:resources           # regenerate Windows icon/manifest resources
```

### Build requirements

All GUI build tasks build the settings frontend first, which requires [Node.js](https://nodejs.org) (see [`frontend/`](../frontend)).

Linux GUI builds use the experimental [Wails purego backend](https://github.com/wailsapp/wails/blob/v3.1.0/v3/pkg/application/PUREGO_LINUX.md) with `CGO_ENABLED=0`. No C compiler, GTK/WebKit development headers or Docker are needed to build. The target machine still needs GTK4/WebKitGTK runtime libraries, e.g. on Ubuntu 24.04+: `sudo apt-get install libgtk-4-1 libwebkitgtk-6.0-4`. For older distros that lack `webkitgtk-6.0`, use `wails3 task linux:build EXTRA_TAGS=gtk3` and install `libgtk-3-0 libwebkit2gtk-4.1-0` on the target instead.

Windows and Linux binaries are CGO-free and cross-compilable. Select the architecture with `GOARCH`, e.g. `GOARCH=arm64 wails3 task linux:build:all`. All Linux tasks set `GOOS=linux`; Windows tasks need `GOOS=windows` when invoked on another OS. CI builds Linux amd64 and arm64 GUI binaries on one runner. The Linux headless build (`linux:build:headless`) omits Wails entirely and needs no GTK/WebKit runtime libraries.

`wails3 task test` disables CGO and selects `purego` on Linux. To run Linux tests directly, use `CGO_ENABLED=0 go test -tags purego ./...`.
