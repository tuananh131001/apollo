package navidrome_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/internal/server"
	"github.com/tuananh131001/apollo/internal/youtube"
)

// ytDlpEndless sends audio until it is killed, like a long song.
const ytDlpEndless = "exec yes fake-m4a-audio"

func TestInterceptStreamFreesSlotWhenClientStopsReading(t *testing.T) {
	nd := newFakeNavidrome(t)
	audio := youtube.NewAudioService(newFakeYtDlp(t, ytDlpEndless).Path, 1)
	stream := navidrome.NewInterceptStreamService(audio, 200*time.Millisecond, nd.URL, nil)
	srv := httptest.NewServer(server.NewHandler(navidrome.NewRestProxy(nd.URL, nil, stream)))
	t.Cleanup(srv.Close)
	target := "/rest/stream.view?" + streamQuery(ndUser, ndPassword, navidrome.LastFMSongID("Lena", "Say"))

	// A client that asks for the song, then never reads it, takes the only
	// yt-dlp slot.
	stalled, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stalled.Close() })
	if _, err := fmt.Fprintf(stalled, "GET %s HTTP/1.1\r\nHost: apollo\r\n\r\n", target); err != nil {
		t.Fatal(err)
	}

	// The stalled stream must give the slot back, so another client can play.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(srv.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.Header.Get("Content-Type") == youtube.ContentType {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a client that stopped reading still holds the only yt-dlp slot after 5s (last response %s)", resp.Header.Get("Content-Type"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
