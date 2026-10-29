package deezer_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tuananh131001/apollo/internal/deezer"
	"github.com/tuananh131001/apollo/test/utils"
)

// The cache tests run in a synctest bubble so its clock can jump hours ahead,
// with Deezer answered in memory: a real server would leave the bubble.

func TestFindCoverCacheExpires(t *testing.T) {
	tests := []struct {
		name, body string
		// fresh is how long the answer is still served from the cache, and
		// stale how long until Deezer is asked again.
		fresh, stale time.Duration
	}{
		{name: "cover", body: `{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`, fresh: 23 * time.Hour, stale: 25 * time.Hour},
		{name: "no cover", body: `{"data":[]}`, fresh: 59 * time.Minute, stale: 61 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				deezer := newMemoryDeezer(func(*http.Request) string { return tt.body })
				svc := deezer.service()
				start := time.Now()

				svc.FindCover(t.Context(), "Lena", "Say")
				time.Sleep(tt.fresh)
				svc.FindCover(t.Context(), "Lena", "Say")
				if got := deezer.Calls(); got != 1 {
					t.Errorf("deezer calls after %v = %d, want 1 (cached)", tt.fresh, got)
				}
				time.Sleep(start.Add(tt.stale).Sub(time.Now()))
				svc.FindCover(t.Context(), "Lena", "Say")
				if got := deezer.Calls(); got != 2 {
					t.Errorf("deezer calls after %v = %d, want 2 (expired)", tt.stale, got)
				}
			})
		})
	}
}

func TestFindCoverCacheEvictsLeastRecentlyUsed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deezer := newMemoryDeezer(func(r *http.Request) string {
			// Every song is by Lena, so every lookup finds a cover.
			title := strings.TrimPrefix(r.URL.Query().Get("q"), "Lena ")
			return `{"data":[` + track(title, "Lena", utils.SayCover) + `]}`
		})
		svc := deezer.service()

		// Fill the cache, which holds 4096 songs, using song 0 last so song 1
		// is the least recently used.
		const size = 4096
		for i := range size {
			svc.FindCover(t.Context(), "Lena", "song "+strconv.Itoa(i))
		}
		svc.FindCover(t.Context(), "Lena", "song 0")
		svc.FindCover(t.Context(), "Lena", "one song too many")
		before := deezer.Calls()

		svc.FindCover(t.Context(), "Lena", "song 0")
		if got := deezer.Calls() - before; got != 0 {
			t.Errorf("recently used song: deezer calls = %d, want 0 (cached)", got)
		}
		svc.FindCover(t.Context(), "Lena", "song 1")
		if got := deezer.Calls() - before; got != 1 {
			t.Errorf("least recently used song: deezer calls = %d, want 1 (evicted)", got)
		}
	})
}

// Two searches looking the same song up at once both ask Deezer; the cache
// keeps one answer.
func TestFindCoverConcurrentLookups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		arrived := make(chan struct{})
		release := make(chan struct{})
		deezer := newMemoryDeezer(func(*http.Request) string {
			arrived <- struct{}{}
			<-release
			return `{"data":[` + track("Say", "Lena", utils.SayCover) + `]}`
		})
		svc := deezer.service()

		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { svc.FindCover(t.Context(), "Lena", "Say") })
		}
		<-arrived
		<-arrived
		close(release)
		wg.Wait()

		if got := svc.FindCover(t.Context(), "Lena", "Say"); got != utils.SayCover {
			t.Errorf("cover = %q, want %q", got, utils.SayCover)
		}
		if got := deezer.Calls(); got != 2 {
			t.Errorf("deezer calls = %d, want 2", got)
		}
	})
}

// memoryDeezer answers Deezer searches in memory with respond's body.
type memoryDeezer struct {
	respond func(*http.Request) string

	mu    sync.Mutex
	calls int
}

func newMemoryDeezer(respond func(*http.Request) string) *memoryDeezer {
	return &memoryDeezer{respond: respond}
}

func (d *memoryDeezer) RoundTrip(r *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(d.respond(r))),
		Request:    r,
	}, nil
}

func (d *memoryDeezer) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// service returns a CoverArtService that sends its requests to d.
func (d *memoryDeezer) service() *deezer.CoverArtService {
	return utils.NewDeezerOn(d)
}
