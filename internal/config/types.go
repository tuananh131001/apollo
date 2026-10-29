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
	// StreamWriteTimeout ends a Last.fm song stream, freeing its yt-dlp, when
	// the client accepts no audio for this long.
	StreamWriteTimeout time.Duration
	LogLevel           slog.Level
}
