package store

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Live database credentials.
//
// A process cannot be handed new environment variables, so a credential baked
// into DATABASE_URL at startup can only change by restarting. Reading it from a
// file instead lets a rotation land without one: pgx calls BeforeConnect for
// every new physical connection, so each connection picks up whatever the file
// says at that moment while the connections already open keep serving.
//
// That is what makes rotation free of failed queries when paired with the two
// login roles (see docs/db-credentials.md): the idle role is given a password,
// the file is pointed at it, and new connections use a credential the database
// has already accepted. It is also the seam a managed database's IAM auth or
// Vault's per-lease roles plug into, where the token expires every few minutes
// and there is no alternative to fetching it per connection.
//
// File format is the same key=value shape as the vault entries it comes from:
//
//	DB_USER=charging_b
//	DB_PASSWORD=…
//
// Blank lines and # comments are ignored, and values may be quoted.
type credentials struct {
	path string

	mu       sync.Mutex
	user     string
	password string
	mod      time.Time // modification time of the last successful read
	size     int64
}

// newCredentials reads the file once, so a missing or malformed one fails the
// process at startup rather than at the first reconnect hours later.
func newCredentials(path string) (*credentials, error) {
	c := &credentials{path: path}
	if err := c.reload(); err != nil {
		return nil, err
	}
	return c, nil
}

// apply sets the connection's user and password, re-reading the file when it
// has changed. Read failures in flight are deliberately NOT fatal: the file is
// a mounted secret that can briefly vanish or be swapped atomically underneath
// us (Kubernetes replaces the whole directory), and the last known-good
// credential is a far better answer than refusing to open connections.
func (c *credentials) apply(cc *pgx.ConnConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed() {
		if err := c.readLocked(); err != nil {
			return // keep serving with what we had
		}
	}
	if c.user != "" {
		cc.User = c.user
	}
	if c.password != "" {
		cc.Password = c.password
	}
}

// changed reports whether the file looks different from the one last read. Stat
// follows the path rather than a held descriptor, so an atomic swap of the file
// (or, on Kubernetes, of the directory the path resolves through) is seen.
func (c *credentials) changed() bool {
	fi, err := os.Stat(c.path)
	if err != nil {
		return false // unreadable: keep what we have, don't thrash
	}
	return !fi.ModTime().Equal(c.mod) || fi.Size() != c.size
}

func (c *credentials) reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readLocked()
}

func (c *credentials) readLocked() error {
	f, err := os.Open(c.path)
	if err != nil {
		return fmt.Errorf("db credentials: %w", err)
	}
	defer f.Close()

	var user, password string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "DB_USER":
			user = v
		case "DB_PASSWORD":
			password = v
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("db credentials %s: %w", c.path, err)
	}
	if user == "" && password == "" {
		return fmt.Errorf("db credentials %s: neither DB_USER nor DB_PASSWORD found", c.path)
	}

	if fi, err := os.Stat(c.path); err == nil {
		c.mod, c.size = fi.ModTime(), fi.Size()
	}
	c.user, c.password = user, password
	return nil
}
