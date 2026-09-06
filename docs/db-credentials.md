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

## How the app reads its credential

`api` and `ingest` do not take the database user and password from
`DATABASE_URL`. They read `/secrets/db-credentials` — a mounted file Ansible
writes from the vault — on **every new database connection**, through pgx's
`BeforeConnect` hook (`internal/store/credentials.go`):

```
DB_USER=charging_a
DB_PASSWORD=…
```

A process cannot be handed new environment variables, so a credential baked into
the DSN at startup can only change by restarting. Reading it from a file means a
rotation lands on the next connection while the connections already open keep
serving. Set `DB_CREDENTIALS_FILE` to enable it; unset (local runs, the one-shot
migrator) the DSN's own credentials are used, exactly as before.

Failure behaviour is deliberately asymmetric: a missing or unparseable file
**fails the process at startup**, because that means a broken mount and it should
be loud. The same failure **in flight is ignored** and the last known-good
credential keeps being used — a mounted secret can briefly vanish while it is
swapped (Kubernetes replaces the whole directory atomically), and refusing to
open connections over that would be worse than being a few seconds stale.

## Rotating the runtime password — without restarting anything

Say `charging_a` is active.

**1. Give the idle role a new password.** Set `DB_PASSWORD_CHARGING_B` in the
vault and deploy (`--tags app`). Nothing live is touched: the deploy re-passwords
a role nobody is connected as.

**2. Switch over, no restart.** Set `DB_USER: charging_b` in the vault, then:

```bash
cd deploy/ansible && ansible-playbook site.yml --tags dbcreds
```

That rewrites `/secrets/db-credentials` and stops there — no image build, no
container recreation, no dropped requests. New connections come up as
`charging_b` immediately; the ones already open finish their lives as
`charging_a` (up to pgx's one-hour `MaxConnLifetime`). To cut that tail short,
`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename =
'charging_a'` — the pool reconnects on the new credential.

Because `ingest` keeps a warm in-memory signature cache, avoiding a restart here
is worth real work: a cold pass rewrites every row (measured: 69,408 rows in
21 minutes, against 3–4 minutes warm).

Next time, rotate `charging_a` and switch back. Both roles' passwords are set
from the vault on every deploy, so the active one is simply re-set to the value
it already has — a no-op that existing sessions never notice.

### Or as part of a deploy

If you would rather fold rotation into a normal deploy, both steps are just
`--tags app` runs. The credentials file is written before the stack starts, so
the app never comes up against a password the database has not accepted yet.

Both passwords live in `deploy/ansible/group_vars/charging/vault.yml` under
`charging_env`, as `DB_PASSWORD_CHARGING_A` / `DB_PASSWORD_CHARGING_B`
(`openssl rand -hex 24`).

**What this still does not cover:** deploys themselves. There is one `api`
container behind a single nginx `proxy_pass`, so recreating it drops requests for
a few seconds. Rotation no longer needs a deploy, so that blip no longer applies
to it — but any code change still pays it. Fixing that is separate work (a second
api replica behind an nginx `upstream` with `proxy_next_upstream error timeout
http_502`, or blue/green), and it is also what `api` needs before it can be
scaled at all, since it currently hosts the export snapshotter and the Mobilithek
spool drainers.

**On Kubernetes** this same file is a Secret mounted as a volume: the kubelet
updates it in place, so rotation stays a `kubectl apply` with no rollout. (Secrets
injected as environment variables do *not* update, and `subPath` mounts never
refresh — mount the directory.) It is also the seam a managed database's IAM auth
or Vault's per-lease roles plug into, where the token expires every few minutes
and fetching it per connection is the only option.

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
set after the stack starts — but the credentials file is written before it, and
the app fails fast on a credential that does not work yet. So on a brand-new box
leave `DB_USER` out of the vault for the first deploy (everything falls back to
the owner, exactly as it did before this change) and add it on the second.
