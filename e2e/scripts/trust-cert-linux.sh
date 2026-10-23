#!/usr/bin/env bash
# Create the Pelton e2e certificates, then install ca.crt into the Linux system trust store.
# The CA is limited to 127.0.0.1 and localhost. untrust-cert-linux.sh removes it.

set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "trust-cert-linux.sh only runs on Linux." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=e2e/scripts/ca-common.sh
. "${script_dir}/ca-common.sh"
"${script_dir}/create-certs.sh"

ca_crt="${E2E_CERT_DIR}/ca.crt"
if [[ ! -f "${ca_crt}" ]]; then
  echo "Missing ${ca_crt} after certificate creation." >&2
  exit 1
fi

id=""
id_like=""
if [[ -f /etc/os-release ]]; then
  id="$(sed -n 's/^ID=//p' /etc/os-release | head -n 1 | tr -d '"')"
  id_like="$(sed -n 's/^ID_LIKE=//p' /etc/os-release | head -n 1 | tr -d '"')"
fi

family=""
case " ${id} ${id_like} " in
  *" debian "*|*" ubuntu "*) family="debian" ;;
  *" fedora "*|*" rhel "*|*" centos "*) family="rhel" ;;
esac

if [[ -z "${family}" ]]; then
  if command -v update-ca-certificates >/dev/null 2>&1; then
    family="debian"
  elif command -v trust >/dev/null 2>&1 || command -v update-ca-trust >/dev/null 2>&1; then
    family="rhel"
  else
    echo "Unsupported Linux distribution: no update-ca-certificates, trust, or update-ca-trust." >&2
    exit 1
  fi
fi

run_sudo() {
  printf 'Running:'
  printf ' %q' sudo "$@"
  printf '\n'
  sudo "$@"
}

if [[ "${family}" == "debian" ]]; then
  if ! command -v update-ca-certificates >/dev/null 2>&1; then
    echo "update-ca-certificates is not installed." >&2
    exit 1
  fi
  dest="${E2E_DEBIAN_ANCHOR}"
  if [[ ! -d /usr/local/share/ca-certificates ]]; then
    run_sudo mkdir -p /usr/local/share/ca-certificates
  fi
  run_sudo cp "${ca_crt}" "${dest}"
  run_sudo update-ca-certificates
else
  # On Fedora/RHEL a file in the anchors directory is easy to find and remove
  # again, so it is preferred. Elsewhere (e.g. Arch) `trust anchor` knows the
  # distribution's own anchor location.
  if command -v update-ca-trust >/dev/null 2>&1 && [[ -d /etc/pki/ca-trust/source ]]; then
    run_sudo mkdir -p "$(dirname "${E2E_RHEL_ANCHOR}")"
    run_sudo cp "${ca_crt}" "${E2E_RHEL_ANCHOR}"
    run_sudo update-ca-trust
  elif command -v trust >/dev/null 2>&1; then
    run_sudo trust anchor --store "${ca_crt}"
  else
    echo "Neither update-ca-trust nor trust is installed." >&2
    exit 1
  fi
fi

echo "Trusted Pelton e2e local CA in the system trust store."
echo "Remove it again with e2e/scripts/untrust-cert-linux.sh."
