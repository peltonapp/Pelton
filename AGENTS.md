# Agent instructions for Pelton

Pelton is a FOSS, cross-platform email client (Go + Wails backend, Svelte 5 +
TypeScript frontend) built around privacy.

No telemetry. No tracking. No analytics. No phoning home. No third-party
servers in the data path. No slop. No dead code. No hardcoded colors. No
speculative features. No exceptions without asking first.

This file is the shared entry point for any coding agent working in this
repo (Claude, Codex, Aider, or otherwise) — `CLAUDE.md`, `.codex.md`, and
`.aider.md` point back here. It's kept short on purpose; the real detail
lives in `AGENTS/`, read the file for whatever area you're touching before
you start:

- **[AGENTS/privacy.md](AGENTS/privacy.md)** — the privacy/no-telemetry
  constraints and the configurability principle. Read before touching
  networking, sync, credentials, or settings.
- **[AGENTS/design-tokens.md](AGENTS/design-tokens.md)** — UI style and the
  design token system. Read before touching anything under
  `frontend/src/components/`, `frontend/src/theme/`, or `style.css`.
- **[AGENTS/backend.md](AGENTS/backend.md)** — Go package layout,
  docstrings, testing, concurrency. Read before touching `internal/` or
  `cmd/`.
- **[AGENTS/frontend.md](AGENTS/frontend.md)** — Svelte/TS layout,
  docstrings, localization, testing. Read before touching `frontend/`.
- **[AGENTS/git-workflow.md](AGENTS/git-workflow.md)** — commit style,
  branch naming, `dev`/`main` flow, release changelog. Read before opening a
  branch or PR.
- **[AGENTS/no-slop.md](AGENTS/no-slop.md)** — the no-slop rules in detail.
  Applies everywhere, always.

## Project shape

- `internal/` — Go backend, one package per concern.
- `frontend/` — Svelte 5 + TypeScript + Vite.
- `cmd/` — standalone test/debug binaries, not the main app.
- `main.go` + `wails.json` wire the two together.
- `make run` for dev (frontend hot reload, restart for Go changes, isolated `PELTON_DEV` data dir). `make
  build-*` for platform builds. The `Makefile` is the source of truth for
  build/run/package commands.
