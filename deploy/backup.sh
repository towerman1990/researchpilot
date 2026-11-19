#!/usr/bin/env bash
set -euo pipefail
umask 077

deploy_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
env_file="${RESEARCHPILOT_ENV_FILE:-$deploy_dir/.env}"
output="${1:?Usage: bash deploy/backup.sh OUTPUT.sql.gz}"
if [[ -e "$output" ]]; then
  printf 'Backup already exists: %s\n' "$output" >&2
  exit 1
fi

temporary="$(mktemp -- "${output}.tmp.XXXXXX")"
trap 'rm -f -- "$temporary"' EXIT
docker compose --env-file "$env_file" -f "$deploy_dir/compose.yaml" exec -T mysql sh -ec '
  export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
  exec mysqldump -uroot --single-transaction --no-tablespaces --set-gtid-purged=OFF --databases researchpilot
' | gzip -c > "$temporary"
gzip -t -- "$temporary"
# Publish atomically without overwriting an existing backup.
ln -- "$temporary" "$output"
printf 'Backup saved: %s\n' "$output"
