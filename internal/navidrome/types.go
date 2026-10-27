package navidrome

import (
	"encoding/xml"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/tuananh131001/apollo/internal/lastfm"
)

// RestProxy forwards /rest/* requests to Navidrome unchanged, except searches
// its InterceptSearchService answers itself. It also answers when Navidrome
// cannot be reached; every response Navidrome sends, including its own errors,
// is passed through as-is.
type RestProxy struct {
	proxy  *httputil.ReverseProxy
	search *InterceptSearchService
}

// InterceptSearchService answers Subsonic searches written as "/s" commands
// with Last.fm results instead of Navidrome's library.
type InterceptSearchService struct {
	tracks   *lastfm.TrackService
	upstream *url.URL
	client   *http.Client
}

// Subsonic response envelope used when apollo answers a /rest/* request itself.
// The same struct encodes to JSON (inside jsonEnvelope) and to XML.

type jsonEnvelope struct {
	Response subsonicResponse `json:"subsonic-response"`
}

type subsonicResponse struct {
	XMLName       xml.Name       `json:"-" xml:"subsonic-response"`
	Xmlns         string         `json:"-" xml:"xmlns,attr"`
	Status        string         `json:"status" xml:"status,attr"`
	Version       string         `json:"version" xml:"version,attr"`
	Type          string         `json:"type,omitempty" xml:"type,attr,omitempty"`
	ServerVersion string         `json:"serverVersion,omitempty" xml:"serverVersion,attr,omitempty"`
	OpenSubsonic  bool           `json:"openSubsonic,omitempty" xml:"openSubsonic,attr,omitempty"`
	Error         *subsonicError `json:"error,omitempty" xml:"error,omitempty"`
	SearchResult3 *searchResult3 `json:"searchResult3,omitempty" xml:"searchResult3,omitempty"`
}

type subsonicError struct {
	Code    int    `json:"code" xml:"code,attr"`
	Message string `json:"message" xml:"message,attr"`
}

type searchResult3 struct {
	Song []song `json:"song,omitempty" xml:"song"`
}

// song carries the fields Subsonic clients require of every song, even when
// apollo has no value for them.
type song struct {
	ID          string `json:"id" xml:"id,attr"`
	Parent      string `json:"parent" xml:"parent,attr"`
	IsDir       bool   `json:"isDir" xml:"isDir,attr"`
	IsVideo     bool   `json:"isVideo" xml:"isVideo,attr"`
	Title       string `json:"title" xml:"title,attr"`
	Album       string `json:"album" xml:"album,attr"`
	Artist      string `json:"artist" xml:"artist,attr"`
	DiscNumber  int    `json:"discNumber" xml:"discNumber,attr"`
	Size        int64  `json:"size" xml:"size,attr"`
	ContentType string `json:"contentType" xml:"contentType,attr"`
	Suffix      string `json:"suffix" xml:"suffix,attr"`
	Path        string `json:"path" xml:"path,attr"`
	Created     string `json:"created" xml:"created,attr"`
	Type        string `json:"type" xml:"type,attr"`
}
