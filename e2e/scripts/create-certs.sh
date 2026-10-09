#!/usr/bin/env bash
# Generate the Pelton e2e local CA and localhost certificate when they are missing or do not verify.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cert_dir="$(cd "${script_dir}/.." && pwd)/certs"
ca_crt="${cert_dir}/ca.crt"
ca_key="${cert_dir}/ca.key"
server_crt="${cert_dir}/server.crt"
server_key="${cert_dir}/server.key"
fullchain="${cert_dir}/server-fullchain.crt"

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to create Pelton e2e certificates." >&2
  exit 1
fi

if [[ -f "${ca_crt}" && -f "${server_crt}" && -f "${server_key}" ]] \
  && openssl verify -CAfile "${ca_crt}" "${server_crt}" >/dev/null 2>&1; then
  echo "Pelton e2e certificates already exist and verify; leaving them unchanged."
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

openssl genrsa -out "${tmp}/ca.key" 2048
cat > "${tmp}/ca.cnf" <<'EOF'
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no

[dn]
CN = Pelton e2e Local CA

[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
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

mkdir -p "${cert_dir}"
umask 077
cp "${tmp}/ca.crt" "${ca_crt}"
cp "${tmp}/ca.key" "${ca_key}"
cp "${tmp}/server.crt" "${server_crt}"
cp "${tmp}/server.key" "${server_key}"
cp "${tmp}/server-fullchain.crt" "${fullchain}"
chmod 644 "${ca_crt}" "${server_crt}" "${fullchain}"
chmod 600 "${ca_key}" "${server_key}"

echo "Wrote Pelton e2e certificates to ${cert_dir}"
