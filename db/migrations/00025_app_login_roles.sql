-- +goose Up
-- +goose StatementBegin
-- Separate the runtime credential from the owning one, so a password can be
-- rotated without ever touching the credential that is serving traffic.
--
-- Postgres allows a role exactly one password (there is no MySQL-style
-- "retain current password"), so a live rotation needs two login roles and a
-- switchover instead:
--
--   charging       the bootstrap superuser. Owns every object, runs the
--                  migrations, and is what pg_dump/psql use. Only the one-shot
--                  migrate container connects with it, so re-passwording it
--                  never interrupts a running api or ingest.
--   charging_app   a NOLOGIN group holding the runtime privileges (DML only —
--                  every DDL in this project lives in a migration).
--   charging_a     two interchangeable login roles in that group. api and
--   charging_b     ingest use one; the other is idle, and its password can be
--                  changed at any moment with zero effect on live connections.
--                  Switching DATABASE_URL to the freshly-passworded role then
--                  costs one ordinary deploy, no failed queries.
--
-- The login roles are created WITHOUT a password on purpose: secrets belong in
-- the vault, not in a migration. Until one is set they cannot connect, which is
-- the safe default. The deploy sets them from vault values (see
-- deploy/ansible/roles/charging/tasks/main.yml).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'charging_app') THEN
        CREATE ROLE charging_app NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'charging_a') THEN
        CREATE ROLE charging_a LOGIN IN ROLE charging_app;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'charging_b') THEN
        CREATE ROLE charging_b LOGIN IN ROLE charging_app;
    END IF;
END
$$;

-- Database and owner are named dynamically: the test harness applies these same
-- migrations to a per-package database (charging_test_<pkg>), and a hard-coded
-- name would grant on the wrong one there — or fail outright on a cluster whose
-- database is named something else.
DO $$
BEGIN
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO charging_app', current_database());
END
$$;
GRANT USAGE ON SCHEMA public TO charging_app;

-- Existing objects. DML only: the app never issues DDL, so withholding it means
-- a leaked runtime credential cannot drop a table or read another database.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO charging_app;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO charging_app;

-- Objects a future migration creates. Migrations run as the owner, so the
-- default privileges are declared FOR that role; without this every new table
-- would be invisible to the app until someone remembered to grant it.
DO $$
BEGIN
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
                   'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO charging_app', current_user);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
                   'GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO charging_app', current_user);
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
                   'REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM charging_app', current_user);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
                   'REVOKE USAGE, SELECT, UPDATE ON SEQUENCES FROM charging_app', current_user);
    EXECUTE format('REVOKE CONNECT ON DATABASE %I FROM charging_app', current_database());
END
$$;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM charging_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM charging_app;
REVOKE ALL ON SCHEMA public FROM charging_app;
DROP ROLE IF EXISTS charging_b;
DROP ROLE IF EXISTS charging_a;
DROP ROLE IF EXISTS charging_app;
-- +goose StatementEnd
