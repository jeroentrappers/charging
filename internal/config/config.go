// Package config loads runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	APIAddr     string

	// Reference vehicle for the comparable session prices.
	VehicleUsableKWh   float64
	VehicleConsumption float64 // kWh / 100 km

	// Availability older than this is treated as unknown (not "available").
	AvailabilityStaleAfter time.Duration
	// Price freshness window for the readiness check.
	PriceStaleAfter time.Duration
	// Address for the ingest process to expose Prometheus /metrics.
	MetricsAddr string
	// Comma-separated allowed CORS origins for the API ("*" = any).
	CORSOrigins string
	// Bearer token protecting the admin endpoints; empty disables them.
	AdminToken string

	// Public path prefix the API is reverse-proxied under (e.g. "/api"); sets
	// the OpenAPI server URL and the docs' spec link. Empty = mounted at root.
	APIBasePath string

	// Public origin+prefix the API is reachable at (e.g. https://host/api), used
	// to build absolute OCPI URLs in the handshake. Empty = derive from request.
	PublicURL string
	// Our OCPI eMSP identity for the credentials handshake.
	OCPICountry   string
	OCPIPartyID   string
	OCPIPartyName string

	// Bulk dataset export (open static dumps served at /export). Empty dir
	// disables it.
	ExportDir        string
	ExportFullEvery  time.Duration
	ExportAvailEvery time.Duration

	// OSRMURL is the base URL of a self-hosted osrm-routed server (e.g.
	// http://osrm:5000). Empty disables route/corridor search.
	OSRMURL string

	// MobilithekPushToken authenticates inbound Mobilithek webhook pushes
	// (the token is embedded in the registered callback URL). Empty disables
	// the push receiver.
	MobilithekPushToken string

	// MobilithekCaptureDir, if set, is where raw push payloads are written
	// (for building/validating the parser). Empty = log a snippet only.
	MobilithekCaptureDir string

	// MobilithekSpoolDir, if set, makes the webhook durably enqueue each push
	// there; worker(s) drain it into the DB (decoupled, restart-safe). Empty
	// falls back to inline ingest in a goroutine.
	MobilithekSpoolDir string
	// MobilithekWorkers is how many spool drainers run (default 1).
	MobilithekWorkers int
}

func Load() Config {
	return Config{
		DatabaseURL:            env("DATABASE_URL", "postgres://charging:charging@localhost:5433/charging?sslmode=disable"),
		APIAddr:                env("API_ADDR", ":8080"),
		VehicleUsableKWh:       envFloat("VEHICLE_USABLE_KWH", 60),
		VehicleConsumption:     envFloat("VEHICLE_CONSUMPTION_KWH100", 18),
		AvailabilityStaleAfter: envDuration("AVAILABILITY_STALE_AFTER", 15*time.Minute),
		PriceStaleAfter:        envDuration("PRICE_STALE_AFTER", 48*time.Hour),
		MetricsAddr:            env("METRICS_ADDR", ":9090"),
		CORSOrigins:            env("CORS_ORIGINS", "*"),
		AdminToken:             os.Getenv("ADMIN_TOKEN"),
		APIBasePath:            env("API_BASE_PATH", ""),
		PublicURL:              env("PUBLIC_URL", ""),
		OCPICountry:            env("OCPI_COUNTRY", "BE"),
		OCPIPartyID:            env("OCPI_PARTY_ID", "APM"),
		OCPIPartyName:          env("OCPI_PARTY_NAME", "Appmire Charging"),
		ExportDir:              env("EXPORT_DIR", "./export"),
		ExportFullEvery:        envDuration("EXPORT_FULL_EVERY", 5*time.Minute),
		ExportAvailEvery:       envDuration("EXPORT_AVAIL_EVERY", time.Minute),
		OSRMURL:                env("OSRM_URL", ""),
		MobilithekPushToken:    os.Getenv("MOBILITHEK_PUSH_TOKEN"),
		MobilithekCaptureDir:   env("MOBILITHEK_CAPTURE_DIR", ""),
		MobilithekSpoolDir:     env("MOBILITHEK_SPOOL_DIR", ""),
		MobilithekWorkers:      envInt("MOBILITHEK_WORKERS", 1),
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
