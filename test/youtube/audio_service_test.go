package youtube_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tuananh131001/apollo/internal/youtube"
)

func TestFetchReturnsAudio(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "printf 'fake-m4a-audio'"), 1)

	audio, err := s.Fetch(context.Background(), "Lena", "Say")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer func() { _ = audio.Close() }()

	if got, _ := io.ReadAll(audio); string(got) != "fake-m4a-audio" {
		t.Errorf("audio = %q, want fake-m4a-audio", got)
	}
}

// yt-dlp's default YouTube clients wait about 5s for a pre-roll ad before the
// audio starts; the visionos client, tried first, does not.
func TestFetchPrefersPlayerClientWithoutAdWait(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, `printf '%s\n' "$@"`), 1)

	audio, err := s.Fetch(context.Background(), "Lena", "Say")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer func() { _ = audio.Close() }()
	args, _ := io.ReadAll(audio)

	if !strings.Contains(string(args), "youtube:player_client=visionos,default") {
		t.Errorf("yt-dlp args = %q, want visionos first, then the default clients", args)
	}
}

func TestFetchReportsNoMatchAsNotFound(t *testing.T) {
	// yt-dlp succeeds without output when YouTube Music has no song.
	s := youtube.NewAudioService(writeYtDlp(t, "exit 0"), 1)

	_, err := s.Fetch(context.Background(), "Lena", "Say")

	if !errors.Is(err, youtube.ErrNotFound) {
		t.Errorf("Fetch error = %v, want ErrNotFound", err)
	}
}

func TestFetchReportsYtDlpFailure(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "echo 'ERROR: HTTP Error 403: Forbidden' >&2; exit 1"), 1)

	_, err := s.Fetch(context.Background(), "Lena", "Say")

	if err == nil || errors.Is(err, youtube.ErrNotFound) {
		t.Errorf("Fetch error = %v, want a failure other than ErrNotFound", err)
	}
}

func TestFetchRejectsWhenAllSlotsAreBusy(t *testing.T) {
	// Sends some audio, then keeps the download open.
	s := youtube.NewAudioService(writeYtDlp(t, "printf 'audio'; exec sleep 30"), 1)

	first, err := s.Fetch(context.Background(), "Lena", "Say")
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if _, err := s.Fetch(context.Background(), "Lena", "Say"); !errors.Is(err, youtube.ErrBusy) {
		t.Errorf("Fetch while busy error = %v, want ErrBusy", err)
	}

	_ = first.Close()
	next, err := s.Fetch(context.Background(), "Lena", "Say")
	if err != nil {
		t.Fatalf("Fetch after Close: %v, want the slot freed", err)
	}
	_ = next.Close()
}

func TestFetchFreesSlotAfterFailure(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "exit 1"), 1)

	for range 3 {
		if _, err := s.Fetch(context.Background(), "Lena", "Say"); errors.Is(err, youtube.ErrBusy) {
			t.Fatal("Fetch after a failure = ErrBusy, want the slot freed")
		}
	}
}

// ytDlpSayJSON is what yt-dlp -j prints for Lena's "Say", trimmed.
const ytDlpSayJSON = `{"_type":"video","id":"tUg5Wsv7kNI","title":"SAY","duration":177.4,"format_id":"140","url":"https://rr1---sn.googlevideo.com/videoplayback","filesize":2868137}`

func TestLookupReturnsDuration(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "echo '"+ytDlpSayJSON+"'"), 1)

	song, err := s.Lookup(context.Background(), "Lena", "Say")

	if err != nil || song.Duration != 177*time.Second {
		t.Errorf("Lookup = %+v, %v; want 177s", song, err)
	}
}

// The lookup asks the player client Fetch asks, so both find the same audio.
func TestLookupDoesNotDownload(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	s := youtube.NewAudioService(writeYtDlp(t, "echo \"$@\" > '"+args+"'; echo '"+ytDlpSayJSON+"'"), 1)

	if _, err := s.Lookup(context.Background(), "Lena", "Say"); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	got, _ := os.ReadFile(args)
	for _, want := range []string{"--skip-download", "--dump-json", "music.youtube.com/search", "youtube:player_client=visionos,default"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("yt-dlp args = %q, want %s", got, want)
		}
	}
}

func TestLookupReportsNoMatchAsNotFound(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "exit 0"), 1)

	_, err := s.Lookup(context.Background(), "Lena", "Say")

	if !errors.Is(err, youtube.ErrNotFound) {
		t.Errorf("Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLookupReportsYtDlpFailure(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "echo 'ERROR: HTTP Error 403: Forbidden' >&2; exit 1"), 1)

	_, err := s.Lookup(context.Background(), "Lena", "Say")

	if err == nil || errors.Is(err, youtube.ErrNotFound) || !strings.Contains(err.Error(), "403") {
		t.Errorf("Lookup error = %v, want a failure with yt-dlp's stderr", err)
	}
}

func TestLookupCachesAnswers(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	s := youtube.NewAudioService(writeYtDlp(t, "echo run >> '"+runs+"'; echo '"+ytDlpSayJSON+"'"), 1)

	for range 2 {
		if _, err := s.Lookup(context.Background(), "Lena", "Say"); err != nil {
			t.Fatalf("Lookup: %v", err)
		}
	}

	if got, _ := os.ReadFile(runs); strings.Count(string(got), "run") != 1 {
		t.Errorf("yt-dlp ran %d times, want 1", strings.Count(string(got), "run"))
	}
}

func TestLookupDoesNotCacheFailures(t *testing.T) {
	// Fails the first time only.
	failed := filepath.Join(t.TempDir(), "failed")
	s := youtube.NewAudioService(writeYtDlp(t, "[ -e '"+failed+"' ] || { touch '"+failed+"'; exit 1; }; echo '"+ytDlpSayJSON+"'"), 1)

	if _, err := s.Lookup(context.Background(), "Lena", "Say"); err == nil {
		t.Fatal("first Lookup succeeded, want a failure")
	}
	if song, err := s.Lookup(context.Background(), "Lena", "Say"); err != nil || song.Duration == 0 {
		t.Errorf("second Lookup = %+v, %v; want the song", song, err)
	}
}

func TestLookupRejectsWhenAllSlotsAreBusy(t *testing.T) {
	s := youtube.NewAudioService(writeYtDlp(t, "printf 'audio'; exec sleep 30"), 1)
	playing, err := s.Fetch(context.Background(), "Lena", "Say")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer func() { _ = playing.Close() }()

	if _, err := s.Lookup(context.Background(), "Lena", "Say"); !errors.Is(err, youtube.ErrBusy) {
		t.Errorf("Lookup while busy error = %v, want ErrBusy", err)
	}
}

func TestCheck(t *testing.T) {
	if err := youtube.NewAudioService(writeYtDlp(t, "exit 0"), 1).Check(); err != nil {
		t.Errorf("Check with yt-dlp installed = %v, want nil", err)
	}
	missing := filepath.Join(t.TempDir(), "yt-dlp")
	if err := youtube.NewAudioService(missing, 1).Check(); err == nil {
		t.Error("Check with yt-dlp missing = nil, want an error")
	}
}

// writeYtDlp writes a stand-in yt-dlp that runs the shell script body.
func writeYtDlp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}
	return path
}
