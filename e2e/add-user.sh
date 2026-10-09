#!/usr/bin/env bash
# Add accounts to a running, provisioned pelton-e2e Stalwart. Each name becomes
# <name>@example.org with the password <name>-e2e.
#   e2e/add-user.sh carol dave
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 name [name...]" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

cli() {
  docker compose -p pelton-e2e -f docker-compose.yml run --rm --no-deps cli "$@"
}

domain_out="$(cli query Domain --where name=example.org --fields id)"
domain_id="$(awk 'NF { line=$0 } END { print $NF }' <<<"$domain_out")"
if [[ -z "$domain_id" || "$domain_id" == "id" ]]; then
  echo "domain example.org missing, run provision.sh first: $domain_out" >&2
  exit 1
fi

for name in "$@"; do
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
