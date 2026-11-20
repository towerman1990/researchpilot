#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

engine="${CONTAINER_ENGINE:-docker}"
project="rp-production-smoke-$$-$RANDOM"
temp="$(mktemp -d)"
# Bind-mounted Compose secrets must be readable by the image's non-root user.
chmod 755 "$temp"
export RESEARCHPILOT_IMAGE="$project:local"
export RESEARCHPILOT_DOMAIN=localhost RESEARCHPILOT_SCHEME=http
export BRAVE_SEARCH_API_KEY= GITHUB_READ_TOKEN=
export MYSQL_PASSWORD=smoke-test-password MYSQL_ROOT_PASSWORD=smoke-root-password
export MODEL_MASTER_KEY_PATH="$temp/model-master.key"
export TRIAL_MODEL_API_KEY_PATH="$temp/trial-model.key"
umask 077
openssl rand -hex 32 > "$MODEL_MASTER_KEY_PATH"
printf '%s' 'fixture-trial-key' > "$TRIAL_MODEL_API_KEY_PATH"
chmod 644 "$MODEL_MASTER_KEY_PATH" "$TRIAL_MODEL_API_KEY_PATH"

compose=("$engine" compose -p "$project" -f deploy/compose.yaml)
trial=("${compose[@]}" -f deploy/compose.trial.yaml)
cleanup() {
    local status=$?
    if (( status != 0 )); then
        "${trial[@]}" ps -a >&2 || true
        "${trial[@]}" logs --no-color --tail=60 >&2 || true
    fi
    "${trial[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
    "$engine" image rm "$RESEARCHPILOT_IMAGE" >/dev/null 2>&1 || true
    rm -f "$MODEL_MASTER_KEY_PATH" "$TRIAL_MODEL_API_KEY_PATH" "$temp/cookies"
    rmdir "$temp" || true
}
trap cleanup EXIT

"${trial[@]}" config --format json | python3 -c '
import json, os, sys
config = json.load(sys.stdin)
api = config["services"]["api"]
worker = config["services"]["worker"]
api_secrets = {entry["source"] for entry in api["secrets"]}
worker_secrets = {entry["source"] for entry in worker["secrets"]}
assert api["environment"]["TRIAL_MODEL_ENABLED"] == "true"
assert api["environment"]["TRIAL_MODEL_API_KEY_FILE"] == "/run/secrets/trial_model_api_key"
assert api_secrets == {"model_master_key", "trial_model_api_key"}
assert worker_secrets == {"model_master_key"}
assert config["secrets"]["trial_model_api_key"]["file"] == os.environ["TRIAL_MODEL_API_KEY_PATH"]
'

"$engine" build -t "$RESEARCHPILOT_IMAGE" .
"${compose[@]}" up -d
base=http://localhost
wait_ready() {
    for ((i=0; i<60; i++)); do
        if curl -fsS --max-time 3 "$base/readyz" >/dev/null 2>&1; then return 0; fi
        sleep 2
    done
    echo 'Production proxy did not reach API readiness' >&2
    return 1
}
wait_ready
curl -fsS --max-time 3 "$base/queuez" >/dev/null
api="$base/api/v1"
credentials='{"email":"production-smoke@example.test","password":"ci-smoke-password-2026"}'
curl -fsS --max-time 5 -H 'Content-Type: application/json' -d "$credentials" "$api/auth/register" >/dev/null
curl -fsS --max-time 5 -H 'Content-Type: application/json' -d "$credentials" \
    -c "$temp/cookies" "$api/auth/login" >/dev/null
task="$(curl -fsS --max-time 5 -b "$temp/cookies" \
    -H 'Content-Type: application/json' -H 'Idempotency-Key: production-smoke-fixture' \
    -d '{"goal":"Validate production proxy and Worker"}' "$api/research/tasks" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["task_id"])')"
stream="$(curl -fsSN --max-time 45 -b "$temp/cookies" "$api/research/tasks/$task/events?after=0")"
grep -q '^event: report_ready' <<< "$stream"
curl -fsS --max-time 5 -b "$temp/cookies" "$api/research/tasks/$task/report" |
    python3 -c 'import json,sys; assert "M1" in json.load(sys.stdin)["markdown"]'

"${trial[@]}" up -d --no-deps api
wait_ready
"${trial[@]}" exec -T api sh -ec 'test -r "$TRIAL_MODEL_API_KEY_FILE" && test -s "$TRIAL_MODEL_API_KEY_FILE"'
curl -fsS --max-time 5 -b "$temp/cookies" "$api/model-config" |
    python3 -c 'import json,sys; config=json.load(sys.stdin); assert config["trial_enabled"] is True and config["trial_remaining"] == 10'
echo 'Production Compose smoke passed via Caddy, including trial secret configuration.'
