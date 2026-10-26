---
title: Build from source
description: Build and run Pelton from source on macOS, Windows, or Linux.
---

# Build from source

Pelton is Go + [Wails](https://wails.io) on the backend, Svelte + TypeScript
on the frontend. The `Makefile` at the repo root is the source of truth for
build/run/package commands; this page just walks through using it.

## Checklist
<div class="checklist" markdown>
- [ ] Install the prerequisites
- [ ] Clone the repo
- [ ] Run it in dev mode, or build a release binary
</div>

## Prerequisites

- [Go](https://go.dev) 1.27.1 or newer
- [Node.js](https://nodejs.org) and [pnpm](https://pnpm.io) (the frontend uses pnpm, not npm)
- The Wails CLI, installed at the exact version this repo pins in `go.mod`:

    ```bash
    go install github.com/wailsapp/wails/v2/cmd/wails@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)
    ```

    Installing a different Wails CLI version than the one pinned in
    `go.mod` can rewrite `go.mod` and the generated TypeScript bindings out
    from under you, the command above sidesteps that by matching them.

Platform-specific toolchains, only needed for the platform you're building
*for*:

- **Linux**: the GTK/WebKitGTK development packages Wails needs (see the
  [Wails Linux guide](https://wails.io/docs/gettingstarted/installation#linux)
  for your distro's package names).
- **Windows**: WebView2 (preinstalled on most modern Windows) and, if you
  want to build the installer, [NSIS](https://nsis.sourceforge.io/).
- **macOS**: Xcode command line tools. Building the `.dmg` also needs
  [`create-dmg`](https://github.com/sindresorhus/create-dmg)
  (`brew install create-dmg`).

Cross-compiling (building a Windows binary from macOS, for example) needs
the target platform's toolchain too, and isn't always practical. Building
on the target platform gets you a cleaner build.

- [x] Install the prerequisites

## Clone the repo

```bash
git clone https://github.com/peltonapp/Pelton.git
cd Pelton
```

- [x] Clone the repo

## Running in dev mode

```bash
make run
```

This installs Go and pnpm dependencies, regenerates the TypeScript bindings
from the Go methods, and launches `wails dev` with hot reload for both the
Go backend and the Svelte frontend. It points Pelton at an isolated
`PELTON_DEV` config/database directory, so it never touches a real
install's accounts, mail cache, or settings.

## Building a release binary

```bash
make build-mac    # macOS, needs macOS
make build-win    # Windows, needs Windows for a clean build
make build-linux  # Linux
```

On macOS the `Makefile` sets the macOS 13 deployment target for the cgo code
(`CGO_CFLAGS`/`CGO_LDFLAGS` with `-mmacosx-version-min=13.0`); set those by
hand if you run `wails build` directly.

Each produces a binary under `build/bin/`. A couple of platforms have an
extra packaging step:

- **macOS**: `make dmg` (builds first, then packages `build/bin/Pelton.dmg`
  with a drag-to-Applications drop link).
- **Windows**: `wails build -platform windows/amd64 -nsis -ldflags "..."`
  produces an NSIS installer instead of a bare `.exe` (the `Makefile`'s
  `build-win` target builds the bare `.exe`; add `-nsis` yourself for an
  installer, matching what the release workflow does).
- **Linux**: `build-linux` also copies `build/linux/pelton.desktop` next to
  the binary in `build/bin/`. Install the binary somewhere on your `PATH`,
  install the `.desktop` file to `~/.local/share/applications/`, and give
  it an icon named `pelton` (see `build/icons/`).

- [x] Run it in dev mode, or build a release binary

## Need help?

See [Support](../support.md).
