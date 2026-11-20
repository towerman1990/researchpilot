#!/usr/bin/env bash
set -euo pipefail
project="$PWD"
db="rp_m2_$(date +%s)_$$"
mysql_container=researchpilot-mysql-1
redis_container=''
mysql() {
    podman exec "$mysql_container" sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -e "$1"' sh "$1"
}
cleanup() {
    if [[ -n "$redis_container" ]]; then
        podman stop "$redis_container" >/dev/null 2>&1 || true
    fi
    case "$db" in rp_m2_[0-9]*)
        mysql "DROP DATABASE IF EXISTS $db;" >/dev/null 2>&1 || true;;
    esac
}
trap cleanup EXIT
podman start "$mysql_container" >/dev/null
for attempt in $(seq 1 30); do
    if mysql 'SELECT 1;' >/dev/null 2>&1; then break; fi
    sleep 1
done
mysql "CREATE DATABASE $db; GRANT ALL ON $db.* TO 'rp'@'%';"
# Share the MySQL network namespace so test traffic to Redis stays on loopback.
redis_image="$(podman inspect researchpilot-redis-1 --format '{{.ImageName}}')"
redis_container="$(podman run --pull=never --rm -d --network "container:$mysql_container" "$redis_image")"
podman run --rm --network "container:$mysql_container" -v "$project:/src:ro" -w /src \
    -e "TEST_MYSQL_DSN=rp:rp-local@tcp(127.0.0.1:3306)/$db" \
    -e MODEL_MASTER_KEY_FILE=/src/.cache/model-master.key \
    -e TEST_REDIS_ADDR=127.0.0.1:6379 \
    -e MIGRATIONS_DIR=/src/migrations --entrypoint /bin/sh \
    docker.io/library/alpine:3.22 -ec '
        for package in migrations persistence service execution; do
            "/src/.cache/$package-integration" -test.v -test.timeout=60s
        done
    '
