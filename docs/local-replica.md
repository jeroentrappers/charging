# Local replica of appmire-hetz1

Run the full stack locally (docker compose) with a **copy of production data** to
test, debug, and experiment without touching prod.

## TL;DR

```
make local-replica     # pull prod DB → restore locally (sanitized) → start the stack
```

Then the API is on `:8080` and the PWA on `:5173` (see `docker-compose.prod.yml`).
Do it in steps if you prefer:

```
make prod-db-dump      # 1. pull live appmire-hetz1 DB → backups/prod-<ts>.dump (+ prod-latest.dump)
make local-restore     # 2. restore that dump into the local stack DB (sanitized)
make prod-up           # 3. build + start db + migrate + api + ingest + web
```

## What each step does

- **`make prod-db-dump`** (`scripts/pull-prod-db.sh`) streams `pg_dump -Fc` from
  the production `charging-db-1` container over SSH straight into
  `backups/prod-<ts>.dump` — nothing is written on the box. Override the
  connection with `PROD_HOST` / `PROD_USER` / `PROD_SSH_KEY` / `PROD_SSH_PORT`
  if your access differs (defaults match `deploy/ansible/inventory.ini`). The
  `backups/` dir is git-ignored — dumps hold real data, never commit them.

- **`make local-restore`** (`scripts/restore-local-db.sh`) restores a dump into
  the local prod-compose DB (`--clean`, idempotent), waits for the DB to truly
  accept queries first, verifies by row count, and retries across a fresh
  container's init window (initdb/PostGIS setup briefly restarts postgres).
  **Then it sanitizes:** `NULL cpo.token` + `enabled = false` on every
  source, so the local ingest can never poll real operator APIs with production
  credentials. Pass `--keep-secrets` to skip (don't, unless you know why).
  `FILE=backups/prod-YYYYMMDD-HHMMSS.dump` restores a specific dump.

## Notes / gotchas

- **Apple Silicon (arm64):** the official `postgis/postgis:17-3.5` image is
  **amd64-only** (no arm64 manifest), so it runs under slow QEMU emulation and a
  fresh container drops connections during the first heavy restore. Point the
  local DB at a multi-arch PostGIS 17/3.5 image instead — set `DB_IMAGE` (the
  `db` service reads `${DB_IMAGE:-postgis/postgis:17-3.5}`, so prod is
  unaffected). This repo's `.env` already sets:

  ```
  DB_IMAGE=imresamu/postgis:17-3.5   # multi-arch; runs native aarch64
  ```

  After changing the image, recreate the DB on a fresh volume so it's rebuilt
  natively: `docker compose -p charging_prod -f docker-compose.prod.yml down -v`
  then `make local-restore`. (The restore script retries across the fresh
  container's initdb/PostGIS-setup window, which briefly restarts postgres.)
- **Sources are disabled after restore.** To exercise live ingest locally,
  re-enable a source and set its token by hand (`chargingctl` or SQL) — but
  point it at a test credential, not prod's.
- **Target another DB:** the restore defaults to the prod-compose project
  (`charging_prod`). Override with `COMPOSE_PROJECT` / `COMPOSE_FILE`
  (e.g. to load into the lightweight dev DB from `docker-compose.yml`).
- **Refresh:** re-run `make prod-db-dump && make local-restore` any time to pull
  a fresh production snapshot.
