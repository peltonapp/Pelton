#!/usr/bin/env bash
# Create the Pelton e2e certificates, then trust ca.crt for the current user:
# the certificate goes into the login keychain with user trust settings, not
# the machine-wide admin domain (-d). The CA is limited to 127.0.0.1 and
# localhost. untrust-cert-macos.sh removes it.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "trust-cert-macos.sh only runs on macOS (Darwin)." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=e2e/scripts/ca-common.sh
. "${script_dir}/ca-common.sh"

# Older versions trusted the CA machine-wide (admin domain, -d). Adding user
# trust on top would leave that in place, so stop until it is gone.
if macos_has_trust_settings admin; then
  echo "A \"${E2E_CA_CN}\" is trusted machine-wide (admin trust settings, from an older" >&2
  echo "version of this script). Remove it first, then run this script again:" >&2
  echo "  e2e/scripts/untrust-cert-macos.sh" >&2
  exit 1
fi

"${script_dir}/create-certs.sh"

ca_crt="${E2E_CERT_DIR}/ca.crt"

echo "macOS may prompt for your password to trust the Pelton e2e local CA for your user."
security add-trusted-cert -r trustRoot -k "${E2E_MACOS_KEYCHAIN}" "${ca_crt}"
echo "Trusted. Remove it again with e2e/scripts/untrust-cert-macos.sh."
