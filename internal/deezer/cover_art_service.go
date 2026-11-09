// Package deezer finds album covers on Deezer.
package deezer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/tuananh131001/apollo/internal/cache"
)

// DefaultAPIURL is the Deezer API root.
var DefaultAPIURL = &url.URL{Scheme: "https", Host: "api.deezer.com", Path: "/"}

// DefaultImageURL is the root of Deezer's album cover CDN.
var DefaultImageURL = &url.URL{Scheme: "https", Host: "cdn-images.dzcdn.net", Path: "/images/cover/"}

// requestTimeout bounds a Deezer search, and the wait for the CDN to start
// sending a cover, so a slow Deezer cannot stall a search or a client.
const requestTimeout = 5 * time.Second

// searchLimit is how many Deezer matches are checked for the right artist.
const searchLimit = 10

// How long FindTrack answers are cached, and how many. A song with no cover
// is checked again sooner, in case Deezer adds it.
const (
	coverCacheTTL   = 24 * time.Hour
	noCoverCacheTTL = time.Hour
	coverCacheSize  = 4096
)

// Cover sizes in pixels: the size sent when the client asks for none, and the
// bounds a requested size is clamped to.
const (
	defaultCoverSize = 1000
	minCoverSize     = 32
	maxCoverSize     = 1000
)

// ErrNotFound is returned by Fetch when Deezer has no cover for the id.
var ErrNotFound = errors.New("deezer cover not found")

// NewCoverArtService returns a CoverArtService that searches apiURL and fetches
// covers from imageURL using transport, or http.DefaultTransport when transport
// is nil.
func NewCoverArtService(apiURL, imageURL *url.URL, transport http.RoundTripper) *CoverArtService {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &CoverArtService{
		apiURL:   apiURL,
		imageURL: imageURL,
		client:   &http.Client{Transport: transport},
		cache:    cache.NewLRU[Track](coverCacheSize),
	}
}

// ValidCoverID reports whether id has the form of a Deezer cover id, 32
// lowercase hex digits, so it is safe to put in a CDN path.
func ValidCoverID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// FindTrack returns the album cover of artist's song title, with the song's
// length when Deezer has the song itself, or no cover when Deezer has no song
// by that artist. A song by the same artist with the same title is preferred
// over the first song by the artist, whose cover stands in. Answers are
// cached; failures are not, and are logged and reported as no cover.
func (s *CoverArtService) FindTrack(ctx context.Context, artist, title string) Track {
	wantArtist, wantTitle := fold(artist), fold(title)
	key := wantArtist + "\x00" + wantTitle
	if track, ok := s.cache.Get(key); ok {
		return track
	}
	tracks, err := s.search(ctx, artist+" "+title)
	if err != nil {
		// A search the caller gave up on, such as one past a search's
		// deadline, is not a Deezer failure.
		if ctx.Err() != nil {
			slog.DebugContext(ctx, "deezer search cancelled", "artist", artist, "title", title, "err", err)
		} else {
			slog.Error("deezer search failed", "artist", artist, "title", title, "err", err)
		}
		return Track{}
	}
	track := matchTrack(tracks, wantArtist, wantTitle)
	ttl := coverCacheTTL
	if track.Cover == "" {
		ttl = noCoverCacheTTL
	}
	s.cache.Put(key, track, ttl)
	return track
}

// matchTrack returns the first of tracks by artist titled title, or else the
// cover alone of the first by artist, since another song's length would be
// wrong. It returns no cover when none is by artist. artist and title are
// folded.
func matchTrack(tracks []searchTrack, artist, title string) Track {
	var match Track
	for _, t := range tracks {
		if fold(t.Artist.Name) != artist || !ValidCoverID(t.Album.MD5Image) {
			continue
		}
		if fold(t.Title) == title {
			return Track{Cover: t.Album.MD5Image, Duration: time.Duration(t.Duration) * time.Second}
		}
		if match.Cover == "" {
			match.Cover = t.Album.MD5Image
		}
	}
	return match
}

func (s *CoverArtService) search(ctx context.Context, query string) ([]searchTrack, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	u := s.apiURL.JoinPath("search")
	u.RawQuery = url.Values{
		"q":     {query},
		"limit": {strconv.Itoa(searchLimit)},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "deezer request", "method", "search", "query", u.RawQuery)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	if body.Error != nil {
		return nil, fmt.Errorf("deezer error %d: %s", body.Error.Code, body.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return body.Data, nil
}

// Fetch requests cover id as a square JPEG of size pixels, the default size
// when size is not positive, and returns the response for the caller to close.
// The CDN must start answering within requestTimeout; the image itself streams
// for as long as the caller reads it, until ctx ends. A cover Deezer does not
// have is ErrNotFound.
func (s *CoverArtService) Fetch(ctx context.Context, id string, size int) (*http.Response, error) {
	if !ValidCoverID(id) {
		return nil, ErrNotFound
	}
	if size <= 0 {
		size = defaultCoverSize
	}
	size = min(max(size, minCoverSize), maxCoverSize)
	n := strconv.Itoa(size)
	u := s.imageURL.JoinPath(id, n+"x"+n+"-000000-80-0-0.jpg")

	ctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	timer := time.AfterFunc(requestTimeout, cancel)
	resp, err := s.client.Do(req)
	answeredInTime := timer.Stop()
	if err != nil {
		cancel()
		return nil, err
	}
	switch {
	case !answeredInTime:
		// The timeout fired as the response arrived, so its body is unusable.
		err = fmt.Errorf("no answer within %v", requestTimeout)
	case resp.StatusCode == http.StatusOK:
		resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusForbidden:
		err = ErrNotFound
	default:
		err = fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	cancel()
	return nil, err
}

// cancelOnClose releases a request's context when its response body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// fold returns s in lowercase without diacritics, so names Deezer spells
// without accents ("Son Tung M-TP") match Last.fm's ("Sơn Tùng M-TP").
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(t, s)
	if err != nil {
		folded = s
	}
	// đ is a letter of its own, not d with a mark, so NFD leaves it whole.
	folded = strings.NewReplacer("đ", "d", "Đ", "d").Replace(folded)
	return strings.ToLower(strings.TrimSpace(folded))
}
