#!/usr/bin/env bash
# Provision the pelton-e2e Stalwart: password policy, example.org, alice and bob,
# then install the already-trusted server certificate and reload TLS.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

compose() {
  docker compose -p pelton-e2e -f docker-compose.yml "$@"
}

cli() {
  compose run --rm --no-deps cli "$@"
}

last_token() {
  awk 'NF { line=$0 } END { print $NF }' <<<"$1"
}

echo "waiting for http://127.0.0.1:18081/healthz/ready"
deadline=$((SECONDS + 90))
until curl -fsS "http://127.0.0.1:18081/healthz/ready" >/dev/null; do
  if (( SECONDS > deadline )); then
    echo "stalwart did not become ready" >&2
    exit 1
  fi
  sleep 1
done

echo "lowering password policy"
cli update Authentication \
  --field passwordMinStrength=zero \
  --field passwordMinLength=1
cli create Action --field @type=ReloadSettings

echo "creating domain example.org"
cli create Domain --field name=example.org --field isEnabled=true
domain_out="$(cli query Domain --where name=example.org --fields id)"
domain_id="$(last_token "$domain_out")"
if [[ -z "$domain_id" || "$domain_id" == "id" ]]; then
  echo "domain id missing in: $domain_out" >&2
  exit 1
fi
echo "domain id $domain_id"

for name in alice bob; do
  echo "creating $name@example.org"
  cli create Account/User \
    --field "name=$name" \
    --field "domainId=$domain_id" \
    --field "credentials={\"0\":{\"@type\":\"Password\",\"secret\":\"${name}-e2e\"}}" \
    --field 'roles={"@type":"User"}' \
    --field 'permissions={"@type":"Inherit"}' \
    --field 'encryptionAtRest={"@type":"Disabled"}' \
    --field 'aliases={}' \
    --field 'memberGroupIds={}' \
    --field 'quotas={"maxDiskQuota":20000000000,"maxEmails":200000}'
done

echo "raising message limits"
cli update Email \
  --field maxMessageSize=104857600 \
  --field maxAttachmentSize=104857600 \
  --field maxMessages=500000
cli update Http \
  --field 'rateLimitAuthenticated={"count":1000000,"period":3600000}' \
  --field 'rateLimitAnonymous={"count":100000,"period":3600000}'
cli update Imap \
  --field 'maxRequestRate={"count":1000000,"period":3600000}'
cli create Action --field @type=ReloadSettings

echo "installing TLS certificate"
cert_out="$(cli create Certificate \
  --field 'certificate={"@type":"File","filePath":"/certs/server-fullchain.crt"}' \
  --field 'privateKey={"@type":"File","filePath":"/certs/server.key"}')"
echo "$cert_out"
cert_id="$(last_token "$cert_out")"
if [[ -z "$cert_id" || "$cert_id" == "id" ]]; then
  echo "certificate id missing" >&2
  exit 1
fi
echo "certificate id $cert_id"

cli update SystemSettings \
  --field defaultHostname=127.0.0.1 \
  --field "defaultDomainId=$domain_id" \
  --field "defaultCertificateId=$cert_id"
cli create Action --field @type=ReloadTlsCertificates
cli create Action --field @type=ReloadSettings

echo "provisioned"
