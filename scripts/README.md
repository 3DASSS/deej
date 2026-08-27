## Developer scripts

This document lists the various scripts in the project and their purposes.

> Note: All scripts are meant to be run from the root of the repository, i.e. from the _root_ `deej` directory: `.\pkg\deej\scripts\...\whatever.bat`. They're not guaranteed to work correctly if run from another directory.

### Windows

- [`build-dev.bat`](./windows/build-dev.bat): Builds deej with a console window, for development purposes
- [`build-release.bat`](./windows/build-release.bat): Builds deej as a standalone tray application without a console window, for releases
- [`build-all.bat`](./windows/build-all.bat): Helper script to build all variants
- [`make-icon.bat`](./windows/make-icon.bat): Converts a .ico file to an icon byte array in a Go file. Used by our systray library. You shouldn't need to run this unless you change the deej logo
- [`make-rsrc.bat`](./windows/make-rsrc.bat): Generates `rsrc_windows_<arch>.syso` resource files inside `cmd` alongside `main.go` (via [go-winres](https://github.com/tc-hib/go-winres), configured in `cmd/winres/winres.json`) - These indicate to the Go linker to use the deej application manifest and icon when building.
- [`prepare-release.bat`](./windows/prepare-release.bat): Tags, builds and renames the release binaries in preparation for a GitHub release. Usage: `prepare-release.bat vX.Y.Z` (binaries will be under `releases\vX.Y.Z\`)

### Linux

- [`build-dev.sh`](./linux/build-dev.sh): Builds deej for development purposes
- [`build-release.sh`](./linux/build-release.sh): Builds deej for releases
- [`build-all.sh`](./linux/build-all.sh): Helper script to build all variants

### Build requirements

All build scripts build the settings GUI frontend first, which requires [Node.js](https://nodejs.org) (see [`frontend/`](../frontend)).

Linux builds use cgo (the [Wails](https://wails.io) GTK backend for the tray and settings window) and need GTK4/WebKitGTK headers, e.g. on Ubuntu 24.04+: `sudo apt-get install libgtk-4-dev libwebkitgtk-6.0-dev`. For older distros that lack `webkitgtk-6.0`, build with `-tags gtk3` and `libgtk-3-dev libwebkit2gtk-4.1-dev` instead.

Windows binaries are cgo-free, so the shell scripts can still cross-compile them (e.g. `GOOS=windows GOARCH=arm64 ./scripts/linux/build-release.sh`). Linux targets must be built natively on a matching architecture; CI uses a native arm64 runner for the arm64 build.
