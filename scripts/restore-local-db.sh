#!/usr/bin/env bash
# Restore a production dump (from pull-prod-db.sh) into the LOCAL stack DB so you
# can experiment against real data. Targets the local prod-compose project by
# default; override with COMPOSE_PROJECT / COMPOSE_FILE.
#
#   scripts/restore-local-db.sh [backups/prod-latest.dump]
#
# SAFETY: after restoring, it sanitizes the copy — NULLs cpo.token and disables
# every source — so the local ingest can never poll real operator APIs with
# production credentials. Pass --keep-secrets to skip that (don't, unless you
# know why).
set -euo pipefail
cd "$(dirname "$0")/.."

FILE="backups/prod-latest.dump"
SANITIZE=1
for a in "$@"; do
  case "$a" in
    --keep-secrets) SANITIZE=0 ;;
    *) FILE="$a" ;;
  esac
done
[ -f "$FILE" ] || { echo "dump not found: $FILE (run: make prod-db-dump)" >&2; exit 1; }

PROJECT=${COMPOSE_PROJECT:-charging_prod}
COMPOSE=${COMPOSE_FILE:-docker-compose.prod.yml}
dc() { docker compose -p "$PROJECT" -f "$COMPOSE" "$@"; }

echo "starting local db (project=$PROJECT)..."
dc up -d db
# Wait until postgres actually answers a query, not just opens the socket — a
# fresh (esp. emulated amd64-on-arm64) db accepts connections a moment before
# it's ready for a heavy restore.
echo "waiting for postgres to accept queries..."
for _ in $(seq 1 90); do
  dc exec -T db psql -U charging -d charging -tAc 'SELECT 1' >/dev/null 2>&1 && break
  sleep 1
done

# pg_restore returns non-zero on benign --clean warnings, so success is judged by
# the data actually landing (charger rows), with one retry for a transient blip.
restore_once() { dc exec -T db pg_restore -U charging -d charging --clean --if-exists --no-owner --no-privileges < "$FILE" >/tmp/pgrestore.log 2>&1 || true; }
# Must not fail under `set -e`/pipefail: during the init window psql exits
# non-zero, and `n=$(count)` would otherwise abort the whole script. Swallow it —
# an empty result just means "not ready yet", which the retry loop handles.
count() { dc exec -T db psql -U charging -d charging -tAc 'SELECT count(*) FROM charger' 2>/dev/null | tr -d '[:space:]' || true; }

echo "restoring $FILE ..."
# A freshly-created container runs initdb + PostGIS setup and briefly restarts
# postgres; a restore landing in that window fails silently. Retry (verifying by
# rowcount) until it lands or we give up.
n=""
for attempt in 1 2 3 4 5 6; do
  restore_once
  n=$(count)
  if [ -n "$n" ] && [ "$n" != "0" ]; then
    break
  fi
  echo "  attempt $attempt didn't land (charger=$n); db may still be initialising, waiting 5s..."
  sleep 5
done
if [ -z "$n" ] || [ "$n" = "0" ]; then
  echo "restore failed after retries:" >&2
  tail -20 /tmp/pgrestore.log >&2
  exit 1
fi

if [ "$SANITIZE" = 1 ]; then
  echo "sanitizing: NULL cpo.token + disable all sources (no real-API polling locally)"
  dc exec -T db psql -U charging -d charging -q -c \
    "UPDATE cpo SET token = NULL, enabled = false;"
fi

echo "done. Chargers: $(count)"
echo "bring the full stack up with: make prod-up   (or: make local-replica)"
