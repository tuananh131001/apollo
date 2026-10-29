package deezer_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tuananh131001/apollo/internal/deezer"
	"github.com/tuananh131001/apollo/test/utils"
)

func TestFindCoverSendsQuery(t *testing.T) {
	var got url.Values
	var path string
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, r *http.Request) {
		path, got = r.URL.Path, r.URL.Query()
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	svc.FindCover(t.Context(), "Lena", "Say")

	want := url.Values{"q": {"Lena Say"}, "limit": {"10"}}
	if path != "/api/search" || got.Encode() != want.Encode() {
		t.Errorf("request = %s?%s, want /api/search?%s", path, got.Encode(), want.Encode())
	}
}

func TestFindCoverDecodesRealResponse(t *testing.T) {
	// The first three matches of a real search for "Son Tung M-TP Lạc Trôi".
	body, err := os.ReadFile("testdata/search_lac_troi.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})

	// Deezer spells the artist without diacritics.
	got := svc.FindCover(t.Context(), "Sơn Tùng M-TP", "Lạc Trôi")

	if want := "f9caaa021815b0d96117d4fad11379d6"; got != want {
		t.Errorf("cover = %q, want %q", got, want)
	}
}

func TestFindCoverMatching(t *testing.T) {
	tests := []struct {
		name, artist, title, body, want string
	}{
		{
			name:   "skips other artists",
			artist: "Lena", title: "Say",
			body: `{"data":[` + track("Say Yes", "Lana Del Rey", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") + `,` + track("SAY (feat. T.R.I)", "Lena", utils.SayCover) + `]}`,
			want: utils.SayCover,
		},
		{
			name:   "prefers the same title",
			artist: "Lena", title: "Say",
			body: `{"data":[` + track("Satellite", "Lena", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") + `,` + track("SAY", "LENA", utils.SayCover) + `]}`,
			want: utils.SayCover,
		},
		{
			name:   "folds đ",
			artist: "Đen", title: "Bài Này Chill Phết",
			body: `{"data":[` + track("Bai Nay Chill Phet", "Den", utils.SayCover) + `]}`,
			want: utils.SayCover,
		},
		{
			name:   "no artist match",
			artist: "Lena", title: "Say",
			body: `{"data":[` + track("Say", "Lana Del Rey", utils.SayCover) + `]}`,
			want: "",
		},
		{
			name:   "invalid cover id",
			artist: "Lena", title: "Say",
			body: `{"data":[` + track("Say", "Lena", "../x") + `]}`,
			want: "",
		},
		{
			name:   "nothing found",
			artist: "Lena", title: "Say",
			body: `{"data":[],"total":0}`,
			want: "",
		},
		{
			name:   "deezer error",
			artist: "Lena", title: "Say",
			body: `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`,
			want: "",
		},
		{
			name:   "not json",
			artist: "Lena", title: "Say",
			body: `<html>`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})

			if got := svc.FindCover(t.Context(), tt.artist, tt.title); got != tt.want {
				t.Errorf("FindCover(%q, %q) = %q, want %q", tt.artist, tt.title, got, tt.want)
			}
		})
	}
}

func TestFindCoverHTTPFailure(t *testing.T) {
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`))
	})

	if got := svc.FindCover(t.Context(), "Lena", "Say"); got != "" {
		t.Errorf("cover = %q, want none", got)
	}
}

func TestFindCoverCachesAnswers(t *testing.T) {
	for _, tt := range []struct{ name, body, want string }{
		{name: "cover", body: `{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`, want: utils.SayCover},
		{name: "no cover", body: `{"data":[]}`, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_, _ = w.Write([]byte(tt.body))
			})

			first := svc.FindCover(t.Context(), "Lena", "Say")
			// The same song, spelled differently.
			second := svc.FindCover(t.Context(), "LENA", "say")

			if first != tt.want || second != tt.want {
				t.Errorf("covers = %q, %q; want %q twice", first, second, tt.want)
			}
			if calls != 1 {
				t.Errorf("deezer calls = %d, want 1", calls)
			}
		})
	}
}

func TestFindCoverDoesNotCacheFailures(t *testing.T) {
	calls := 0
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`))
	})

	first := svc.FindCover(t.Context(), "Lena", "Say")
	second := svc.FindCover(t.Context(), "Lena", "Say")

	if first != "" || second != utils.SayCover {
		t.Errorf("covers = %q, %q; want none, then %q", first, second, utils.SayCover)
	}
}

func TestFindCoverCancelled(t *testing.T) {
	calls := 0
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`))
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cancelled := svc.FindCover(ctx, "Lena", "Say")
	// A cancelled lookup is not remembered as no cover.
	later := svc.FindCover(t.Context(), "Lena", "Say")

	if cancelled != "" || later != utils.SayCover {
		t.Errorf("covers = %q, %q; want none, then %q", cancelled, later, utils.SayCover)
	}
}

func TestFetch(t *testing.T) {
	var path string
	svc := utils.NewDeezer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	})

	resp, err := svc.Fetch(t.Context(), utils.SayCover, 250)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	_ = resp.Body.Close()

	if want := "/images/cover/" + utils.SayCover + "/250x250-000000-80-0-0.jpg"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
}

// P2 #4: a cover that takes longer than the request timeout to stream, such as
// a large one sent to a slow client, must still arrive whole.
func TestFetchStreamsSlowImages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const chunks = 8
		svc := utils.NewDeezerOn(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"image/jpeg"}},
				Body:       &slowBody{ctx: r.Context(), chunks: chunks},
				Request:    r,
			}, nil
		}))

		resp, err := svc.Fetch(t.Context(), utils.SayCover, 1000)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)

		if err != nil || len(body) != chunks {
			t.Errorf("read %d of %d bytes, err = %v; want the whole image", len(body), chunks, err)
		}
	})
}

// The timeout still bounds a CDN that does not answer at all.
func TestFetchTimesOutWaitingForCDN(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := utils.NewDeezerOn(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}))
		start := time.Now()

		_, err := svc.Fetch(t.Context(), utils.SayCover, 1000)

		if err == nil {
			t.Fatal("Fetch succeeded, want a timeout")
		}
		if waited := time.Since(start); waited != 5*time.Second {
			t.Errorf("waited %v, want the 5s timeout", waited)
		}
	})
}

// slowBody sends one byte a second, chunks times, and fails once ctx ends.
type slowBody struct {
	ctx    context.Context
	chunks int
}

func (b *slowBody) Read(p []byte) (int, error) {
	if b.chunks == 0 {
		return 0, io.EOF
	}
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-time.After(time.Second):
	}
	b.chunks--
	p[0] = 'x'
	return 1, nil
}

func (b *slowBody) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchErrors(t *testing.T) {
	tests := []struct {
		name, id string
		status   int
		notFound bool
	}{
		{name: "invalid id", id: "../../etc", status: http.StatusOK, notFound: true},
		{name: "404", id: utils.SayCover, status: http.StatusNotFound, notFound: true},
		{name: "500", id: utils.SayCover, status: http.StatusInternalServerError, notFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			svc := utils.NewDeezer(t, func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(tt.status)
			})

			resp, err := svc.Fetch(t.Context(), tt.id, 0)

			if err == nil {
				_ = resp.Body.Close()
				t.Fatal("Fetch succeeded, want an error")
			}
			if got := errors.Is(err, deezer.ErrNotFound); got != tt.notFound {
				t.Errorf("err = %v, want ErrNotFound = %v", err, tt.notFound)
			}
			if !deezer.ValidCoverID(tt.id) && called {
				t.Error("CDN was called for an invalid id")
			}
		})
	}
}

func TestValidCoverID(t *testing.T) {
	for id, want := range map[string]bool{
		utils.SayCover:                      true,
		"":                                  false,
		"911A6E5E5E5D64DFF4E73094BEAE7783":  false,
		"911a6e5e5e5d64dff4e73094beae778":   false,
		"911a6e5e5e5d64dff4e73094beae77830": false,
		"911a6e5e5e5d64dff4e73094beae778g":  false,
		"../../../../../../../../../../../": false,
	} {
		if got := deezer.ValidCoverID(id); got != want {
			t.Errorf("ValidCoverID(%q) = %v, want %v", id, got, want)
		}
	}
}

// track is a Deezer search match.
func track(title, artist, cover string) string {
	return `{"title":"` + title + `","artist":{"name":"` + artist + `"},"album":{"md5_image":"` + cover + `"}}`
}
