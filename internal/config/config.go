// Package config loads runtime configuration from the environment.
package config

import "os"

type Config struct {
	DatabaseURL string
	APIAddr     string
}

func Load() Config {
	return Config{
		DatabaseURL: env("DATABASE_URL", "postgres://charging:charging@localhost:5433/charging?sslmode=disable"),
		APIAddr:     env("API_ADDR", ":8080"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
