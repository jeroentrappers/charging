// Package config loads runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string
	APIAddr     string

	// Reference vehicle for the comparable session prices.
	VehicleUsableKWh   float64
	VehicleConsumption float64 // kWh / 100 km
}

func Load() Config {
	return Config{
		DatabaseURL:        env("DATABASE_URL", "postgres://charging:charging@localhost:5433/charging?sslmode=disable"),
		APIAddr:            env("API_ADDR", ":8080"),
		VehicleUsableKWh:   envFloat("VEHICLE_USABLE_KWH", 60),
		VehicleConsumption: envFloat("VEHICLE_CONSUMPTION_KWH100", 18),
	}
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
