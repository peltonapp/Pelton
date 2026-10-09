#!/usr/bin/env bash
# Create the Pelton e2e certificates, then install ca.crt into the Linux system trust store.

set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "trust-cert-linux.sh only runs on Linux." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${script_dir}/create-certs.sh"

ca_crt="$(cd "${script_dir}/.." && pwd)/certs/ca.crt"
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
  dest="/usr/local/share/ca-certificates/pelton-e2e-local-ca.crt"
  if [[ ! -d /usr/local/share/ca-certificates ]]; then
    run_sudo mkdir -p /usr/local/share/ca-certificates
  fi
  run_sudo cp "${ca_crt}" "${dest}"
  run_sudo update-ca-certificates
else
  if command -v trust >/dev/null 2>&1; then
    run_sudo trust anchor --store "${ca_crt}"
  elif command -v update-ca-trust >/dev/null 2>&1; then
    anchor_dir="/etc/pki/ca-trust/source/anchors"
    dest="${anchor_dir}/pelton-e2e-local-ca.crt"
    if [[ ! -d "${anchor_dir}" ]]; then
      run_sudo mkdir -p "${anchor_dir}"
    fi
    run_sudo cp "${ca_crt}" "${dest}"
    run_sudo update-ca-trust
  else
    echo "Neither trust nor update-ca-trust is installed." >&2
    exit 1
  fi
fi

echo "Trusted Pelton e2e local CA in the system trust store."
