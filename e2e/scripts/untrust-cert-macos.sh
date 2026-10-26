#!/usr/bin/env bash
# Remove every Pelton e2e local CA from the macOS keychains (trust settings
# included) and delete e2e/certs. Run it when you are done with the e2e suite,
# or before regenerating the certificates.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "untrust-cert-macos.sh only runs on macOS (Darwin)." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=e2e/scripts/ca-common.sh
. "${script_dir}/ca-common.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# One PEM file per Pelton e2e CA in the keychain search list, plus e2e/certs/ca.crt:
# trust settings can outlive the keychain item, and removing them needs the
# certificate.
# find-certificate exits 44 when there is none; with pipefail that would end the script.
{ security find-certificate -a -c "${E2E_CA_CN}" -p 2>/dev/null || true; } \
  | awk -v dir="${tmp}" '/-----BEGIN CERTIFICATE-----/ { n++ } n { print > (dir "/ca-" n ".pem") }'
if [[ -f "${E2E_CERT_DIR}/ca.crt" ]]; then
  cp "${E2E_CERT_DIR}/ca.crt" "${tmp}/ca-file.pem"
fi

# remove_trust removes the trust settings of $2 in domain $1 (user or admin).
# "could not be found" means the certificate had none there; anything else is
# printed. The check after the loop decides whether the script failed.
remove_trust() {
  local domain="$1" pem="$2" out
  local args=()
  [[ "${domain}" == "admin" ]] && args=(-d)
  # ${args[@]+...}: bash 3.2 (macOS) treats an empty array as unset under -u.
  if ! out="$(security remove-trusted-cert ${args[@]+"${args[@]}"} "${pem}" 2>&1)"; then
    case "${out}" in
      *"could not be found"* | *"No Trust Settings"*) ;;
      *) echo "removing the ${domain} trust settings failed: ${out}" >&2 ;;
    esac
  fi
}

# bash 3.2 (macOS /bin/bash) has no associative arrays: dedupe in a string.
seen=" "
hashes=()
for pem in "${tmp}"/ca-*.pem; do
  [[ -f "${pem}" ]] || continue
  sha1="$(openssl x509 -in "${pem}" -noout -fingerprint -sha1 | sed 's/.*=//; s/://g')"
  [[ "${seen}" != *" ${sha1} "* ]] || continue
  seen="${seen}${sha1} "
  echo "removing the trust settings of ${E2E_CA_CN} ${sha1}"
  echo "macOS may prompt for your password to change the trust settings."
  # trust-cert-macos.sh sets user trust settings; older versions used the
  # admin domain (-d). Remove both.
  remove_trust user "${pem}"
  remove_trust admin "${pem}"
  hashes+=("${sha1}")
done

# Trust settings are what make macOS trust the CA. Check them before the
# certificates go: while the certificate is still in the keychain, Keychain
# Access (or a rerun) can still remove its trust.
left="$(macos_trust_domains | paste -sd, -)"
if [[ -n "${left}" ]]; then
  echo "${E2E_CA_CN} still has trust settings in the ${left} domain, so it is still trusted" >&2
  echo "(see security dump-trust-settings, and dump-trust-settings -d for admin)." >&2
  echo "Remove them in Keychain Access, or with security remove-trusted-cert [-d] <CA PEM>," >&2
  echo "then run this script again. The certificates and e2e/certs are kept." >&2
  exit 1
fi

removed=0
for sha1 in "${hashes[@]+"${hashes[@]}"}"; do
  # Only certificates still in a keychain; ca-file.pem may not be one.
  if macos_ca_hashes | grep -qix "${sha1}"; then
    echo "deleting ${E2E_CA_CN} ${sha1} from the keychain"
    security delete-certificate -Z "${sha1}" -t
    removed=$((removed + 1))
  fi
done

if (( removed == 0 )); then
  echo "No ${E2E_CA_CN} found in the keychains."
fi
if [[ -n "$(macos_ca_hashes)" ]]; then
  echo "A ${E2E_CA_CN} is still in a keychain. Remove it in Keychain Access." >&2
  exit 1
fi

if [[ -d "${E2E_CERT_DIR}" ]]; then
  rm -rf "${E2E_CERT_DIR}"
  echo "Deleted ${E2E_CERT_DIR}."
fi
