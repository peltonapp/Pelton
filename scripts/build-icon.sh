#!/usr/bin/env bash
# build-icon.sh compiles the macOS "Liquid Glass" app icon (Icon Composer
# build/darwin/pelton.icon) into an Assets.car + a partial Info.plist using
# actool, then copies them next to the built .app so macOS 26 renders the new
# dynamic icon. Run after `make build` (the .app must already exist).
#
# Prerequisite (one time, needs admin): the actool image-compiler helper must be
# installed, otherwise it fails with "A required plugin failed to load":
#
#     sudo xcodebuild -runFirstLaunch
#
# Usage: scripts/build-icon.sh [path/to/Pelton.app]
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
icon_src="$repo_root/build/darwin/pelton.icon"
app="${1:-$repo_root/build/bin/Pelton.app}"
icon_name="pelton"

if ! command -v actool >/dev/null 2>&1 && ! xcrun --find actool >/dev/null 2>&1; then
  echo "error: actool not found; install Xcode (not just the Command Line Tools)." >&2
  exit 1
fi

if [ ! -d "$icon_src" ]; then
  echo "error: $icon_src not found" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# actool consumes the .icon bundle directly (wrapping it in an .xcassets makes it
# emit only a partial plist with no Assets.car). this produces Assets.car (the
# dynamic Liquid Glass icon) plus <name>.icns (a static fallback).
mkdir -p "$work/out"
xcrun actool "$icon_src" \
  --compile "$work/out" \
  --app-icon "$icon_name" \
  --output-partial-info-plist "$work/partial.plist" \
  --platform macosx \
  --minimum-deployment-target 26.0 \
  --target-device mac \
  --errors --warnings

echo "compiled icon assets:"
ls -1 "$work/out"

if [ -d "$app" ]; then
  res="$app/Contents/Resources"
  mkdir -p "$res"
  cp "$work/out/Assets.car" "$res/Assets.car"
  cp "$work/out/$icon_name.icns" "$res/$icon_name.icns"
  # CFBundleIconFile=iconfile is what pre-macOS-26 systems (everything today,
  # including every CI runner) actually display - they don't understand
  # Assets.car/CFBundleIconName at all. actool's own $icon_name.icns fallback
  # is meant for that same Liquid Glass pipeline and isn't pre-masked with
  # rounded corners the way a normal .icns is, so on those systems it showed
  # up as a square with a visible outline. build/icons/pelton.icns is the
  # repo's regular, properly masked icon; use that for the pre-26 fallback.
  cp "$repo_root/build/icons/pelton.icns" "$res/iconfile.icns"
  echo "installed Assets.car + $icon_name.icns into $res"

  # ensure Info.plist references the asset-catalog icon, even if this .app was
  # built from an older template that lacked the key.
  plist="$app/Contents/Info.plist"
  pb=/usr/libexec/PlistBuddy
  if [ -x "$pb" ] && [ -f "$plist" ]; then
    "$pb" -c "Set :CFBundleIconName $icon_name" "$plist" 2>/dev/null \
      || "$pb" -c "Add :CFBundleIconName string $icon_name" "$plist"
    echo "set CFBundleIconName=$icon_name in $plist"
  fi
else
  echo "app bundle not found at $app; assets left in: $repo_root/build/icons/compiled"
  rm -rf "$repo_root/build/icons/compiled"
  mkdir -p "$repo_root/build/icons/compiled"
  cp -R "$work/out/." "$repo_root/build/icons/compiled/"
  trap - EXIT
fi
