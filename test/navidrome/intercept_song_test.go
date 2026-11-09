package navidrome_test

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/test/utils"
)

// songEnvelope decodes a getSong response that apollo answered itself.
type songEnvelope struct {
	Response struct {
		Status        string         `json:"status"`
		Type          string         `json:"type"`
		ServerVersion string         `json:"serverVersion"`
		Error         subsonicError  `json:"error"`
		Song          map[string]any `json:"song"`
	} `json:"subsonic-response"`
}

// Supersonic looks a song up with getSong before playing it, and plays nothing
// when the lookup fails.
func TestInterceptSongAnswersLastFMSong(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newSongProxy(t, nd.URL, noCovers)
	id := navidrome.LastFMSongID("Justin Bieber", "Baby")

	rec := utils.Serve(proxy, "/rest/getSong.view?"+streamQuery(ndUser, ndPassword, id))

	resp := utils.DecodeJSON[songEnvelope](t, rec).Response
	if resp.Status != "ok" || resp.Type != "navidrome" || resp.ServerVersion != ndServerVersion {
		t.Errorf("response = %+v, want ok from navidrome %s", resp, ndServerVersion)
	}
	if resp.Song["id"] != id || resp.Song["title"] != "Baby" || resp.Song["artist"] != "Justin Bieber" {
		t.Errorf("song = %v, want %s: Justin Bieber - Baby", resp.Song, id)
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

// A song looked up by id must be the song the search returned, so a client
// treats both the same.
func TestInterceptSongMatchesSearchResult(t *testing.T) {
	nd := newFakeNavidrome(t)
	search := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayOnDeezer))
	})
	found := utils.DecodeJSON[rawSearchEnvelope](t, utils.Serve(search, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))).Response.SearchResult3.Song
	if len(found) != 1 || found[0]["coverArt"] == nil {
		t.Fatalf("search songs = %+v, want 1 with coverArt", found)
	}

	getSong := newSongProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayOnDeezer))
	})

	rec := utils.Serve(getSong, "/rest/getSong.view?"+streamQuery(ndUser, ndPassword, found[0]["id"].(string)))

	got := utils.DecodeJSON[songEnvelope](t, rec).Response.Song
	for k, v := range found[0] {
		if got[k] != v {
			t.Errorf("song[%q] = %v, want %v as in search", k, got[k], v)
		}
	}
	if len(got) != len(found[0]) {
		t.Errorf("song = %v, want the search song %v", got, found[0])
	}
}

func TestInterceptSongAnswersXML(t *testing.T) {
	nd := newFakeNavidrome(t)
	query := url.Values{
		"u":  {ndUser},
		"p":  {ndPassword},
		"v":  {subsonicVersion},
		"c":  {"apollo-test"},
		"id": {navidrome.LastFMSongID("Lena", "Say")},
	}

	rec := utils.Serve(newSongProxy(t, nd.URL, noCovers), "/rest/getSong?"+query.Encode())

	if got := rec.Header().Get("Content-Type"); got != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	var resp struct {
		Status string     `xml:"status,attr"`
		Song   lastfmSong `xml:"song"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode xml: %v; body = %s", err, rec.Body)
	}
	want := lastfmSong{ID: query.Get("id"), Title: "Say", Artist: "Lena"}
	if resp.Status != "ok" || resp.Song != want {
		t.Errorf("response = %+v, want ok with %+v", resp, want)
	}
}

func TestInterceptSongAnswersFormPost(t *testing.T) {
	nd := newFakeNavidrome(t)
	id := navidrome.LastFMSongID("Lena", "Say")

	rec := servePost(newSongProxy(t, nd.URL, noCovers), "/rest/getSong.view", streamQuery(ndUser, ndPassword, id))

	resp := utils.DecodeJSON[songEnvelope](t, rec).Response
	if resp.Status != "ok" || resp.Song["id"] != id {
		t.Errorf("response = %+v, want ok with song %s", resp, id)
	}
}

func TestInterceptSongRejectsCredentialsLikeNavidrome(t *testing.T) {
	nd := newFakeNavidrome(t)

	rec := utils.Serve(newSongProxy(t, nd.URL, noCovers), "/rest/getSong.view?"+streamQuery(ndUser, "wrong", navidrome.LastFMSongID("Lena", "Say")))

	resp := utils.DecodeJSON[songEnvelope](t, rec).Response
	want := subsonicError{Code: 40, Message: "Wrong username or password"}
	if resp.Status != "failed" || resp.Error != want || resp.Song != nil {
		t.Errorf("response = %+v, want failed with %+v and no song", resp, want)
	}
}

func TestInterceptSongRejectsMalformedID(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newSongProxy(t, nd.URL, noCovers)

	for _, id := range []string{"lastfm-", "lastfm-!!!"} {
		t.Run(id, func(t *testing.T) {
			rec := utils.Serve(proxy, "/rest/getSong.view?"+streamQuery(ndUser, ndPassword, id))

			resp := utils.DecodeJSON[songEnvelope](t, rec).Response
			if resp.Status != "failed" || resp.Error.Code != 70 || resp.Song != nil {
				t.Errorf("response = %+v, want failed with code 70 and no song", resp)
			}
		})
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptSongNavidromeUnreachable(t *testing.T) {
	upstream := closedUpstream(t)

	rec := utils.Serve(newSongProxy(t, upstream, noCovers), "/rest/getSong.view?"+streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say")))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body)
	}
	resp := utils.DecodeJSON[songEnvelope](t, rec).Response
	if resp.Status != "failed" || resp.Error.Message != unavailableMessage || resp.Song != nil {
		t.Errorf("response = %+v, want %q and no song", resp, unavailableMessage)
	}
}

func TestInterceptSongForwardsOtherRequests(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newSongProxy(t, nd.URL, noCovers)

	for _, target := range []string{
		"/rest/getSong.view?" + streamQuery(ndUser, ndPassword, "library-song"),
		"/rest/getSong.view?" + subsonicQuery(ndUser, ndPassword),
		"/rest/getAlbum.view?" + streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say")),
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

// newSongProxy returns a RestProxy in front of the Navidrome at upstream that
// answers getSong for Last.fm songs, with covers from a Deezer test server
// running deezerHandler.
func newSongProxy(t *testing.T, upstream *url.URL, deezerHandler http.HandlerFunc) *navidrome.RestProxy {
	t.Helper()
	song := navidrome.NewInterceptSongService(utils.NewDeezer(t, deezerHandler), upstream, nil)
	return navidrome.NewRestProxy(upstream, nil, song)
}
