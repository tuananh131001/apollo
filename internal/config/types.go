package config

import (
	"log/slog"
	"net/url"
	"time"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
	NavidromeURL    *url.URL
	// LastFMAPIKey authenticates "/s" searches against the Last.fm API.
	LastFMAPIKey string
	LogLevel     slog.Level
}
