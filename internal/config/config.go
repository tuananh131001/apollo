// Package config loads application configuration from the environment.
package config

import (
	"errors"
	"net/url"
	"os"
	"time"
)

// Load reads configuration from environment variables, falling back to defaults.
func Load() (Config, error) {
	cfg := Config{
		Addr:            getenv("APOLLO_ADDR", ":8080"),
		ShutdownTimeout: 10 * time.Second,
		LastFMAPIKey:    os.Getenv("LASTFM_API_KEY"),
	}
	if v := os.Getenv("APOLLO_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, err
		}
		cfg.ShutdownTimeout = d
	}
	// Accepts debug, info, warn or error; defaults to info.
	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("APOLLO_LOG_LEVEL", "info"))); err != nil {
		return Config{}, err
	}
	u, err := loadNavidromeURL()
	if err != nil {
		return Config{}, err
	}
	cfg.NavidromeURL = u
	return cfg, nil
}

// loadNavidromeURL reads the required APOLLO_NAVIDROME_URL, the Navidrome
// server that /rest/* traffic is proxied to.
func loadNavidromeURL() (*url.URL, error) {
	u, err := url.Parse(os.Getenv("APOLLO_NAVIDROME_URL"))
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("APOLLO_NAVIDROME_URL must be an absolute http(s) URL, e.g. http://localhost:4533")
	}
	return u, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
