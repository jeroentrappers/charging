// Command migrate applies the embedded database migrations and exits. It runs
// as a one-shot step before api/ingest start (see docker-compose.prod.yml).
package main

import (
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"github.com/pressly/goose/v3"

	migrations "github.com/appmire/charging/db/migrations"
	"github.com/appmire/charging/internal/config"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := config.Load()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Error("dialect", "err", err)
		os.Exit(1)
	}
	if err := goose.Up(db, "."); err != nil {
		log.Error("migrate up", "err", err)
		os.Exit(1)
	}
	log.Info("migrations applied")
}
