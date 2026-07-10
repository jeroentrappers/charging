#!/usr/bin/env bash
# Dump the LIVE production database (appmire-hetz1) to backups/ for local use.
# Streams pg_dump over SSH straight into a local custom-format dump — nothing is
# written on the box. Override the connection with PROD_HOST / PROD_USER /
# PROD_SSH_KEY / PROD_SSH_PORT / DB_CONTAINER if your access differs.
#
#   scripts/pull-prod-db.sh            # -> backups/prod-<ts>.dump (+ prod-latest.dump)
set -euo pipefail
cd "$(dirname "$0")/.."

HOST=${PROD_HOST:-136.243.103.58}
USER=${PROD_USER:-ansible}
PORT=${PROD_SSH_PORT:-22}
KEY=${PROD_SSH_KEY:-$HOME/.ssh/id_ed25519_oldbox}
DB_CONTAINER=${DB_CONTAINER:-charging-db-1}

mkdir -p backups
TS=$(date +%Y%m%d-%H%M%S)
OUT="backups/prod-${TS}.dump"

key_arg=()
[ -f "$KEY" ] && key_arg=(-i "$KEY")

echo "dumping production DB from ${USER}@${HOST} (container ${DB_CONTAINER})..."
# -Fc: compressed custom format (parallel/selective restore). --no-owner/-privileges
# so it restores cleanly into the local 'charging' role.
ssh "${key_arg[@]}" -p "$PORT" -o StrictHostKeyChecking=no "${USER}@${HOST}" \
  "docker exec -i ${DB_CONTAINER} pg_dump -U charging -d charging -Fc --no-owner --no-privileges" > "$OUT"

if [ ! -s "$OUT" ]; then
  echo "error: dump is empty — check SSH access and DB_CONTAINER" >&2
  rm -f "$OUT"
  exit 1
fi
ln -sf "$(basename "$OUT")" backups/prod-latest.dump
echo "wrote $OUT ($(du -h "$OUT" | cut -f1)); symlinked backups/prod-latest.dump"
