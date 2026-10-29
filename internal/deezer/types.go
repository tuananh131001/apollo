package deezer

import (
	"net/http"
	"net/url"
	"time"

	"github.com/tuananh131001/apollo/internal/cache"
)

// CoverArtService finds songs' album covers on the Deezer API and fetches them
// from Deezer's image CDN.
type CoverArtService struct {
	apiURL   *url.URL
	imageURL *url.URL
	client   *http.Client
	cache    *cache.LRU[Track]
}

// Track is what Deezer knows of a song: the id of its album cover, "" when it
// has none, and its length, zero when unknown.
type Track struct {
	Cover    string
	Duration time.Duration
}

// searchResponse is the /search body. Failed calls, such as those over
// Deezer's rate limit, carry Error instead of Data.
type searchResponse struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
	Data []searchTrack `json:"data"`
}

type searchTrack struct {
	Title string `json:"title"`
	// Duration is in seconds.
	Duration int `json:"duration"`
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		MD5Image string `json:"md5_image"`
	} `json:"album"`
}
