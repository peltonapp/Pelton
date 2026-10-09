# Pelton - email client (Wails + Svelte)

.PHONY: build build-mac build-win build-linux build-nix dmg run run-nightly app-dev dev clean tidy deps licenses icon disclaimer sync-docs e2e e2e-manual

# version string injected into the binary. it prefers the latest git tag (with a
# short commit suffix on untagged commits) and falls back to "dev". it is wired
# into main.version via ldflags and shown in the about section.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# wails links webkit2gtk-4.0 on Linux unless built with the webkit2_41 tag. most
# current distros ship only 4.1, so build-linux and the dev targets add the tag
# whenever pkg-config finds it. empty on macOS and Windows, where there is no
# webkitgtk.
WEBKIT_TAGS := $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo -tags webkit2_41)

# wails adds -mmacosx-version-min=10.13 to the cgo flags unless they already set
# one, but Info.plist enforces macOS 13 (and Go 1.27 needs it), so build the
# cgo code for 13 on macOS.
ifeq ($(shell uname),Darwin)
export CGO_CFLAGS := $(strip $(CGO_CFLAGS) -mmacosx-version-min=13.0)
export CGO_LDFLAGS := $(strip $(CGO_LDFLAGS) -mmacosx-version-min=13.0)
endif

# production build into build/bin
build:
	wails build -ldflags "$(LDFLAGS)"

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
		--volicon "build/bin/Pelton.app/Contents/Resources/pelton.icns" \
		--window-size 540 380 \
		--icon-size 128 \
		--icon "Pelton.app" 130 170 \
		--app-drop-link 410 170 \
		--hide-extension "Pelton.app" \
		"build/bin/Pelton.dmg" \
		"build/bin/Pelton.app"

# windows build (amd64). cross-compiling from another OS needs the appropriate
# toolchain (mingw-w64) and webview2; run on Windows for a no-fuss build.
build-win: disclaimer
	wails build -platform windows/amd64 -ldflags "$(LDFLAGS)"

# linux build (amd64), then drop the .desktop launcher next to the binary so it
# is easy to install into ~/.local/share/applications (or a package). building
# from macOS needs the gtk/webkit2gtk toolchain; run on Linux for a clean build.
build-linux:
	wails build -platform linux/amd64 $(WEBKIT_TAGS) -ldflags "$(LDFLAGS)"
	cp build/linux/pelton.desktop build/bin/pelton.desktop
	@echo "linux binary + pelton.desktop in build/bin (install the .desktop and an icon named 'pelton')"

# nix packaging build: same as build-linux but tagged for nixpkgs'
# webkitgtk_4_1 (nixpkgs dropped the older webkit2gtk 4.0 build)
build-nix:
	wails build -tags webkit2_41 -ldflags "$(LDFLAGS)" -o pelton

# run the whole app in dev mode: make sure go + npm deps are present, regenerate
# the typescript bindings from the go methods, then launch wails dev with hot
# reload for both the go backend and the svelte frontend. PELTON_DEV points the
# app at a separate Pelton-dev config/database directory (see
# internal/storage/db.go), so testing here never touches a real install's
# accounts, mail cache or settings.
run: deps
	wails generate module
	PELTON_DEV=1 wails dev $(WEBKIT_TAGS) -ldflags "$(LDFLAGS)"

# run the app in the cosmetic demo mode (--potatoes-are-nice): the ui fills with
# fixed potato-themed sample data for website screenshots and never touches real
# accounts, mail or the network. Same dev setup as `run`, just with the flag.
nice-potatoes: deps
	wails generate module
	PELTON_DEV=1 wails dev $(WEBKIT_TAGS) -appargs "--potatoes-are-nice" -ldflags "$(LDFLAGS)"

# run the app as a nightly build would behave: the launch warning dialog, the
# status bar marker and the nightly name/icon in the about block. PELTON_DEV
# still points it at the dev data directory, so this does not create the real
# nightly one either.
run-nightly: deps
	wails generate module
	PELTON_DEV=1 wails dev $(WEBKIT_TAGS) -ldflags "$(LDFLAGS) -X main.channel=nightly"

dev: run

# playwright suite against Stalwart in Docker. see e2e/README.md for the
# prerequisites (Docker, trusted test CA, free ports).
e2e:
	e2e/run.sh

# same Stalwart, accounts and (smaller) seed as e2e, then `make run` instead of
# Playwright, so the mailboxes can be added and tried by hand. Quitting Pelton
# tears Stalwart down and wipes Pelton-dev.
e2e-manual:
	e2e/manual.sh

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
	wails build -clean || true

# regenerate docs/contributing/{guidelines,code-of-conduct,dco}.md from the
# canonical CONTRIBUTING.md, CODE_OF_CONDUCT.md and DCO.md at the repo root.
# run this after editing any of those three before building or serving docs.
sync-docs:
	scripts/sync-root-docs.sh
