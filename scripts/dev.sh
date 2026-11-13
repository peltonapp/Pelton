#!/usr/bin/env bash
# dev.sh runs Pelton in dev mode: vite serves the frontend with hot reload, and
# a non-production build of the app loads the ui from it (Wails reads
# FRONTEND_DEVSERVER_URL) with the web inspector enabled. PELTON_DEV keeps it on
# the separate dev data directory. Arguments are passed to the app.
#
# Environment:
#   CHANNEL  "nightly" to run as a nightly build would
#   VERSION  version shown in the about section (default: git describe)
set -euo pipefail
cd "$(dirname "$0")/.."

port="${DEV_PORT:-5173}"
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ldflags="-X main.version=$version"
if [ -n "${CHANNEL:-}" ]; then
  ldflags="$ldflags -X main.channel=$CHANNEL"
fi

tags=""
case "$(go env GOOS)" in
linux) tags="gtk3" ;;
darwin) export MACOSX_DEPLOYMENT_TARGET=13.0 ;;
esac

# main.go embeds frontend/dist, which has to exist for the build even though
# dev mode never serves from it.
mkdir -p frontend/dist
[ -e frontend/dist/index.html ] || : >frontend/dist/index.html

(cd frontend && exec pnpm exec vite --port "$port" --strictPort) &
vite=$!
trap 'kill "$vite" 2>/dev/null || true' EXIT INT TERM

for _ in $(seq 1 100); do
  if curl -s -o /dev/null "http://localhost:$port/"; then
    break
  fi
  sleep 0.2
done

PELTON_DEV=1 FRONTEND_DEVSERVER_URL="http://localhost:$port" \
  go run -tags "$tags" -ldflags "$ldflags" . "$@"
