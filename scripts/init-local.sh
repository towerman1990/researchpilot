#!/bin/sh
# Prepare the local model encryption key without replacing an existing one.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cache_dir="$project_dir/.cache"
key_path="$cache_dir/model-master.key"
umask 077
mkdir -p "$cache_dir"
chmod 700 "$cache_dir"
if [ -e "$key_path" ]; then
  if [ ! -f "$key_path" ] || [ ! -r "$key_path" ]; then
    printf '%s\n' 'Existing model key is not a readable regular file.' >&2
    exit 1
  fi
  printf '%s\n' 'Existing model encryption key preserved.'
  exit 0
fi
if ! command -v openssl >/dev/null 2>&1; then
  printf '%s\n' 'OpenSSL is required to create the model encryption key.' >&2
  exit 1
fi
# Compose mounts this file into containers running as UID 65532. The parent
# directory remains private to the owner on the host.
tmp_path=$(mktemp "$cache_dir/.model-master.XXXXXXXX")
trap 'rm -f "$tmp_path"' EXIT HUP INT TERM
openssl rand -hex 32 > "$tmp_path"
chmod 644 "$tmp_path"
# An atomic hard link prevents an existing key from being overwritten.
if ln "$tmp_path" "$key_path" 2>/dev/null; then
  printf '%s\n' 'Local model encryption key is ready.'
elif [ -f "$key_path" ]; then
  printf '%s\n' 'Existing model encryption key preserved.'
else
  printf '%s\n' 'Could not create the model encryption key.' >&2
  exit 1
fi
