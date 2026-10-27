package lastfm

import (
	"net/http"
	"net/url"
)

// TrackService searches tracks on the Last.fm API.
type TrackService struct {
	apiKey  string
	baseURL *url.URL
	client  *http.Client
}

// Track is one Last.fm track.search match.
type Track struct {
	Name   string `json:"name"`
	Artist string `json:"artist"`
	URL    string `json:"url"`
	MBID   string `json:"mbid"`
}

// searchResponse is the track.search body. Failed calls carry Error and
// Message instead of Results.
type searchResponse struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
	Results struct {
		TrackMatches struct {
			Track []Track `json:"track"`
		} `json:"trackmatches"`
	} `json:"results"`
}
