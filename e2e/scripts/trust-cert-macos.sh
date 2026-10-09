#!/usr/bin/env bash
# Create the Pelton e2e certificates, then trust ca.crt in the macOS login keychain.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "trust-cert-macos.sh only runs on macOS (Darwin)." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${script_dir}/create-certs.sh"

ca_crt="$(cd "${script_dir}/.." && pwd)/certs/ca.crt"
keychain="${HOME}/Library/Keychains/login.keychain-db"

echo "macOS may prompt for your password to trust the Pelton e2e local CA in the login keychain."
security add-trusted-cert -d -r trustRoot -k "${keychain}" "${ca_crt}"
