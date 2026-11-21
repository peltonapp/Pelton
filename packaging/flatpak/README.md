# Flatpak / Flathub

`sh.arne.Pelton.yml` is the Flathub manifest. This directory is the source of
truth for it; Flathub builds from its own copy in
`flathub/sh.arne.Pelton`, so every change here has to be mirrored there.

## How the offline build works

Flathub builders have no network access, which means the manifest cannot run
`go mod download` or `pnpm install`. Instead the release workflow
(`.github/workflows/release.yml`, job `flatpak-source`) publishes a
`Pelton-<tag>-vendored.tar.gz` asset on every release containing:

- the repository sources at that tag
- `vendor/` from `go mod vendor`
- `frontend/node_modules` installed with pnpm's hoisted linker, so it is real
  directories rather than a symlink farm pointing at a store that will not
  exist on the builder
- a `VERSION` file holding the tag, which the manifest passes to
  `-X main.version`

The manifest's only source is that tarball. Inside the sandbox the build is
`vite build` followed by `go build -mod=vendor -tags "production gtk3"`; the
Wails CLI is never needed, because `main.go` embeds `frontend/dist` with
`go:embed`.

The archive is around 100 MB. That is the cost of a genuinely offline build
and is normal for Go and Node applications on Flathub.

## First submission

1. Cut a release. The manifest points at a release asset, so there is nothing
   to submit until one exists. The `flatpak-source` job prints the tarball's
   sha256 to the workflow summary.
2. Update `url` and `sha256` in `sh.arne.Pelton.yml` to that release.
3. Fork `flathub/flathub` and create a branch named exactly `sh.arne.Pelton`.
4. Put `sh.arne.Pelton.yml` at the root of that branch and open a pull request
   against the `new-pr` branch, not `master`.
5. A bot test-builds the manifest and a reviewer goes through it. Expect
   questions about `--filesystem=home`: Wails opens GTK file chooser
   dialogs directly instead of going through the document portal, so a
   narrower permission would make saving attachments and exporting backups
   fail wherever the user pointed the dialog.
6. Once merged, Flathub creates `flathub/sh.arne.Pelton` and you get commit
   access to it.

## Verifying the app

After publication, sign in to flathub.org with the GitHub account and follow
the verification flow for `sh.arne.Pelton`. It asks for a token to be served
at `https://arne.sh/.well-known/org.flathub.VerifiedApps.txt`.

## Per-release updates

Nothing, normally. The `x-checker-data` block lets Flathub's external data
checker notice a new GitHub release, rewrite the url and checksum and open the
pull request on its own. The version string, the AppStream release entry and
the screenshots all come out of the tarball, so no other field has to move.

Merge that pull request in `flathub/sh.arne.Pelton` and copy the same two
lines back into this file so the two do not drift.

If the manifest itself changes (permissions, runtime version, build steps),
change it here first and copy it over.

## Runtime version

`runtime-version` is pinned to a GNOME runtime that still ships
`webkit2gtk-4.1`, which is what Pelton's GTK3 build of Wails links against. GNOME runtimes go end
of life roughly a year after release and Flathub will email about it; bumping
is usually a one-line change plus a rebuild.
