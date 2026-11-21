# Pelton - email client (Wails v3 + Svelte)

.PHONY: build build-mac build-win build-linux build-nix dmg run nice-potatoes run-nightly app-dev dev bindings clean tidy deps licenses icon disclaimer sync-docs e2e e2e-manual e2e-app e2e-channel

# version string injected into the binary. it prefers the latest git tag (with a
# short commit suffix on untagged commits) and falls back to "dev". it is wired
# into main.version via ldflags and shown in the about section.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
export VERSION

# the wails3 cli, run at the version go.mod pins rather than whatever happens
# to be installed. only the bindings generator and the windows build use it.
# gtk3 keeps the cli itself buildable on Linux with the GTK 3 headers Pelton
# needs anyway; the tag does nothing elsewhere.
WAILS_VERSION := $(shell go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3 2>/dev/null)
WAILS3 := go run -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)

# production build for this OS into build/bin (the .app on macOS). see
# scripts/build.sh.
build:
	scripts/build.sh

# compile the macOS Liquid Glass app icon (build/darwin/pelton.icon) into the
# built .app via actool. needs a one-time `sudo xcodebuild -runFirstLaunch`.
icon:
	scripts/build-icon.sh

# macOS build that also installs the Liquid Glass icon into the .app, and ships
# the warranty/liability terms inside the bundle so they travel with the copy.
build-mac: build icon
	cp DISCLAIMER.md build/bin/Pelton.app/Contents/Resources/DISCLAIMER.md

# macOS build packaged into a distributable .dmg (build/bin/Pelton.dmg), with a
# drag-to-Applications drop link. Needs create-dmg (`brew install create-dmg`).
dmg: build-mac
	rm -f build/bin/Pelton.dmg
	create-dmg \
		--volname "Pelton" \
		--volicon "build/icons/pelton.icns" \
		--window-size 540 380 \
		--icon-size 128 \
		--icon "Pelton.app" 130 170 \
		--app-drop-link 410 170 \
		--hide-extension "Pelton.app" \
		"build/bin/Pelton.dmg" \
		"build/bin/Pelton.app"

# windows build (amd64) with the NSIS installer. run on Windows with NSIS
# installed; makensis has to be on PATH.
build-win: disclaimer
	NSIS=1 scripts/build.sh

# linux build (amd64), then drop the .desktop launcher next to the binary so it
# is easy to install into ~/.local/share/applications (or a package). needs the
# gtk3 and webkit2gtk-4.1 development packages.
build-linux:
	scripts/build.sh
	cp build/linux/pelton.desktop build/bin/pelton.desktop
	@echo "linux binary + pelton.desktop in build/bin (install the .desktop and an icon named 'pelton')"

# nix packaging build: the frontend and a gtk3 binary named pelton, with no
# git or network (packaging/nix/package.nix passes VERSION).
build-nix:
	cd frontend && pnpm run build
	go build -tags "production gtk3" -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o build/bin/pelton .

# regenerate the typescript bindings in frontend/bindings from the bound go
# methods. run after adding or changing a method on desktop.App.
bindings:
	$(WAILS3) generate bindings -ts -d frontend/bindings -silent ./...

# run the whole app in dev mode: vite serves the frontend with hot reload and
# the app loads it from there. PELTON_DEV points the app at a separate
# Pelton-dev config/database directory (see internal/storage/db.go), so testing
# here never touches a real install's accounts, mail cache or settings. go
# changes need a restart of make run.
run: deps bindings
	scripts/dev.sh

# run the app in the cosmetic demo mode (--potatoes-are-nice): the ui fills with
# fixed potato-themed sample data for website screenshots and never touches real
# accounts, mail or the network. Same dev setup as `run`, just with the flag.
nice-potatoes: deps bindings
	scripts/dev.sh --potatoes-are-nice

# run the app as a nightly build would behave: the launch warning dialog, the
# status bar marker and the nightly name/icon in the about block. PELTON_DEV
# still points it at the dev data directory, so this does not create the real
# nightly one either.
run-nightly: deps bindings
	CHANNEL=nightly scripts/dev.sh

dev: run

# playwright suite against Stalwart in Docker. see e2e/README.md for the
# prerequisites (Docker, trusted test CA, free ports).
e2e:
	e2e/run.sh

# same Stalwart, accounts and (smaller) seed as e2e, then the app instead of
# Playwright, so the mailboxes can be added and tried by hand. Quitting Pelton
# tears Stalwart down and wipes the Pelton-e2e-manual data directory.
e2e-manual:
	e2e/manual.sh

# the app as e2e/run.sh and e2e/manual.sh start it: wails dev built as channel
# $(E2E_CHANNEL), so it gets its own data directory, keyring service and
# single-instance lock (Pelton-$(E2E_CHANNEL)) and the scripts can wipe them
# without touching Pelton-dev, which every worktree's `make run` shares.
# PELTON_DEV is removed because it would win over the channel; PELTON_DEVTOOLS
# keeps the developer overlays. The Wails dev server gets its own address:
# `make run` keeps the default 34115, and wails dev only logs a failed bind, so
# sharing it would point Playwright at the developer's Pelton-dev app.
# Dependencies install from the lockfiles only (--frozen-lockfile).
E2E_CHANNEL ?= e2e
E2E_DEVSERVER_PORT ?= 34125
e2e-app: e2e-channel
	go mod download
	cd frontend && pnpm install --frozen-lockfile
	@# wails dev runs frontend:install (a plain pnpm install) when package.json.md5
	@# is missing or stale. Record the checksum it compares, so it skips that.
	cd frontend && { md5sum package.json 2>/dev/null || md5 -r package.json; } | cut -d' ' -f1 | tr -d '\n' >package.json.md5
	wails generate module
	env -u PELTON_DEV PELTON_DEVTOOLS=1 wails dev $(WEBKIT_TAGS) -devserver 127.0.0.1:$(E2E_DEVSERVER_PORT) -ldflags "$(LDFLAGS) -X main.channel=$(E2E_CHANNEL)"

# an empty channel would be the real Pelton data directory, and 34115 is the
# dev server of `make run`. The port must be plain digits (no leading zero, so
# 034115 cannot pass for something other than 34115) from 1 to 65535.
e2e-channel:
	@case "$(E2E_CHANNEL)" in e2e|e2e-*) ;; *) echo "E2E_CHANNEL must be e2e or e2e-<name>, got '$(E2E_CHANNEL)'" >&2; exit 1;; esac
	@p='$(E2E_DEVSERVER_PORT)'; case "$$p" in ''|*[!0-9]*|0*|??????*|34115) ok=;; *) ok=1;; esac; \
	if [ -z "$$ok" ] || [ "$$p" -gt 65535 ]; then echo "E2E_DEVSERVER_PORT must be a port from 1 to 65535 without leading zeros, other than 34115 (make run), got '$$p'" >&2; exit 1; fi

# alias kept for discoverability; identical to run.
app-dev: run

# sync go + npm dependencies (frontend uses pnpm)
deps:
	go mod download
	cd frontend && pnpm install

tidy:
	go mod tidy

# render DISCLAIMER.md into the plain text the windows installer shows on its
# liability page. the result is committed, so this only needs running after an
# edit to DISCLAIMER.md (build-win does it anyway).
disclaimer:
	node scripts/gen-installer-notice.mjs

# build licenses/manifest.json (embedded and shown in the about section).
licenses:
	node scripts/collect-licenses.mjs

clean:
	go clean
	rm -rf build/bin frontend/dist

# regenerate docs/contributing/{guidelines,code-of-conduct,dco}.md from the
# canonical CONTRIBUTING.md, CODE_OF_CONDUCT.md and DCO.md at the repo root.
# run this after editing any of those three before building or serving docs.
sync-docs:
	scripts/sync-root-docs.sh
