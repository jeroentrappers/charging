// Package testdb gives each test binary its own freshly-migrated database, so
// `go test ./...` (which runs packages in parallel) can't have one package's
// TRUNCATE wipe another's rows. Each package calls RunMain(m, "<label>") from
// TestMain; tests then call DSN(t) for an isolated, migrated DSN (or a skip if
// no server is reachable).
package testdb

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"github.com/pressly/goose/v3"

	migrations "github.com/appmire/charging/db/migrations"
)

var (
	dsnVal  string
	provErr error
)

// base is the admin/connection DSN used to CREATE the per-package database.
// TEST_DATABASE_URL or DATABASE_URL override the local docker-compose default.
// The charging role's postgres superuser can CREATE DATABASE from any db.
func base() string {
	for _, k := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "postgres://charging:charging@localhost:5433/charging?sslmode=disable"
}

// RunMain provisions an isolated, migrated database named charging_test_<label>
// (dropped + recreated fresh), then runs the package's tests. Provisioning
// errors are deferred to DSN(t) as a skip, so a missing DB skips rather than
// hard-fails. Call from each DB-using package's TestMain.
func RunMain(m *testing.M, label string) int {
	dsnVal, provErr = provision(label)
	return m.Run()
}

// DSN returns this package's isolated DSN, or skips the test if the database is
// unavailable.
func DSN(t testing.TB) string {
	t.Helper()
	if provErr != nil {
		t.Skipf("no test database (%v); run `make db-up`", provErr)
	}
	return dsnVal
}

func provision(label string) (string, error) {
	name := "charging_test_" + label
	admin, err := sql.Open("pgx", base())
	if err != nil {
		return "", err
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		return "", err
	}
	// Fresh DB each run. FORCE drops any lingering connections (PG13+).
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
		return "", err
	}
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		return "", err
	}

	dbDSN, err := withDBName(base(), name)
	if err != nil {
		return "", err
	}
	mig, err := sql.Open("pgx", dbDSN)
	if err != nil {
		return "", err
	}
	defer mig.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return "", err
	}
	if err := goose.Up(mig, "."); err != nil {
		return "", fmt.Errorf("migrate %s: %w", name, err)
	}
	return dbDSN, nil
}

// withDBName swaps the database name in a postgres URL.
func withDBName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}
