package youtube

import (
	"context"
	"io"
	"os/exec"
	"sync"
)

// AudioService downloads songs from YouTube Music by running yt-dlp, at most
// a fixed number at a time.
type AudioService struct {
	ytdlp string
	// slots holds one token per running yt-dlp.
	slots chan struct{}
}

// audio is a running yt-dlp download. Reading past the end reports yt-dlp's
// failure, if it failed; Close kills yt-dlp if it is still running.
type audio struct {
	cmd     *exec.Cmd
	stdout  io.Reader
	stderr  *tailBuffer
	cancel  context.CancelFunc
	release func()
	r       io.Reader

	once sync.Once
	err  error
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}
