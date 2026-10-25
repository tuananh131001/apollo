package config

import (
	"net/url"
	"time"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
	NavidromeURL    *url.URL
}
