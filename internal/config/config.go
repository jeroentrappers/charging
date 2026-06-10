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
