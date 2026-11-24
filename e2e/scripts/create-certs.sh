#!/usr/bin/env bash
# Generate the Pelton e2e local CA and the localhost server certificate.
#
# The CA is name-constrained to 127.0.0.1 and localhost (with pathlen:0), so
# even once it is trusted system-wide it cannot vouch for any other host. Its
# private key only ever lives in a temporary directory: it signs the server
# certificate and is deleted. Nothing in e2e/certs can mint new certificates.
#
# A CA that is already there is never replaced silently. Regenerating would
# leave the old one trusted, so the script stops and asks for
# untrust-cert-<os>.sh first.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=e2e/scripts/ca-common.sh
. "${script_dir}/ca-common.sh"

cert_dir="${E2E_CERT_DIR}"
ca_crt="${cert_dir}/ca.crt"
legacy_ca_key="${cert_dir}/ca.key"
server_crt="${cert_dir}/server.crt"
server_key="${cert_dir}/server.key"
fullchain="${cert_dir}/server-fullchain.crt"

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to create Pelton e2e certificates." >&2
  exit 1
fi

untrust_hint() {
  case "$(uname -s)" in
    Darwin) echo "  e2e/scripts/untrust-cert-macos.sh" ;;
    *) echo "  e2e/scripts/untrust-cert-linux.sh" ;;
  esac
}

# An older version of this script kept the CA key in e2e/certs. That key can
# sign a certificate for any host the CA is trusted for, so it must go.
if [[ -f "${legacy_ca_key}" ]]; then
  echo "Found ${legacy_ca_key}, the key of an earlier, unconstrained Pelton e2e CA." >&2
  echo "Remove that CA from the trust store and delete e2e/certs first:" >&2
  untrust_hint >&2
  exit 1
fi

if [[ -f "${ca_crt}" ]]; then
  if ! ca_is_constrained "${ca_crt}"; then
    echo "${ca_crt} has no name constraints: it is an earlier Pelton e2e CA" >&2
    echo "that could vouch for any host. Untrust it and delete e2e/certs first:" >&2
    untrust_hint >&2
    exit 1
  fi
  if [[ -f "${server_crt}" && -f "${server_key}" && -f "${fullchain}" ]] \
    && openssl verify -CAfile "${ca_crt}" "${server_crt}" >/dev/null 2>&1; then
    # Older versions left the directory 0700; see the chmod below.
    chmod 755 "${cert_dir}"
    echo "Pelton e2e certificates already exist and verify; leaving them unchanged."
    exit 0
  fi
  echo "${ca_crt} exists but the server certificate is missing or does not verify." >&2
  echo "A new CA would leave this one trusted. Untrust it and delete e2e/certs first:" >&2
  untrust_hint >&2
  exit 1
fi

# No ca.crt here, but an older one may still be trusted (e2e/certs deleted by
# hand). Generating another would stack a second Pelton e2e CA on top.
if ca_trusted_elsewhere; then
  echo "A \"${E2E_CA_CN}\" is still trusted on this machine, but e2e/certs/ca.crt is gone." >&2
  echo "Remove it from the trust store first:" >&2
  untrust_hint >&2
  exit 1
fi

tmp="$(mktemp -d)"
# The CA key exists only in here.
trap 'rm -rf "${tmp}"' EXIT
umask 077

openssl genrsa -out "${tmp}/ca.key" 2048
cat > "${tmp}/ca.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no

[dn]
CN = ${E2E_CA_CN}

[v3_ca]
basicConstraints = critical,CA:TRUE,pathlen:0
keyUsage = critical,keyCertSign,cRLSign
nameConstraints = critical,permitted;IP:127.0.0.1/255.255.255.255,permitted;DNS:localhost
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer
EOF
openssl req -x509 -new -key "${tmp}/ca.key" -sha256 -days 825 \
  -out "${tmp}/ca.crt" \
  -config "${tmp}/ca.cnf"

openssl genrsa -out "${tmp}/server.key" 2048
openssl req -new -key "${tmp}/server.key" -sha256 \
  -out "${tmp}/server.csr" \
  -subj "/CN=localhost"

cat > "${tmp}/server.ext" <<'EOF'
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:localhost,IP:127.0.0.1
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
EOF
openssl x509 -req \
  -in "${tmp}/server.csr" \
  -CA "${tmp}/ca.crt" \
  -CAkey "${tmp}/ca.key" \
  -CAcreateserial \
  -out "${tmp}/server.crt" \
  -days 825 \
  -sha256 \
  -extfile "${tmp}/server.ext"

cat "${tmp}/server.crt" "${tmp}/ca.crt" > "${tmp}/server-fullchain.crt"
openssl verify -CAfile "${tmp}/ca.crt" "${tmp}/server.crt" >/dev/null
rm -f "${tmp}/ca.key" "${tmp}/ca.srl"

mkdir -p "${cert_dir}"
# umask 077 above would leave it 0700. Stalwart reads it through a bind mount
# as its own user, which on Linux needs to traverse it; nothing in it can sign.
chmod 755 "${cert_dir}"
cp "${tmp}/ca.crt" "${ca_crt}"
cp "${tmp}/server.crt" "${server_crt}"
cp "${tmp}/server.key" "${server_key}"
cp "${tmp}/server-fullchain.crt" "${fullchain}"
chmod 644 "${ca_crt}" "${server_crt}" "${fullchain}"
# Stalwart reads this through the read-only bind mount. See e2e/README.md if
# the container runs as a user that cannot read a 0600 file.
chmod 600 "${server_key}"

echo "Wrote Pelton e2e certificates to ${cert_dir} (the CA key was not kept)."
