// Package config loads application configuration from the environment.
package config

import (
	"os"
	"time"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() (Config, error) {
	cfg := Config{
		Addr:            getenv("APOLLO_ADDR", ":8080"),
		ShutdownTimeout: 10 * time.Second,
	}
	if v := os.Getenv("APOLLO_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, err
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
