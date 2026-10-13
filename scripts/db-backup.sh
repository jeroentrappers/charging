#!/usr/bin/env bash
# Logical backup of the charging Postgres: pg_dump -> gzip, with local rotation
# and optional offsite copy. Installed behind a daily systemd timer
# (deploy/systemd/charging-db-backup.{service,timer}). Run on demand:
#   sudo systemctl start charging-db-backup.service
#
# Env:
#   BACKUP_DIR   where dumps land (default /var/backups/charging)
#   KEEP         how many dumps to retain (default 14)
#   DB_CONTAINER compose db container name (default charging-db-1)
#   RCLONE_REMOTE if set, `rclone copy` each dump there for offsite durability
#                 (e.g. "b2:charging-backups"). Configure rclone separately.
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/charging}"
KEEP="${KEEP:-14}"
DB_CONTAINER="${DB_CONTAINER:-charging-db-1}"

mkdir -p "$BACKUP_DIR"
ts="$(date -u +%Y%m%dT%H%M%SZ)"
out="$BACKUP_DIR/charging-$ts.sql.gz"

# No -t: a TTY would translate newlines and corrupt the dump stream.
docker exec "$DB_CONTAINER" pg_dump -U charging -d charging | gzip -9 > "$out"

# Fail loudly on an empty/truncated dump rather than silently rotating good ones away.
if [ ! -s "$out" ] || [ "$(gzip -t "$out" 2>&1 || echo bad)" = "bad" ]; then
  echo "ERROR: backup $out is empty or not a valid gzip" >&2
  rm -f "$out"
  exit 1
fi

# Rotate: keep the newest $KEEP dumps.
ls -1t "$BACKUP_DIR"/charging-*.sql.gz 2>/dev/null | tail -n +"$((KEEP + 1))" | xargs -r rm -f

echo "backup ok: $out ($(du -h "$out" | cut -f1))"

if [ -n "${RCLONE_REMOTE:-}" ]; then
  rclone copy "$out" "$RCLONE_REMOTE" && echo "offsite copy -> $RCLONE_REMOTE"
fi
