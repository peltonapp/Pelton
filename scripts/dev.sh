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
darwin)
  export MACOSX_DEPLOYMENT_TARGET=13.0
  ldflags="$ldflags -extldflags=-mmacosx-version-min=13.0"
  ;;
esac

# vite needs node 20.19+ or 22.12+ (frontend/package.json engines); an older
# node crashes it on start, and the app then waits for a server that never
# comes.
node_version="$(node -p 'process.versions.node' 2>/dev/null || echo 0.0.0)"
if ! node -e '
  const [maj, min] = process.versions.node.split(".").map(Number)
  process.exit(maj > 22 || (maj === 22 && min >= 12) || (maj === 20 && min >= 19) ? 0 : 1)
' 2>/dev/null; then
  echo "dev.sh: node $node_version is too old for vite; use node 20.19+ or 22.12+ (e.g. fnm use 26)" >&2
  exit 1
fi

# main.go embeds frontend/dist, which has to exist for the build even though
# dev mode never serves from it.
mkdir -p frontend/dist
[ -e frontend/dist/index.html ] || : >frontend/dist/index.html

(cd frontend && exec pnpm exec vite --port "$port" --strictPort) &
vite=$!
trap 'kill "$vite" 2>/dev/null || true' EXIT INT TERM

until curl -s -o /dev/null "http://localhost:$port/"; do
  if ! kill -0 "$vite" 2>/dev/null; then
    echo "dev.sh: the vite dev server exited, see its output above" >&2
    exit 1
  fi
  sleep 0.2
done

PELTON_DEV=1 FRONTEND_DEVSERVER_URL="http://localhost:$port" \
  go run -tags "$tags" -ldflags "$ldflags" . "$@"
