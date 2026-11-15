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

- [Go](https://go.dev) 1.27 or newer
- [Node.js](https://nodejs.org) and [pnpm](https://pnpm.io) (the frontend uses pnpm, not npm)

No Wails CLI install is needed. The build and the binding generator run
through `go run` at the version pinned in `go.mod`.

Platform-specific toolchains, only needed for the platform you're building
*for*:

- **Linux**: the GTK 3 and WebKitGTK 4.1 development packages, e.g.
  `libgtk-3-dev libwebkit2gtk-4.1-dev` on Debian/Ubuntu or `gtk3-devel
  webkit2gtk4.1-devel` on Fedora.
- **Windows**: WebView2 (preinstalled on most modern Windows) and, if you
  want to build the installer, [NSIS](https://nsis.sourceforge.io/).
- **macOS**: macOS 13 Ventura or newer and the Xcode command line tools. Building the `.dmg` also needs
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
from the Go methods, and starts the Vite dev server (hot reload for the
Svelte frontend) alongside the app. Go changes need `make run` restarted.
It points Pelton at an isolated `PELTON_DEV` config/database directory, so
it never touches a real install's accounts, mail cache, or settings.

## Building a release binary

```bash
make build-mac    # macOS, needs macOS
make build-win    # Windows, needs Windows for a clean build
make build-linux  # Linux
```

Each produces a binary under `build/bin/`. A couple of platforms have an
extra packaging step:

- **macOS**: `make dmg` (builds first, then packages `build/bin/Pelton.dmg`
  with a drag-to-Applications drop link).
- **Windows**: `make build-win` builds the `.exe` and, with NSIS
  installed, the installer too (it runs `NSIS=1 scripts/build.sh`).
- **Linux**: `build-linux` also copies `build/linux/pelton.desktop` next to
  the binary in `build/bin/`. Install the binary somewhere on your `PATH`,
  install the `.desktop` file to `~/.local/share/applications/`, and give
  it an icon named `pelton` (see `build/icons/`).

- [x] Run it in dev mode, or build a release binary

## Need help?

See [Support](../support.md).
