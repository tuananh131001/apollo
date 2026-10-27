// Package youtube fetches song audio from YouTube Music with yt-dlp.
package youtube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// audioFormat is YouTube's ~128 kbps AAC audio in an MP4 container, which
// yt-dlp downloads without needing ffmpeg.
const audioFormat = "140"

// ContentType is the media type of the audio Fetch returns.
const ContentType = "audio/mp4"

// firstByteTimeout bounds how long yt-dlp may take to start sending audio.
const firstByteTimeout = 30 * time.Second

// headSize is how much audio Fetch reads before returning.
const headSize = 32 << 10

// maxStderr caps how much of yt-dlp's stderr is kept for error messages.
const maxStderr = 2 << 10

// ErrNotFound is returned when YouTube Music has no song for a query.
var ErrNotFound = errors.New("youtube music has no matching song")

// AudioService downloads songs from YouTube Music by running yt-dlp.
type AudioService struct {
	ytdlp string
}

// NewAudioService returns an AudioService that runs the yt-dlp executable at
// ytdlpPath, which is looked up in PATH when it has no slash.
func NewAudioService(ytdlpPath string) *AudioService {
	return &AudioService{ytdlp: ytdlpPath}
}

// Fetch searches YouTube Music for artist and title and returns the first
// song's audio as yt-dlp downloads it, once the first bytes have arrived. The
// download stops when ctx is cancelled or the audio is closed.
func (s *AudioService) Fetch(ctx context.Context, artist, title string) (io.ReadCloser, error) {
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, s.ytdlp,
		"--format", audioFormat,
		"--playlist-items", "1",
		"--no-progress",
		"--output", "-",
		searchURL(artist, title),
	)
	cmd.WaitDelay = time.Second
	stderr := &tailBuffer{max: maxStderr}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start yt-dlp: %w", err)
	}
	a := &audio{cmd: cmd, stdout: stdout, stderr: stderr, cancel: cancel}

	timer := time.AfterFunc(firstByteTimeout, cancel)
	head := make([]byte, headSize)
	n, err := io.ReadAtLeast(stdout, head, 1)
	timedOut := !timer.Stop()
	if n == 0 {
		// yt-dlp closed its output, so it is exiting, or it has been killed.
		werr := a.wait()
		switch {
		case timedOut:
			return nil, fmt.Errorf("yt-dlp sent no audio within %s; stderr: %s", firstByteTimeout, stderr)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case werr != nil:
			return nil, fmt.Errorf("yt-dlp: %w; stderr: %s", werr, stderr)
		}
		return nil, fmt.Errorf("%w (%w)", ErrNotFound, err)
	}
	a.r = io.MultiReader(bytes.NewReader(head[:n]), stdout)
	return a, nil
}

// searchURL is the YouTube Music search for artist and title, limited to songs.
func searchURL(artist, title string) string {
	u := url.URL{
		Scheme:   "https",
		Host:     "music.youtube.com",
		Path:     "/search",
		RawQuery: url.Values{"q": {strings.TrimSpace(artist + " " + title)}}.Encode(),
		Fragment: "songs",
	}
	return u.String()
}

// audio is a running yt-dlp download. Reading past the end reports yt-dlp's
// failure, if it failed; Close kills yt-dlp if it is still running.
type audio struct {
	cmd    *exec.Cmd
	stdout io.Reader
	stderr *tailBuffer
	cancel context.CancelFunc
	r      io.Reader

	once sync.Once
	err  error
}

func (a *audio) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if err == io.EOF {
		if werr := a.wait(); werr != nil {
			return n, fmt.Errorf("yt-dlp: %w; stderr: %s", werr, a.stderr)
		}
	}
	return n, err
}

func (a *audio) Close() error {
	a.cancel()
	_ = a.wait()
	return nil
}

func (a *audio) wait() error {
	a.once.Do(func() {
		a.err = a.cmd.Wait()
		a.cancel()
	})
	return a.err
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.max; over > 0 {
		b.buf = b.buf[over:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
