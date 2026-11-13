#!/usr/bin/env bash
# build.sh builds Pelton for the OS it runs on, into build/bin: the .app bundle
# on macOS, the exe (and with NSIS=1 the installer) on Windows, the binary on
# Linux. The Makefile and the release and nightly workflows all build through
# it.
#
# Environment:
#   VERSION          version stamped into the binary (default: git describe)
#   PRODUCT_VERSION  version for the bundle metadata (default: VERSION)
#   CHANNEL          empty for a normal build, "nightly" for a nightly
#   NSIS             1 to also build the Windows installer (needs makensis)
#   SKIP_FRONTEND    1 to reuse an existing frontend/dist
set -euo pipefail
cd "$(dirname "$0")/.."

version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
product_version="${PRODUCT_VERSION:-$version}"
channel="${CHANNEL:-}"
goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
out=build/bin

ldflags="-s -w -X main.version=$version"
if [ -n "$channel" ]; then
  ldflags="$ldflags -X main.channel=$channel"
fi

product="Pelton"
nightly=()
if [ "$channel" = "nightly" ]; then
  product="Pelton Nightly"
  nightly=(-nightly)
fi

wails_version="$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
wails3() {
  go run -tags gtk3 "github.com/wailsapp/wails/v3/cmd/wails3@$wails_version" "$@"
}

# meta NAME [appmeta flags...] TEMPLATE renders a packaging template.
meta() {
  local name="$1"
  shift
  go run ./scripts/appmeta -version "$product_version" -name "$name" -product "$product" ${nightly[@]+"${nightly[@]}"} "$@"
}

if [ -z "${SKIP_FRONTEND:-}" ]; then
  (cd frontend && pnpm run build)
fi
mkdir -p "$out"

case "$goos" in
darwin)
  # Go 1.27 runs on macOS 13 and later; build and link the cgo parts for the
  # same, so the binary does not claim an older macOS than it runs on.
  export MACOSX_DEPLOYMENT_TARGET=13.0
  app="$out/$product.app"
  rm -rf "$app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  go build -tags production -trimpath -ldflags "$ldflags -extldflags=-mmacosx-version-min=13.0" -o "$app/Contents/MacOS/$product" .
  meta "$product" build/darwin/Info.plist >"$app/Contents/Info.plist"
  icns=build/icons/pelton.icns
  if [ "$channel" = "nightly" ]; then
    icns=build/icons/pelton-nightly.icns
  fi
  cp "$icns" "$app/Contents/Resources/iconfile.icns"
  echo "built $app"
  ;;

windows)
  exe="Pelton"
  if [ "$channel" = "nightly" ]; then
    exe="PeltonNightly"
  fi
  work="$(mktemp -d)"
  syso="rsrc_windows_$goarch.syso"
  trap 'rm -rf "$work" "$syso"' EXIT
  # the version resource, icon and manifest go into the exe through a .syso
  # next to main.go, which go build links in on its own.
  meta "$exe" -numeric build/windows/info.json >"$work/info.json"
  wails3 generate syso -arch "$goarch" -icon build/windows/icon.ico \
    -manifest build/windows/wails.exe.manifest -info "$work/info.json" -out "$syso"
  go build -tags production -trimpath -ldflags "$ldflags -H windowsgui" -o "$out/$exe.exe" .
  echo "built $out/$exe.exe"

  if [ "${NSIS:-}" = "1" ]; then
    inst=build/windows/installer
    meta "$exe" -numeric "$inst/wails_tools.nsh.tmpl" >"$inst/wails_tools.nsh"
    wails3 generate webview2bootstrapper -dir "$inst/tmp"
    if [ "$channel" = "nightly" ]; then
      echo '!define PELTON_NIGHTLY' >"$inst/channel.nsh"
    fi
    arch="$(echo "$goarch" | tr '[:lower:]' '[:upper:]')"
    (cd "$inst" && makensis "-DARG_WAILS_${arch}_BINARY=..\\..\\bin\\$exe.exe" project.nsi)
    echo "built $out/$exe-$goarch-installer.exe"
  fi
  ;;

linux)
  # gtk3: Pelton's own GTK code and the packages target GTK 3 and
  # webkit2gtk-4.1, where Wails v3 defaults to GTK 4.
  go build -tags "production gtk3" -trimpath -ldflags "$ldflags" -o "$out/Pelton" .
  echo "built $out/Pelton"
  ;;

*)
  echo "build.sh: unsupported OS $goos" >&2
  exit 1
  ;;
esac
