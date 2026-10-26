# Shared by create-certs.sh and the trust/untrust scripts. Sourced, not run.
# shellcheck shell=bash disable=SC2034  # the variables are for the sourcing scripts

E2E_CERT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/certs"
# Every generation of the CA carries this name, so untrust can find all of them.
E2E_CA_CN="Pelton e2e Local CA"
E2E_MACOS_KEYCHAIN="${HOME}/Library/Keychains/login.keychain-db"
E2E_DEBIAN_ANCHOR="/usr/local/share/ca-certificates/pelton-e2e-local-ca.crt"
E2E_RHEL_ANCHOR="/etc/pki/ca-trust/source/anchors/pelton-e2e-local-ca.crt"

# ca_is_constrained reports whether the certificate carries the name
# constraints create-certs.sh writes.
ca_is_constrained() {
  openssl x509 -in "$1" -noout -text 2>/dev/null | grep -q 'X509v3 Name Constraints'
}

# macos_ca_hashes prints the SHA-1 of every Pelton e2e CA in the keychain
# search list.
macos_ca_hashes() {
  { security find-certificate -a -c "${E2E_CA_CN}" -Z 2>/dev/null || true; } \
    | awk '/^SHA-1 hash:/ { print $3 }'
}

# macos_has_trust_settings reports whether a certificate named E2E_CA_CN has
# trust settings in a domain: "user" (security add-trusted-cert without -d,
# what trust-cert-macos.sh writes) or "admin" (-d, machine-wide, what older
# versions wrote). Trust settings outlive the keychain item, so the keychain
# alone does not show whether the CA is still trusted. It only reads them.
macos_has_trust_settings() {
  local flag=""
  [[ "$1" == "admin" ]] && flag="-d"
  # "No Trust Settings were found" exits non-zero and prints nothing on stdout.
  # shellcheck disable=SC2086  # $flag is empty or -d
  { security dump-trust-settings ${flag} 2>/dev/null || true; } | awk -v cn="${E2E_CA_CN}" '
    /^Cert [0-9]+: / { sub(/^Cert [0-9]+: /, ""); sub(/[[:space:]]+$/, ""); if ($0 == cn) found = 1 }
    END { exit (found ? 0 : 1) }
  '
}

# macos_trust_domains prints each domain ("user", "admin") that still holds
# trust settings for a Pelton e2e CA.
macos_trust_domains() {
  local domain
  for domain in user admin; do
    if macos_has_trust_settings "${domain}"; then
      echo "${domain}"
    fi
  done
}

# linux_trust_uris prints the p11-kit URI of every Pelton e2e CA anchor.
linux_trust_uris() {
  command -v trust >/dev/null 2>&1 || return 0
  trust list --filter=ca-anchors 2>/dev/null | awk -v cn="${E2E_CA_CN}" '
    /^pkcs11:/ { uri = $0 }
    $1 == "label:" { sub(/^[[:space:]]*label:[[:space:]]*/, ""); if ($0 == cn) print uri }
  '
}

# ca_trusted_elsewhere reports whether some Pelton e2e CA is still trusted on
# this machine. It only reads the trust store.
ca_trusted_elsewhere() {
  case "$(uname -s)" in
    Darwin)
      [[ -n "$(macos_ca_hashes)" || -n "$(macos_trust_domains)" ]]
      ;;
    Linux)
      [[ -f "${E2E_DEBIAN_ANCHOR}" || -f "${E2E_RHEL_ANCHOR}" || -n "$(linux_trust_uris)" ]]
      ;;
    *)
      return 1
      ;;
  esac
}
