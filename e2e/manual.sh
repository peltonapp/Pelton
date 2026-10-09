#!/usr/bin/env bash
# Manual Stalwart playground: the same Stalwart, provisioning and seed as run.sh,
# then `make run` in the foreground instead of Playwright. Add the mailboxes
# yourself and send mail between them. Quit Pelton (or Ctrl-C) to stop: the trap
# removes the compose project and wipes Pelton-dev unless E2E_KEEP=1.
#
#   E2E_SEED_PER_INBOX=200  messages seeded into alice and bob (0 skips seeding)
#   E2E_KEEP=1              leave Stalwart running and Pelton-dev in place on exit
#   E2E_FORCE=1             take down a pelton-e2e project that is already running
set -euo pipefail

E2E="$(cd "$(dirname "$0")" && pwd)"
APP="$(cd "$E2E/.." && pwd)"

if [[ "$(uname -s)" == "Darwin" ]]; then
  DEV_DATA="${HOME}/Library/Application Support/Pelton-dev"
else
  DEV_DATA="${XDG_CONFIG_HOME:-${HOME}/.config}/Pelton-dev"
fi

if [[ "$DEV_DATA" != *"/Pelton-dev" ]]; then
  echo "refusing to touch a non-dev data directory: $DEV_DATA" >&2
  exit 1
fi

if [[ ! -f "$E2E/certs/ca.crt" ]]; then
  echo "e2e/certs is missing. Run e2e/scripts/trust-cert-macos.sh or" >&2
  echo "e2e/scripts/trust-cert-linux.sh first." >&2
  exit 1
fi

compose() {
  (cd "$E2E" && docker compose -p pelton-e2e -f docker-compose.yml "$@")
}

cleanup() {
  local status=$?
  if [[ "${E2E_KEEP:-}" == "1" ]]; then
    echo "E2E_KEEP=1: Stalwart is still running and $DEV_DATA is kept."
    echo "Stop it with: docker compose -p pelton-e2e -f e2e/docker-compose.yml down -v"
    exit "$status"
  fi
  echo "cleaning up"
  compose down -v || true
  rm -rf "$DEV_DATA"
  exit "$status"
}

# Start from scratch: provision.sh cannot run twice against the same volume.
# A running pelton-e2e may be someone else's run.sh, so do not take it down
# without being asked to.
if [[ -n "$(compose ps -q 2>/dev/null)" && "${E2E_FORCE:-}" != "1" ]]; then
  echo "pelton-e2e is already running (another run.sh or manual.sh?)." >&2
  echo "Stop it first, or rerun with E2E_FORCE=1 to take it down." >&2
  exit 1
fi
compose down -v >/dev/null 2>&1 || true
rm -rf "$DEV_DATA"
trap cleanup EXIT

echo "starting stalwart"
compose up -d --wait
"$E2E/provision.sh"
"$E2E/add-user.sh" carol dave

count="${E2E_SEED_PER_INBOX:-200}"
if [[ "$count" != "0" ]]; then
  echo "seeding $count messages into alice and bob"
  (cd "$APP" && E2E_SEED_PER_INBOX="$count" go run ./e2e/seed)
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

EOF

cd "$APP"
make run
