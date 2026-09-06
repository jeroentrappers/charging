# Database credentials & live password rotation

Postgres gives a role exactly one password — there is no MySQL-style "retain
current password", so you cannot hand out two valid passwords for one role and
retire the old one later. Rotating a password in place therefore breaks every
*new* connection the moment it lands, and (with pgx's one-hour
`MaxConnLifetime`) the pool then degrades as its existing connections age out.

So the credential the app uses is never rotated in place. There are two of them,
and rotation is a switchover.

## The roles

| role | login | used by | privileges |
|---|---|---|---|
| `charging` | ✅ | the one-shot **migrate** container, `pg_dump`, manual `psql` | superuser; owns every object |
| `charging_app` | ❌ | — | group role: `SELECT/INSERT/UPDATE/DELETE` on the app tables, sequence usage |
| `charging_a` | ✅ | **api** + **ingest**, when `DB_USER=charging_a` | inherits `charging_app` |
| `charging_b` | ✅ | **api** + **ingest**, when `DB_USER=charging_b` | inherits `charging_app` |

Created by `db/migrations/00025_app_login_roles.sql`, without passwords — those
live in the vault, not in a migration.

Two properties fall out of the split:

- **The runtime credential cannot do DDL.** Every schema change in this project
  goes through a migration, which runs as the owner, so the app roles need no
  ownership. A leaked runtime password cannot drop a table, and future tables are
  covered automatically by the `ALTER DEFAULT PRIVILEGES` in that migration.
- **The owner's password can be rotated whenever you like.** Only the migrate
  container uses it, and that container runs once per deploy and exits.

## Rotating the runtime password (no failed queries)

Two ordinary deploys. Say `charging_a` is active.

**1. Give the idle role a new password.** In
`deploy/ansible/group_vars/charging/vault.yml`, under `charging_env`, set
`DB_PASSWORD_CHARGING_B` to a fresh value (`openssl rand -hex 24`), leaving
`DB_USER: charging_a`. Then:

```bash
cd deploy/ansible && ansible-playbook site.yml --tags app
```

Nothing that is running is affected: the deploy re-passwords a role nobody is
connected as.

**2. Switch over.** Set `DB_USER: charging_b` in the same file and deploy again.
The app comes up on a credential that has been valid since step 1, so there is
no window in which it holds a password the database has already rejected.

Next time, rotate `charging_a` and switch back. The ansible task sets both roles'
passwords on every deploy, so the active one is simply re-set to the value it
already has — a no-op that existing sessions never notice.

**What this does not remove:** the deploy itself. There is one `api` container
and nginx proxies to it with a single `proxy_pass`, so recreating it drops
requests for a few seconds — the same blip every deploy has. Making rotation
fully invisible means fixing that first (a second api replica behind an nginx
`upstream` with `proxy_next_upstream error timeout http_502`, or blue/green).
The two-role split is what makes that worth doing: with it, rolling deploys give
zero-downtime rotation for free.

## Rotating the owner's password

`POSTGRES_PASSWORD` is only read at initdb, so editing compose does nothing to an
existing volume. Set `DB_ADMIN_PASSWORD` in the vault, deploy, and change it in
the cluster:

```bash
ssh <box> "docker exec charging-db-1 psql -U charging -d charging \
  -c \"ALTER ROLE charging PASSWORD '<new>'\""
```

Order does not matter much here — the only consumer is the next migrate run.

## Local development

Unchanged. Every default is still `charging` / `charging`, so `make db-up`,
`make prod-up`, the tests (`internal/testdb` creates per-package databases, which
needs the owner) and `scripts/restore-local-db.sh` work with no configuration.
`DB_USER`, `DB_PASSWORD_*` and `DB_ADMIN_PASSWORD` are prod-only vault values.

## First install on a fresh box

The roles do not exist until the first migration runs, and their passwords are
set after the stack starts. If the vault already names `DB_USER`, api and ingest
will fail to connect on that very first deploy and restart-loop for a few seconds
until the password task lands, then recover on their own. To avoid even that,
leave `DB_USER` unset for the first deploy and add it on the second.
