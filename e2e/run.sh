#!/usr/bin/env bash
# Full Pelton e2e: start Stalwart, provision, seed, start the e2e app, Playwright.
#
# The app is built as channel "e2e" (make e2e-app), so its data directory,
# keyring service and single-instance lock are Pelton-e2e, never Pelton-dev.
# Everything is checked before anything is started or deleted. On exit the
# trap stops only the process tree this script started, deletes Pelton-e2e and
# its keyring entries, and removes the compose project if this script created it.
#
#   E2E_FORCE=1     take down a pelton-e2e project that is already running
#   E2E_BASE_URL    reuse an already running UI; see e2e/README.md
set -euo pipefail

# shellcheck source=e2e/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

CHANNEL=e2e
DATA_DIR="$(e2e_data_dir "$CHANNEL")"
export E2E_DB="${E2E_DB:-$DATA_DIR/pelton.db}"

install_playwright() {
  cd "$E2E/ui"
  if [[ ! -d node_modules/@playwright/test ]]; then
    pnpm install --frozen-lockfile
    pnpm exec playwright install chromium
  fi
}

# Attach to a UI that is already running. Start, stop and delete nothing.
if [[ -n "${E2E_BASE_URL:-}" ]]; then
  echo "reusing UI at ${E2E_BASE_URL} (database ${E2E_DB})"
  install_playwright
  PELTON_URL="$E2E_BASE_URL" pnpm exec playwright test
  exit 0
fi

URL_FILE="$E2E/.frontend-url"
WAILS_LOG="$E2E/.wails.log"

e2e_check_prereqs docker pnpm go wails make sqlite3 curl
command -v lsof >/dev/null 2>&1 || command -v ss >/dev/null 2>&1 \
  || die "lsof or ss is needed to check that the dev server is the e2e app."

app_pid=""
created_project=""

cleanup() {
  local status=$?
  echo "cleaning up (exit $status)"
  e2e_stop_tree "$app_pid"
  e2e_wipe_data "$CHANNEL"
  e2e_clear_keyring "$CHANNEL"
  if [[ -n "$created_project" ]]; then
    compose down -v || true
  fi
  exit "$status"
}
trap cleanup EXIT

# A stopped project from an aborted run still holds a provisioned volume, and
# provision.sh cannot run twice against the same one.
compose down -v >/dev/null 2>&1 || true
e2e_wipe_data "$CHANNEL"
e2e_clear_keyring "$CHANNEL"

echo "starting stalwart"
created_project=1
compose up -d --wait
"$E2E/provision.sh"
(cd "$APP" && go run ./e2e/seed)

echo "starting the e2e app (data in $DATA_DIR)"
rm -f "$URL_FILE" "$WAILS_LOG"
make -C "$APP" e2e-app E2E_CHANNEL="$CHANNEL" >"$WAILS_LOG" 2>&1 &
app_pid=$!

echo "waiting for frontend URL"
deadline=$((SECONDS + 300))
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
  if ! kill -0 "$app_pid" 2>/dev/null; then
    echo "the e2e app exited" >&2
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
# Anything else is not the dev server make e2e-app was told to use.
case "$url" in
  "http://127.0.0.1:${E2E_DEVSERVER_PORT}" | "http://127.0.0.1:${E2E_DEVSERVER_PORT}/"*) ;;
  *)
    echo "the dev server is at $url, expected http://127.0.0.1:${E2E_DEVSERVER_PORT}" >&2
    tail -n 80 "$WAILS_LOG" >&2
    exit 1
    ;;
esac
# The log line only says what wails dev wanted. Make sure the process that
# actually holds the port is the app this script started, not another one
# (e.g. a Pelton-dev app) that got there first.
if ! e2e_owns_port "$app_pid" "$E2E_DEVSERVER_PORT"; then
  tail -n 80 "$WAILS_LOG" >&2
  exit 1
fi
printf '%s\n' "$url" >"$URL_FILE"
echo "frontend $url"

install_playwright
PELTON_URL="$url" pnpm exec playwright test
