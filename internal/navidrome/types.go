package navidrome

import (
	"encoding/xml"
	"net/http"
	"net/http/httputil"
)

// RestProxy forwards /rest/* requests to Navidrome unchanged. It only answers
// on its own when Navidrome cannot be reached; every response Navidrome sends,
// including its own errors, is passed through as-is.
type RestProxy struct {
	proxy *httputil.ReverseProxy
}

// loggingTransport logs every request apollo sends to Navidrome once its
// response headers arrive.
type loggingTransport struct {
	next http.RoundTripper
}

// Subsonic response envelopes used when apollo answers a /rest/* request itself.

type subsonicError struct {
	Code    int    `json:"code" xml:"code,attr"`
	Message string `json:"message" xml:"message,attr"`
}

type jsonEnvelope struct {
	Response jsonResponse `json:"subsonic-response"`
}

type jsonResponse struct {
	Status  string        `json:"status"`
	Version string        `json:"version"`
	Error   subsonicError `json:"error"`
}

type xmlResponse struct {
	XMLName xml.Name      `xml:"subsonic-response"`
	Xmlns   string        `xml:"xmlns,attr"`
	Status  string        `xml:"status,attr"`
	Version string        `xml:"version,attr"`
	Error   subsonicError `xml:"error"`
}
