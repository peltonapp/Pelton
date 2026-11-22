#!/usr/bin/env bash
# Remove every Pelton e2e local CA from the Linux system trust store and delete
# e2e/certs. Run it when you are done with the e2e suite, or before
# regenerating the certificates.

set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "untrust-cert-linux.sh only runs on Linux." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=e2e/scripts/ca-common.sh
. "${script_dir}/ca-common.sh"

run_sudo() {
  printf 'Running:'
  printf ' %q' sudo "$@"
  printf '\n'
  sudo "$@"
}

if [[ -f "${E2E_DEBIAN_ANCHOR}" ]]; then
  run_sudo rm -f "${E2E_DEBIAN_ANCHOR}"
  run_sudo update-ca-certificates --fresh
fi

if [[ -f "${E2E_RHEL_ANCHOR}" ]]; then
  run_sudo rm -f "${E2E_RHEL_ANCHOR}"
  run_sudo update-ca-trust
fi

# Anchors stored with `trust anchor --store` (Arch and similar).
while IFS= read -r uri; do
  [[ -n "${uri}" ]] || continue
  run_sudo trust anchor --remove "${uri}"
done < <(linux_trust_uris)

if ca_trusted_elsewhere; then
  echo "A ${E2E_CA_CN} is still trusted. Check \`trust list --filter=ca-anchors\`." >&2
  exit 1
fi
echo "No ${E2E_CA_CN} is trusted any more."

if [[ -d "${E2E_CERT_DIR}" ]]; then
  # run.sh used to let Docker create e2e/certs as root when it was missing.
  if ! rm -rf "${E2E_CERT_DIR}" 2>/dev/null; then
    run_sudo rm -rf "${E2E_CERT_DIR}"
  fi
  echo "Deleted ${E2E_CERT_DIR}."
fi
