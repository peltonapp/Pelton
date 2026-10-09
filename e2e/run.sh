#!/usr/bin/env bash
# Full Pelton e2e: wipe dev data, start Stalwart, provision, seed, wails dev, Playwright.
# The trap always stops wails, deletes Pelton-dev, and removes the compose project.
# Reuse an already running UI: E2E_BASE_URL=http://127.0.0.1:PORT ./e2e/run.sh
set -euo pipefail

E2E="$(cd "$(dirname "$0")" && pwd)"
APP="$(cd "$E2E/.." && pwd)"

# Attach to a UI that is already running. Do not start or stop wails,
# do not delete Pelton-dev, and do not run docker compose.
if [[ -n "${E2E_BASE_URL:-}" ]]; then
  echo "reusing UI at ${E2E_BASE_URL}"
  cd "$E2E/ui"
  if [[ ! -d node_modules/@playwright/test ]]; then
    pnpm install
    pnpm exec playwright install chromium
  fi
  PELTON_URL="$E2E_BASE_URL" pnpm exec playwright test
  exit 0
fi

DEV_DATA="${HOME}/Library/Application Support/Pelton-dev"
URL_FILE="$E2E/.frontend-url"
WAILS_LOG="$E2E/.wails.log"
WAILS_PID_FILE="$E2E/.wails.pid"

if [[ "$DEV_DATA" == *"Pelton-nightly"* || "$DEV_DATA" == *"Application Support/Pelton" ]]; then
  echo "refusing to touch a non-dev data directory: $DEV_DATA" >&2
  exit 1
fi

stop_dev() {
  if [[ -f "$WAILS_PID_FILE" ]]; then
    local pid
    pid="$(cat "$WAILS_PID_FILE" 2>/dev/null || true)"
    if [[ -n "${pid}" ]]; then
      kill "$pid" 2>/dev/null || true
      pkill -P "$pid" 2>/dev/null || true
    fi
    rm -f "$WAILS_PID_FILE"
  fi
  # Stop a leftover PELTON_DEV process. Production Pelton and Pelton-nightly are left alone.
  local pid
  for pid in $(pgrep -f '[Pp]elton|wails dev' || true); do
    if ps eww -p "$pid" 2>/dev/null | grep -q 'PELTON_DEV=1'; then
      kill "$pid" 2>/dev/null || true
    fi
  done
}

wipe_dev() {
  stop_dev
  if [[ -d "$DEV_DATA" ]]; then
    rm -rf "$DEV_DATA"
  fi
}

cleanup() {
  local status=$?
  echo "cleaning up (exit $status)"
  stop_dev
  wipe_dev
  (cd "$E2E" && docker compose -p pelton-e2e -f docker-compose.yml down -v) || true
  exit "$status"
}
trap cleanup EXIT

wipe_dev

echo "starting stalwart"
(cd "$E2E" && docker compose -p pelton-e2e -f docker-compose.yml up -d --wait)
"$E2E/provision.sh"
(cd "$APP" && go run ./e2e/seed)

echo "starting wails dev"
rm -f "$URL_FILE" "$WAILS_LOG"
(
  cd "$APP"
  export PELTON_DEV=1
  exec wails dev
) >"$WAILS_LOG" 2>&1 &
echo $! >"$WAILS_PID_FILE"

echo "waiting for frontend URL"
deadline=$((SECONDS + 180))
url=""
while (( SECONDS < deadline )); do
  # The Wails dev server (not the Vite URL) injects window.go.
  url="$(grep -Eo 'To develop in the browser.*http://[^[:space:][:cntrl:]]+' "$WAILS_LOG" | grep -Eo 'http://[^[:space:][:cntrl:]]+' | head -n 1 || true)"
  if [[ -z "$url" ]]; then
    url="$(grep -Eo 'Using DevServer URL: http://[^[:space:][:cntrl:]]+' "$WAILS_LOG" | grep -Eo 'http://[^[:space:][:cntrl:]]+' | head -n 1 || true)"
  fi
  if [[ -n "$url" ]]; then
    break
  fi
  if ! kill -0 "$(cat "$WAILS_PID_FILE")" 2>/dev/null; then
    echo "wails dev exited" >&2
    tail -n 80 "$WAILS_LOG" >&2
    exit 1
  fi
  sleep 1
done
if [[ -z "$url" ]]; then
  echo "frontend URL not found" >&2
  tail -n 80 "$WAILS_LOG" >&2
  exit 1
fi
printf '%s\n' "$url" >"$URL_FILE"
echo "frontend $url"

cd "$E2E/ui"
if [[ ! -d node_modules/@playwright/test ]]; then
  pnpm install
  pnpm exec playwright install chromium
fi
E2E_BASE_URL="$url" PELTON_URL="$url" pnpm exec playwright test
