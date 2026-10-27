package navidrome_test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tuananh131001/apollo/internal/lastfm"
	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/test/utils"
)

// Credentials the fake Navidrome accepts, and the server details it reports.
const (
	ndUser          = "alice"
	ndPassword      = "secret"
	ndServerVersion = "0.64.2 (test)"
)

// sayByLena is a one-track Last.fm track.search response.
const sayByLena = `{"results":{"trackmatches":{"track":[{"name":"Say","artist":"Lena","url":"u"}]}}}`

// lastfmEnvelope decodes a search3 response that apollo answered from Last.fm.
type lastfmEnvelope struct {
	Response struct {
		Status        string        `json:"status"`
		Version       string        `json:"version"`
		Type          string        `json:"type"`
		ServerVersion string        `json:"serverVersion"`
		OpenSubsonic  bool          `json:"openSubsonic"`
		Error         subsonicError `json:"error"`
		SearchResult3 *struct {
			Song []lastfmSong `json:"song"`
		} `json:"searchResult3"`
	} `json:"subsonic-response"`
}

type lastfmSong struct {
	ID     string `json:"id" xml:"id,attr"`
	Title  string `json:"title" xml:"title,attr"`
	Artist string `json:"artist" xml:"artist,attr"`
}

func TestParseUserInput(t *testing.T) {
	tests := []struct {
		input, track, artist string
		ok                   bool
	}{
		{input: "/s Baby", track: "Baby", ok: true},
		{input: "  /s   Baby  ", track: "Baby", ok: true},
		{input: "/s Baby @ Justin Bieber", track: "Baby", artist: "Justin Bieber", ok: true},
		{input: "/s Baby@Justin Bieber", track: "Baby", artist: "Justin Bieber", ok: true},
		{input: "/s Stand by Me", track: "Stand by Me", ok: true},
		{input: "/s", ok: true},
		{input: "/s @ Lena", artist: "Lena", ok: true},
		{input: "Baby", ok: false},
		{input: "/sBaby", ok: false},
		{input: "/search Baby", ok: false},
		{input: "Baby /s", ok: false},
		{input: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			track, artist, ok := navidrome.ParseUserInput(tt.input)
			if track != tt.track || artist != tt.artist || ok != tt.ok {
				t.Errorf("ParseUserInput(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.input, track, artist, ok, tt.track, tt.artist, tt.ok)
			}
		})
	}
}

func TestInterceptAnswersSearchCommandFromLastFM(t *testing.T) {
	nd := newFakeNavidrome(t)
	var got url.Values
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"results":{"trackmatches":{"track":[
			{"name":"Baby","artist":"Justin Bieber","url":"https://www.last.fm/music/Justin+Bieber/_/Baby"}
		]}}}`))
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s Baby @ Justin Bieber"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	if got.Get("track") != "Baby" || got.Get("artist") != "Justin Bieber" {
		t.Errorf("last.fm query = %s, want track=Baby artist=Justin Bieber", got.Encode())
	}
	resp := utils.DecodeJSON[lastfmEnvelope](t, rec).Response
	if resp.Status != "ok" || resp.SearchResult3 == nil {
		t.Fatalf("response = %+v; body = %s", resp, rec.Body)
	}
	// The envelope matches a search Navidrome answered itself.
	if resp.Type != "navidrome" || resp.ServerVersion != ndServerVersion || !resp.OpenSubsonic {
		t.Errorf("server details = (%q, %q, %v), want Navidrome's", resp.Type, resp.ServerVersion, resp.OpenSubsonic)
	}
	want := []lastfmSong{{ID: "https://www.last.fm/music/Justin+Bieber/_/Baby", Title: "Baby", Artist: "Justin Bieber"}}
	if !slices.Equal(resp.SearchResult3.Song, want) {
		t.Errorf("songs = %+v, want %+v", resp.SearchResult3.Song, want)
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptSendsOnlyCredentialsToNavidrome(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	})

	utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say")+"&songCount=5")

	pings := nd.Pings()
	if len(pings) != 1 {
		t.Fatalf("navidrome pings = %d, want 1", len(pings))
	}
	want := url.Values{
		"u": {ndUser},
		"p": {ndPassword},
		"v": {subsonicVersion},
		"c": {"apollo-test"},
		"f": {"json"},
	}
	if pings[0].Encode() != want.Encode() {
		t.Errorf("ping query = %s, want %s", pings[0].Encode(), want.Encode())
	}
}

func TestInterceptRejectsCredentialsLikeNavidrome(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("last.fm was called")
	})

	tests := []struct {
		name, query string
		want        subsonicError
	}{
		{
			name:  "missing user",
			query: "f=json&query=" + url.QueryEscape("/s baby"),
			want:  subsonicError{Code: 10, Message: "missing parameter: 'u'"},
		},
		{
			name:  "wrong password",
			query: searchQuery(ndUser, "wrong", "/s baby"),
			want:  subsonicError{Code: 40, Message: "Wrong username or password"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := utils.Serve(proxy, "/rest/search3.view?"+tt.query)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
			}
			resp := utils.DecodeJSON[lastfmEnvelope](t, rec).Response
			if resp.Status != "failed" || resp.Error != tt.want {
				t.Errorf("response = %+v, want failed with %+v", resp, tt.want)
			}
			if resp.Type != "navidrome" || resp.SearchResult3 != nil {
				t.Errorf("response = %+v, want Navidrome's error envelope only", resp)
			}
		})
	}
}

func TestInterceptRejectsCredentialsInXML(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("last.fm was called")
	})

	rec := utils.Serve(proxy, "/rest/search3.view?query="+url.QueryEscape("/s baby"))

	var resp xmlResponse
	if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode xml: %v; body = %s", err, rec.Body)
	}
	want := subsonicError{Code: 10, Message: "missing parameter: 'u'"}
	if resp.Status != "failed" || resp.Error != want {
		t.Errorf("response = %+v, want failed with %+v", resp, want)
	}
}

func TestInterceptAnswersXML(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	})
	query := url.Values{
		"u":     {ndUser},
		"p":     {ndPassword},
		"v":     {subsonicVersion},
		"c":     {"apollo-test"},
		"query": {"/s say"},
	}

	rec := utils.Serve(proxy, "/rest/search3?"+query.Encode())

	if got := rec.Header().Get("Content-Type"); got != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	var resp struct {
		Status string       `xml:"status,attr"`
		Type   string       `xml:"type,attr"`
		Songs  []lastfmSong `xml:"searchResult3>song"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode xml: %v; body = %s", err, rec.Body)
	}
	want := []lastfmSong{{ID: "u", Title: "Say", Artist: "Lena"}}
	if resp.Status != "ok" || resp.Type != "navidrome" || !slices.Equal(resp.Songs, want) {
		t.Errorf("response = %+v, want ok from navidrome with %+v", resp, want)
	}
}

func TestInterceptNavidromeUnreachable(t *testing.T) {
	proxy := newInterceptingProxy(t, closedUpstream(t), func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("last.fm was called")
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body)
	}
	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "failed" || resp.Error.Message != unavailableMessage {
		t.Errorf("response = %+v, want %q", resp, unavailableMessage)
	}
}

func TestInterceptReturnsEmptyResultsWhenLastFMFails(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":10,"message":"Invalid API key"}`))
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	resp := utils.DecodeJSON[lastfmEnvelope](t, rec).Response
	if resp.Status != "ok" || resp.SearchResult3 == nil || len(resp.SearchResult3.Song) != 0 {
		t.Errorf("response = %+v, want ok with no songs; body = %s", resp, rec.Body)
	}
}

func TestInterceptAnswersFormPost(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	})

	rec := servePost(proxy, "/rest/search3.view", searchQuery(ndUser, ndPassword, "/s say"))

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (f=json from the form)", got)
	}
	resp := utils.DecodeJSON[lastfmEnvelope](t, rec).Response
	want := []lastfmSong{{ID: "u", Title: "Say", Artist: "Lena"}}
	if resp.Status != "ok" || resp.SearchResult3 == nil || !slices.Equal(resp.SearchResult3.Song, want) {
		t.Errorf("response = %+v, want ok with %+v; body = %s", resp, want, rec.Body)
	}
	if pings := nd.Pings(); len(pings) != 1 || pings[0].Get("u") != ndUser {
		t.Errorf("navidrome pings = %v, want one for %s", pings, ndUser)
	}
}

func TestInterceptForwardsFormPostBodyUnchanged(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("last.fm was called")
	})
	form := searchQuery(ndUser, ndPassword, "baby")

	servePost(proxy, "/rest/search3.view", form)

	if bodies := nd.Bodies(); !slices.Equal(bodies, []string{form}) {
		t.Errorf("forwarded bodies = %q, want %q", bodies, form)
	}
}

func TestInterceptForwardsOtherRequests(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("last.fm was called")
	})

	for _, target := range []string{
		"/rest/search3.view?" + searchQuery(ndUser, ndPassword, "baby"),
		"/rest/search2.view?" + searchQuery(ndUser, ndPassword, "/s baby"),
		"/rest/getSong.view?" + subsonicQuery(ndUser, ndPassword),
	} {
		t.Run(target, func(t *testing.T) {
			u, _ := url.Parse(target)
			utils.Serve(proxy, target)

			if forwarded := nd.Forwarded(); !slices.Contains(forwarded, u.Path) {
				t.Errorf("forwarded = %v, want %s", forwarded, u.Path)
			}
		})
	}
	if pings := nd.Pings(); len(pings) != 0 {
		t.Errorf("navidrome pings = %d, want 0", len(pings))
	}
}

func TestInterceptOnlyFetchesFirstSongPage(t *testing.T) {
	nd := newFakeNavidrome(t)
	var calls int
	var limit string
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, r *http.Request) {
		calls++
		limit = r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(sayByLena))
	})

	tests := []struct {
		name, params, wantLimit string
		wantCall                bool
	}{
		{name: "default", params: "", wantLimit: "20", wantCall: true},
		{name: "songCount", params: "&songCount=50&songOffset=0", wantLimit: "50", wantCall: true},
		{name: "no songs", params: "&songCount=0", wantCall: false},
		{name: "later page", params: "&songCount=50&songOffset=50", wantCall: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, limit = 0, ""
			rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say")+tt.params)

			resp := utils.DecodeJSON[lastfmEnvelope](t, rec).Response
			if resp.Status != "ok" || resp.SearchResult3 == nil {
				t.Fatalf("response = %+v; body = %s", resp, rec.Body)
			}
			if got := calls == 1; got != tt.wantCall {
				t.Fatalf("last.fm calls = %d, want call = %v", calls, tt.wantCall)
			}
			if tt.wantCall && limit != tt.wantLimit {
				t.Errorf("last.fm limit = %q, want %q", limit, tt.wantLimit)
			}
			if !tt.wantCall && len(resp.SearchResult3.Song) != 0 {
				t.Errorf("songs = %+v, want none", resp.SearchResult3.Song)
			}
		})
	}
}

// Feishin dereferences these fields on every song (contentType.startsWith
// among them), so a song missing any of them breaks the whole search.
func TestInterceptSongsHaveFieldsClientsRequire(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newInterceptingProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))

	type rawEnvelope struct {
		Response struct {
			SearchResult3 struct {
				Song []map[string]any `json:"song"`
			} `json:"searchResult3"`
		} `json:"subsonic-response"`
	}
	songs := utils.DecodeJSON[rawEnvelope](t, rec).Response.SearchResult3.Song
	if len(songs) != 1 {
		t.Fatalf("songs = %+v, want 1", songs)
	}
	for _, field := range []string{
		"id", "parent", "isDir", "isVideo", "title", "contentType", "suffix",
		"path", "size", "created", "discNumber", "type",
	} {
		if _, ok := songs[0][field]; !ok {
			t.Errorf("song has no %q field: %v", field, songs[0])
		}
	}
}

// fakeNavidrome checks Subsonic credentials on ping like Navidrome does and
// records every other request it receives.
type fakeNavidrome struct {
	URL *url.URL

	mu        sync.Mutex
	pings     []url.Values
	forwarded []string
	bodies    []string
}

func newFakeNavidrome(t *testing.T) *fakeNavidrome {
	t.Helper()
	nd := &fakeNavidrome{}
	srv := httptest.NewServer(http.HandlerFunc(nd.serve))
	t.Cleanup(srv.Close)
	nd.URL, _ = url.Parse(srv.URL)
	return nd
}

func (nd *fakeNavidrome) serve(w http.ResponseWriter, r *http.Request) {
	nd.mu.Lock()
	defer nd.mu.Unlock()

	resp := map[string]any{
		"status":        "ok",
		"version":       subsonicVersion,
		"type":          "navidrome",
		"serverVersion": ndServerVersion,
		"openSubsonic":  true,
	}
	if r.URL.Path != "/rest/ping.view" {
		body, _ := io.ReadAll(r.Body)
		nd.forwarded = append(nd.forwarded, r.URL.Path)
		nd.bodies = append(nd.bodies, string(body))
	} else {
		q := r.URL.Query()
		nd.pings = append(nd.pings, q)
		switch {
		case !q.Has("u"):
			resp["status"] = "failed"
			resp["error"] = subsonicError{Code: 10, Message: "missing parameter: 'u'"}
		case q.Get("u") != ndUser || q.Get("p") != ndPassword:
			resp["status"] = "failed"
			resp["error"] = subsonicError{Code: 40, Message: "Wrong username or password"}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": resp})
}

func (nd *fakeNavidrome) Pings() []url.Values {
	nd.mu.Lock()
	defer nd.mu.Unlock()
	return slices.Clone(nd.pings)
}

func (nd *fakeNavidrome) Bodies() []string {
	nd.mu.Lock()
	defer nd.mu.Unlock()
	return slices.Clone(nd.bodies)
}

func (nd *fakeNavidrome) Forwarded() []string {
	nd.mu.Lock()
	defer nd.mu.Unlock()
	return slices.Clone(nd.forwarded)
}

// newInterceptingProxy returns a RestProxy in front of the Navidrome at
// upstream whose "/s" searches go to a Last.fm test server running
// lastfmHandler.
func newInterceptingProxy(t *testing.T, upstream *url.URL, lastfmHandler http.HandlerFunc) *navidrome.RestProxy {
	t.Helper()
	srv := httptest.NewServer(lastfmHandler)
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL + "/2.0/")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	tracks := lastfm.NewTrackService("test-api-key", base, nil)
	search := navidrome.NewInterceptSearchService(tracks, upstream, nil)
	return navidrome.NewRestProxy(upstream, nil, search)
}

// servePost sends form as a form-encoded POST body for target to h.
func servePost(h http.Handler, target, form string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	return rec
}

// searchQuery returns an authenticated search3 query string for query.
func searchQuery(user, password, query string) string {
	return subsonicQuery(user, password) + "&" + url.Values{"query": {query}}.Encode()
}
