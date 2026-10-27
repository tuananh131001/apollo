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
	// YtDlpPath is the yt-dlp executable that plays Last.fm songs.
	YtDlpPath string
	// YtDlpMaxConcurrent caps how many Last.fm songs stream at once.
	YtDlpMaxConcurrent int
	LogLevel           slog.Level
}
