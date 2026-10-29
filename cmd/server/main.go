// Command server runs the apollo HTTP service.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/tuananh131001/apollo/internal/config"
	"github.com/tuananh131001/apollo/internal/lastfm"
	"github.com/tuananh131001/apollo/internal/navidrome"
	"github.com/tuananh131001/apollo/internal/server"
	"github.com/tuananh131001/apollo/internal/youtube"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Load local settings from .env when present; real environment variables win.
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetLogLoggerLevel(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.LastFMAPIKey == "" {
		slog.Warn("LASTFM_API_KEY is not set; /s searches will return no results")
	}
	tracks := lastfm.NewTrackService(cfg.LastFMAPIKey, lastfm.DefaultBaseURL, nil)
	search := navidrome.NewInterceptSearchService(tracks, cfg.NavidromeURL, nil)
	audio := youtube.NewAudioService(cfg.YtDlpPath, cfg.YtDlpMaxConcurrent)
	if err := audio.Check(); err != nil {
		slog.Warn("yt-dlp not found; Last.fm songs will not play", "path", cfg.YtDlpPath, "err", err)
	}
	stream := navidrome.NewInterceptStreamService(audio, cfg.StreamWriteTimeout, cfg.NavidromeURL, nil)
	restProxy := navidrome.NewRestProxy(cfg.NavidromeURL, nil, search, stream)
	srv := &http.Server{Addr: cfg.Addr, Handler: server.NewHandler(restProxy)}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	slog.Info("shutting down")
	return srv.Shutdown(shutdownCtx)
}
