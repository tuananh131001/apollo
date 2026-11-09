package navidrome_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/test/utils"
)

// sayOnDeezer is a Deezer search response holding Lena's "Say".
const sayOnDeezer = `{"data":[{"title":"Say","artist":{"name":"Lena"},"album":{"md5_image":"` + utils.SayCover + `"}}],"total":1}`

// coverImage stands in for a cover's JPEG bytes.
const coverImage = "\xff\xd8\xff fake jpeg"

// noCovers is a Deezer that finds nothing.
func noCovers(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"data":[],"total":0}`))
}

// fakeCDN serves coverImage for utils.SayCover's images and records the paths it is
// asked for.
type fakeCDN struct {
	mu    sync.Mutex
	paths []string
}

func (c *fakeCDN) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, "/images/cover/"+utils.SayCover+"/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write([]byte(coverImage))
}

func (c *fakeCDN) Paths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.paths)
}

func TestInterceptSearchAddsDeezerCover(t *testing.T) {
	nd := newFakeNavidrome(t)
	var deezerQuery string
	proxy := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}, func(w http.ResponseWriter, r *http.Request) {
		deezerQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(sayOnDeezer))
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))

	songs := utils.DecodeJSON[rawSearchEnvelope](t, rec).Response.SearchResult3.Song
	if len(songs) != 1 {
		t.Fatalf("songs = %+v, want 1", songs)
	}
	if songs[0]["coverArt"] != "dz-"+utils.SayCover {
		t.Errorf("coverArt = %v, want dz-%s", songs[0]["coverArt"], utils.SayCover)
	}
	if want := navidrome.LastFMSongID("Lena", "Say"); songs[0]["id"] != want {
		t.Errorf("id = %v, want %s", songs[0]["id"], want)
	}
	if deezerQuery != "Lena Say" {
		t.Errorf("deezer query = %q, want %q", deezerQuery, "Lena Say")
	}
}

func TestInterceptSearchKeepsSongsWhenDeezerFails(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
	})

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))

	songs := utils.DecodeJSON[rawSearchEnvelope](t, rec).Response.SearchResult3.Song
	if len(songs) != 1 || songs[0]["id"] != navidrome.LastFMSongID("Lena", "Say") {
		t.Fatalf("songs = %+v, want Lena - Say", songs)
	}
	if _, ok := songs[0]["coverArt"]; ok {
		t.Errorf("song = %v, want no coverArt", songs[0])
	}
}

// P2 #2: the same Last.fm track must keep one id whether or not its cover was
// found, or clients treat it as two songs in queues, playlists and stars.
func TestInterceptSearchSongIDDoesNotDependOnDeezer(t *testing.T) {
	nd := newFakeNavidrome(t)
	var mu sync.Mutex
	deezerUp := false
	proxy := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		up := deezerUp
		mu.Unlock()
		if !up {
			_, _ = w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		_, _ = w.Write([]byte(sayOnDeezer))
	})
	target := "/rest/search3.view?" + searchQuery(ndUser, ndPassword, "/s say")

	withoutCover := utils.DecodeJSON[rawSearchEnvelope](t, utils.Serve(proxy, target)).Response.SearchResult3.Song
	mu.Lock()
	deezerUp = true
	mu.Unlock()
	withCover := utils.DecodeJSON[rawSearchEnvelope](t, utils.Serve(proxy, target)).Response.SearchResult3.Song

	if len(withoutCover) != 1 || len(withCover) != 1 || withCover[0]["coverArt"] == nil {
		t.Fatalf("songs = %v then %v, want Lena - Say, then with coverArt", withoutCover, withCover)
	}
	if withoutCover[0]["id"] != withCover[0]["id"] {
		t.Errorf("ids = %v then %v, want the same song id", withoutCover[0]["id"], withCover[0]["id"])
	}
}

// A slow Deezer delays a search by at most the cover deadline; its songs come
// back without covers.
func TestInterceptSearchStopsWaitingForSlowDeezer(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sayByLena))
	}, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		_, _ = w.Write([]byte(sayOnDeezer))
	})

	start := time.Now()
	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s say"))
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Errorf("search took %v, want about the 2s cover deadline", elapsed)
	}
	songs := utils.DecodeJSON[rawSearchEnvelope](t, rec).Response.SearchResult3.Song
	if len(songs) != 1 || songs[0]["coverArt"] != nil {
		t.Errorf("songs = %+v, want Lena - Say without coverArt", songs)
	}
}

// Results come back in Last.fm's order, whichever cover lookup ends first.
func TestInterceptSearchKeepsLastFMOrder(t *testing.T) {
	nd := newFakeNavidrome(t)
	var lastfmTracks []string
	for i := range 20 {
		lastfmTracks = append(lastfmTracks, `{"name":"Song `+string(rune('A'+i))+`","artist":"Lena","url":"u"}`)
	}
	proxy := newCoveringProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"trackmatches":{"track":[` + strings.Join(lastfmTracks, ",") + `]}}}`))
	}, noCovers)

	rec := utils.Serve(proxy, "/rest/search3.view?"+searchQuery(ndUser, ndPassword, "/s song"))

	songs := utils.DecodeJSON[rawSearchEnvelope](t, rec).Response.SearchResult3.Song
	if len(songs) != 20 {
		t.Fatalf("songs = %d, want 20", len(songs))
	}
	for i, s := range songs {
		if want := "Song " + string(rune('A'+i)); s["title"] != want {
			t.Errorf("song %d = %v, want %s", i, s["title"], want)
		}
	}
}

func TestInterceptCoverArtServesDeezerImage(t *testing.T) {
	nd := newFakeNavidrome(t)
	cdn := &fakeCDN{}
	proxy := newCoverArtProxy(t, nd.URL, cdn.serve)

	rec := utils.Serve(proxy, "/rest/getCoverArt.view?"+streamQuery(ndUser, ndPassword, "dz-"+utils.SayCover)+"&size=300")

	if rec.Code != http.StatusOK || rec.Body.String() != coverImage {
		t.Fatalf("response = %d %q, want 200 with the image", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=") {
		t.Errorf("Cache-Control = %q, want a max-age", got)
	}
	want := []string{"/images/cover/" + utils.SayCover + "/300x300-000000-80-0-0.jpg"}
	if paths := cdn.Paths(); !slices.Equal(paths, want) {
		t.Errorf("cdn paths = %v, want %v", paths, want)
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptCoverArtSize(t *testing.T) {
	tests := []struct{ size, want string }{
		{size: "", want: "1000x1000"},
		{size: "abc", want: "1000x1000"},
		{size: "5000", want: "1000x1000"},
		{size: "1", want: "32x32"},
		{size: "64", want: "64x64"},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			cdn := &fakeCDN{}
			proxy := newCoverArtProxy(t, newFakeNavidrome(t).URL, cdn.serve)

			utils.Serve(proxy, "/rest/getCoverArt.view?"+streamQuery(ndUser, ndPassword, "dz-"+utils.SayCover)+"&size="+tt.size)

			want := []string{"/images/cover/" + utils.SayCover + "/" + tt.want + "-000000-80-0-0.jpg"}
			if paths := cdn.Paths(); !slices.Equal(paths, want) {
				t.Errorf("cdn paths = %v, want %v", paths, want)
			}
		})
	}
}

func TestInterceptCoverArtRejectsCredentialsLikeNavidrome(t *testing.T) {
	nd := newFakeNavidrome(t)
	cdn := &fakeCDN{}
	proxy := newCoverArtProxy(t, nd.URL, cdn.serve)

	rec := utils.Serve(proxy, "/rest/getCoverArt.view?"+streamQuery(ndUser, "wrong", "dz-"+utils.SayCover))

	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	want := subsonicError{Code: 40, Message: "Wrong username or password"}
	if resp.Status != "failed" || resp.Error != want {
		t.Errorf("response = %+v, want failed with %+v", resp, want)
	}
	if paths := cdn.Paths(); len(paths) != 0 {
		t.Errorf("cdn paths = %v, want none", paths)
	}
}

func TestInterceptCoverArtNotFound(t *testing.T) {
	nd := newFakeNavidrome(t)
	cdn := &fakeCDN{}
	proxy := newCoverArtProxy(t, nd.URL, cdn.serve)

	for _, tt := range []struct {
		name, id string
		fetched  bool
	}{
		{name: "malformed", id: "dz-../../etc/passwd", fetched: false},
		{name: "empty", id: "dz-", fetched: false},
		{name: "unknown to deezer", id: "dz-00000000000000000000000000000000", fetched: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := len(cdn.Paths())

			rec := utils.Serve(proxy, "/rest/getCoverArt.view?"+streamQuery(ndUser, ndPassword, tt.id))

			resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
			if resp.Status != "failed" || resp.Error.Code != 70 {
				t.Errorf("response = %+v, want failed with code 70", resp)
			}
			if fetched := len(cdn.Paths()) > before; fetched != tt.fetched {
				t.Errorf("cdn fetched = %v, want %v", fetched, tt.fetched)
			}
		})
	}
	if forwarded := nd.Forwarded(); len(forwarded) != 0 {
		t.Errorf("forwarded to navidrome: %v, want nothing", forwarded)
	}
}

func TestInterceptCoverArtCDNFailure(t *testing.T) {
	nd := newFakeNavidrome(t)
	proxy := newCoverArtProxy(t, nd.URL, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	rec := utils.Serve(proxy, "/rest/getCoverArt.view?"+streamQuery(ndUser, ndPassword, "dz-"+utils.SayCover))

	resp := utils.DecodeJSON[jsonEnvelope](t, rec).Response
	if resp.Status != "failed" || resp.Error.Code != 0 {
		t.Errorf("response = %+v, want failed with code 0", resp)
	}
}

func TestInterceptCoverArtForwardsLibraryCovers(t *testing.T) {
	nd := newFakeNavidrome(t)
	cdn := &fakeCDN{}
	proxy := newCoverArtProxy(t, nd.URL, cdn.serve)

	for _, target := range []string{
		"/rest/getCoverArt.view?" + streamQuery(ndUser, ndPassword, "al-7orkzq9nRdgry1xMEZTmRw"),
		"/rest/getCoverArt?" + subsonicQuery(ndUser, ndPassword),
		"/rest/getSong.view?" + streamQuery(ndUser, ndPassword, "dz-"+utils.SayCover),
	} {
		t.Run(target, func(t *testing.T) {
			u, _ := url.Parse(target)
			utils.Serve(proxy, target)

			if forwarded := nd.Forwarded(); !slices.Contains(forwarded, u.Path) {
				t.Errorf("forwarded = %v, want %s", forwarded, u.Path)
			}
		})
	}
	if paths := cdn.Paths(); len(paths) != 0 {
		t.Errorf("cdn paths = %v, want none", paths)
	}
}

// rawSearchEnvelope decodes a search3 response's songs with every field.
type rawSearchEnvelope struct {
	Response struct {
		SearchResult3 struct {
			Song []map[string]any `json:"song"`
		} `json:"searchResult3"`
	} `json:"subsonic-response"`
}

// newCoverArtProxy returns a RestProxy in front of the Navidrome at upstream
// that serves Last.fm songs' covers from a Deezer test server running
// deezerHandler.
func newCoverArtProxy(t *testing.T, upstream *url.URL, deezerHandler http.HandlerFunc) *navidrome.RestProxy {
	t.Helper()
	coverArt := navidrome.NewInterceptCoverArtService(utils.NewDeezer(t, deezerHandler), upstream, nil)
	return navidrome.NewRestProxy(upstream, nil, coverArt)
}
