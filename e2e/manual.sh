#!/usr/bin/env bash
# Manual Stalwart playground: the same Stalwart, provisioning and seed as run.sh,
# then the app in the foreground instead of Playwright. Add the mailboxes
# yourself and send mail between them. Quit Pelton (or Ctrl-C) to stop.
#
# The app is built as channel "e2e-manual" (make e2e-app), so its data
# directory and keyring service are Pelton-e2e-manual, never Pelton-dev. On
# exit the trap deletes both and removes the compose project, unless E2E_KEEP=1.
#
#   E2E_SEED_PER_INBOX=200  messages seeded into alice and bob (0 skips seeding)
#   E2E_KEEP=1              leave Stalwart running and Pelton-e2e-manual in place on exit
#   E2E_FORCE=1             take down a pelton-e2e project that is already running
set -euo pipefail

# shellcheck source=e2e/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

CHANNEL=e2e-manual
DATA_DIR="$(e2e_data_dir "$CHANNEL")"

e2e_check_prereqs docker go wails make curl

created_project=""

cleanup() {
  local status=$?
  if [[ "${E2E_KEEP:-}" == "1" ]]; then
    echo "E2E_KEEP=1: Stalwart is still running and $DATA_DIR is kept."
    echo "Stop it with: docker compose -p pelton-e2e -f e2e/docker-compose.yml down -v"
    exit "$status"
  fi
  echo "cleaning up"
  if [[ -n "$created_project" ]]; then
    compose down -v || true
  fi
  e2e_wipe_data "$CHANNEL"
  e2e_clear_keyring "$CHANNEL"
  exit "$status"
}
trap cleanup EXIT

# Start from scratch: provision.sh cannot run twice against the same volume.
compose down -v >/dev/null 2>&1 || true
e2e_wipe_data "$CHANNEL"
e2e_clear_keyring "$CHANNEL"

echo "starting stalwart"
created_project=1
compose up -d --wait
"$E2E/provision.sh"
"$E2E/add-user.sh" carol dave

count="${E2E_SEED_PER_INBOX:-200}"
if [[ "$count" != "0" ]]; then
  echo "seeding $count messages into alice and bob"
  # run.sh's suite reads e2e/manifest.json; leave it alone.
  (cd "$APP" && E2E_SEED_PER_INBOX="$count" E2E_MANIFEST=manual-manifest.json go run ./e2e/seed)
fi

cat <<EOF

Stalwart is ready. Add any of these in Pelton under "Other" (or in any mail client):

  account              password    seeded
  alice@example.org    alice-e2e   $count messages
  bob@example.org      bob-e2e     $count messages
  carol@example.org    carol-e2e   empty
  dave@example.org     dave-e2e    empty

  IMAP host  127.0.0.1   993 (TLS)
  SMTP host  127.0.0.1   465 (TLS)

Mail between @example.org accounts is delivered locally, so you can send from
one mailbox and read it in another.

Add more accounts from another terminal while this runs:
  e2e/add-user.sh erin    # erin@example.org / erin-e2e

Pelton's data is in $DATA_DIR.

EOF

make -C "$APP" e2e-app E2E_CHANNEL="$CHANNEL"
