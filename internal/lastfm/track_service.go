// Package lastfm queries the Last.fm web API.
package lastfm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is the Last.fm API root.
var DefaultBaseURL = &url.URL{Scheme: "https", Host: "ws.audioscrobbler.com", Path: "/2.0/"}

// requestTimeout bounds a Last.fm call so a slow API cannot stall a search.
const requestTimeout = 10 * time.Second

// NewTrackService returns a TrackService that calls baseURL with apiKey using
// transport, or http.DefaultTransport when transport is nil.
func NewTrackService(apiKey string, baseURL *url.URL, transport http.RoundTripper) *TrackService {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &TrackService{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Transport: transport, Timeout: requestTimeout},
	}
}

// SearchTrack returns up to limit of Last.fm's matches for track, narrowed to
// artist when it is not empty. A limit of 0 uses Last.fm's default of 30.
// Failures are logged and reported as no matches.
func (s *TrackService) SearchTrack(ctx context.Context, track, artist string, limit int) []Track {
	tracks, err := s.searchTrack(ctx, track, artist, limit)
	if err != nil {
		slog.Error("lastfm track search failed", "track", track, "artist", artist, "err", err)
		return nil
	}
	return tracks
}

func (s *TrackService) searchTrack(ctx context.Context, track, artist string, limit int) ([]Track, error) {
	q := url.Values{
		"method":  {"track.search"},
		"track":   {track},
		"api_key": {s.apiKey},
		"format":  {"json"},
	}
	if artist != "" {
		q.Set("artist", artist)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	u := *s.baseURL
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	q.Set("api_key", "REDACTED")
	slog.DebugContext(ctx, "lastfm request", "method", "track.search", "query", q.Encode())
	resp, err := s.client.Do(req)
	if err != nil {
		// The *url.Error wrapper quotes the request URL, which holds the API key.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			return nil, ue.Err
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	slog.DebugContext(ctx, "lastfm response", "status", resp.StatusCode,
		"error", body.Error, "tracks", len(body.Results.TrackMatches.Track))
	if body.Error != 0 {
		return nil, fmt.Errorf("last.fm error %d: %s", body.Error, body.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return body.Results.TrackMatches.Track, nil
}
