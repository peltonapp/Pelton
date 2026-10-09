---
title: Developing on Pelton
description: Set up a local development environment and understand the codebase layout.
---

# Developing on Pelton

Pelton is Go + [Wails](https://wails.io) on the backend, Svelte 5 +
TypeScript on the frontend. This page covers running the app in dev mode
and finding your way around the codebase. For a plain release build, see
[Build from source](../install/build-from-source.md) instead.

!!! tip "New here?"
    Worth reading the [Contributing guidelines](guidelines.md) first, they
    cover ground rules and the PR workflow this guide assumes.

## Checklist
<div class="checklist" markdown>
- [ ] Install the prerequisites
- [ ] Clone the repo and run it in dev mode
- [ ] Find the area of the codebase you're touching
- [ ] Run the checks before opening a pull request
</div>

## Prerequisites

Same as building from source, see
[Build from source](../install/build-from-source.md#prerequisites) for the
full list: Go 1.27.1+, Node.js + pnpm, the pinned Wails CLI version, and
your platform's GTK/WebKitGTK, WebView2, or Xcode toolchain.

## Running in dev mode

```bash
git clone https://github.com/peltonapp/Pelton.git
cd Pelton
make run
```

`make run` installs Go and pnpm dependencies, regenerates the TypeScript
bindings from the Go methods, and launches `wails dev` with hot reload for
both the Go backend and the Svelte frontend. It points Pelton at an
isolated `PELTON_DEV` config/database directory, so it never touches a
real install's accounts, mail cache, or settings.

A couple of related targets:

```bash
make nice-potatoes  # dev mode with --potatoes-are-nice, fixed sample data
make run-nightly    # dev mode behaving like a nightly build
```

## Project layout

- `internal/`: Go backend, one package per concern: `imap`, `smtp`,
  `storage`, `sync`, `crypto`, `credentials`, `oauth`, `search`, `outbox`,
  `configsync`, `autoconfig`, `mailview`. `internal/desktop` is the Wails
  bind layer exposed to the frontend (`bind_*.go` files group bindings by
  feature).
- `frontend/`: Svelte 5 + TypeScript + Vite.
    - `src/components/`: feature subfolders: `sidebar`, `settings`,
      `compose`, `wizard`, `common`, `detail`, `list`, `onboarding`.
    - `src/stores/`: app state.
    - `src/theme/`: design tokens and theme/accent logic.
    - `src/lib/`: Wails API bindings, locales.
- `cmd/`: standalone test/debug binaries (imaptest, smtptest,
  storagetest, synctest), not the main app.
- `main.go` + `wails.json` wire the backend and frontend together.
- `Makefile`: the source of truth for build/run/package commands.

## Code style

Full detail lives in `AGENTS/backend.md` and `AGENTS/frontend.md` in the
repo. The short version:

- Every exported Go function, type, and package-level var/const gets a
  GoDoc comment stating behavior and edge cases plainly.
- Every exported TypeScript function, type/interface, and public
  component prop gets a short JSDoc-style comment.
- Comments explain non-obvious *why*, never *what*, the code already says
  what it does.
- Sync, downloads, and other heavy I/O run off the main/UI thread via
  goroutines, never block the Wails main thread with network or disk work.

## Localization

Adding user-facing text means adding the key to every locale file under
`frontend/src/lib/locales/`. See
[Translating Pelton](translations.md) for the full workflow.

## Running the checks

```bash
go test ./...            # backend tests
cd frontend && pnpm run check   # svelte-check, must pass
```

Table-driven Go tests live alongside the code (`*_test.go`), see
`internal/crypto`, `internal/smtp`, `internal/sync`, and `internal/outbox`
for the existing style. Add or update tests for any backend logic change,
especially storage, sync, crypto, and parsing code. There's no significant
frontend test suite yet, verify UI changes manually via `make run`.

## End-to-end tests

`e2e/` holds a Playwright suite that drives the real UI against a Stalwart
mail server in Docker. It covers onboarding with two IMAP mailboxes, sending
between them, scrolling and backfilling a 6,400-message inbox, search and
select all.
See [`e2e/README.md`](https://github.com/peltonapp/Pelton/blob/main/e2e/README.md)
for the details.

You need Docker, pnpm, and the Wails CLI matching `go.mod`. Trust the test
CA once with `e2e/scripts/trust-cert-macos.sh` or
`e2e/scripts/trust-cert-linux.sh`, and keep ports 443, 993 and 465 free on
`127.0.0.1`, along with 18081, Stalwart's HTTP port. Then:

```bash
./e2e/run.sh
```

To try the same setup by hand, `./e2e/manual.sh` (or `make e2e-manual`)
starts and seeds Stalwart, then runs `make run` so you can add the test
mailboxes yourself. `make e2e` runs `./e2e/run.sh`.

`run.sh` is macOS-oriented: it wipes `~/Library/Application Support/Pelton-dev`
(a dev-only directory, never a real install's data), and on Linux it does
not wipe the dev data directory.

## Need help?

See [Support](../support.md).
