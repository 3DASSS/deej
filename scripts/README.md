## Build automation

Builds are defined in the repository Taskfiles and can be run through either
the Wails CLI (`wails3 task <name>`) or the standalone Task CLI (`task <name>`).
Run `wails3 task --list` from the repository root to see all available tasks.

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

Linux builds use cgo (the [Wails](https://wails.io) GTK backend for the tray and settings window) and need GTK4/WebKitGTK headers, e.g. on Ubuntu 24.04+: `sudo apt-get install libgtk-4-dev libwebkitgtk-6.0-dev`. For older distros that lack `webkitgtk-6.0`, build with `-tags gtk3` and `libgtk-3-dev libwebkit2gtk-4.1-dev` instead.

Windows binaries are cgo-free, so Task can cross-compile them (e.g. `GOOS=windows GOARCH=arm64 wails3 task windows:build`). Linux targets must be built natively on a matching architecture; CI uses a native arm64 runner for the arm64 build.
