package deezer

import (
	"net/http"
	"net/url"
)

// CoverArtService finds songs' album covers on the Deezer API and fetches them
// from Deezer's image CDN.
type CoverArtService struct {
	apiURL   *url.URL
	imageURL *url.URL
	client   *http.Client
	cache    *coverCache
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
	Title  string `json:"title"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		MD5Image string `json:"md5_image"`
	} `json:"album"`
}
