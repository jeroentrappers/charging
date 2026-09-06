package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/appmire/charging/internal/testdb"
)

func writeCreds(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Coarse filesystem timestamps would hide a same-second rewrite; the size
	// check covers that in practice, but tests should not depend on luck.
	old := time.Now().Add(-2 * time.Second)
	_ = os.Chtimes(path, old, old)
}

func TestCredentials_ParsesAndIgnoresNoise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db-credentials")
	writeCreds(t, path, "# managed by ansible\n\nDB_USER = charging_a \nDB_PASSWORD=\"s3cr#t=with=equals\"\nOTHER=ignored\n")

	c, err := newCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	cc := &pgx.ConnConfig{}
	c.apply(cc)
	if cc.User != "charging_a" {
		t.Errorf("user = %q, want charging_a", cc.User)
	}
	if cc.Password != "s3cr#t=with=equals" {
		t.Errorf("password = %q (a value containing '=' must survive)", cc.Password)
	}
}

func TestCredentials_RejectsAnUnusableFileAtStartup(t *testing.T) {
	dir := t.TempDir()
	if _, err := newCredentials(filepath.Join(dir, "absent")); err == nil {
		t.Error("a missing credentials file must fail the process at startup, not at the first reconnect")
	}
	empty := filepath.Join(dir, "empty")
	writeCreds(t, empty, "# nothing useful here\n")
	if _, err := newCredentials(empty); err == nil {
		t.Error("a file with neither key must be rejected")
	}
}

func TestCredentials_PicksUpAChangeAndSurvivesABadRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db-credentials")
	writeCreds(t, path, "DB_USER=charging_a\nDB_PASSWORD=first\n")
	c, err := newCredentials(path)
	if err != nil {
		t.Fatal(err)
	}

	writeCreds(t, path, "DB_USER=charging_b\nDB_PASSWORD=second\n")
	cc := &pgx.ConnConfig{}
	c.apply(cc)
	if cc.User != "charging_b" || cc.Password != "second" {
		t.Fatalf("after rewrite: %s/%s, want charging_b/second", cc.User, cc.Password)
	}

	// A mounted secret can vanish for a moment while it is swapped. The last
	// known-good credential must keep being handed out rather than an empty one.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	cc = &pgx.ConnConfig{}
	c.apply(cc)
	if cc.User != "charging_b" || cc.Password != "second" {
		t.Errorf("after the file vanished: %s/%s, want the last known-good charging_b/second", cc.User, cc.Password)
	}
}

// The point of the whole exercise: a pool that is already serving picks up a
// rotated credential on its next connection, with no restart, and the
// connections it opened before keep working.
func TestCredentials_RotatesALivePoolWithoutRestart(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.DSN(t) // owner DSN; the hook overrides user+password per connection

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database available (%v)", err)
	}
	defer admin.Close()
	for _, role := range []string{"charging_a", "charging_b"} {
		if _, err := admin.Exec(ctx, "ALTER ROLE "+role+" PASSWORD 'rotation_test_"+role+"'"); err != nil {
			t.Skipf("app roles not present (%v); run migrations first", err)
		}
	}

	path := filepath.Join(t.TempDir(), "db-credentials")
	writeCreds(t, path, "DB_USER=charging_a\nDB_PASSWORD=rotation_test_charging_a\n")

	st, err := NewWithCredentials(ctx, dsn, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Hold a connection open across the rotation, as a busy pool would.
	held, err := st.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	var who string
	if err := held.QueryRow(ctx, "SELECT current_user").Scan(&who); err != nil {
		t.Fatal(err)
	}
	if who != "charging_a" {
		t.Fatalf("connected as %q, want charging_a", who)
	}

	// Rotate: the file now names the other role, whose password already works.
	writeCreds(t, path, "DB_USER=charging_b\nDB_PASSWORD=rotation_test_charging_b\n")

	conn, err := st.Pool.Acquire(ctx) // pool is empty of idle conns -> opens a new one
	if err != nil {
		t.Fatalf("new connection after rotation: %v", err)
	}
	defer conn.Release()
	if err := conn.QueryRow(ctx, "SELECT current_user").Scan(&who); err != nil {
		t.Fatal(err)
	}
	if who != "charging_b" {
		t.Errorf("new connection came up as %q, want charging_b — the rotation was not picked up", who)
	}

	// And the connection opened before the rotation is still usable.
	if err := held.QueryRow(ctx, "SELECT current_user").Scan(&who); err != nil {
		t.Errorf("connection opened before the rotation broke: %v", err)
	} else if who != "charging_a" {
		t.Errorf("held connection is now %q, want the original charging_a", who)
	}
}
