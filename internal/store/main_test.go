package store

import (
	"os"
	"testing"

	"github.com/appmire/charging/internal/testdb"
)

// TestMain gives this package its own freshly-migrated database, so the
// DB-backed tests here (credential rotation) run against real roles and real
// grants rather than a mock. Without it testdb.DSN would hand back an empty
// string and the test would quietly connect nowhere.
func TestMain(m *testing.M) {
	os.Exit(testdb.RunMain(m, "store"))
}
