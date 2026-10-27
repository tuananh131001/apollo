# apollo

Go 1.27.3 application.

## Layout

```
.
├── go.mod, go.sum        # module definition and dependency checksums
├── Makefile, .env.example
├── Dockerfile            # multi-arch image (distroless, non-root)
├── compose.yaml          # local stack: apollo built from source + Navidrome
├── cmd/                  # entry points, one folder per binary
│   └── server/main.go    # HTTP service
├── internal/             # private application code (not importable by other modules)
│   ├── config/           # env-based configuration
│   ├── lastfm/           # Last.fm API client (track search)
│   ├── navidrome/        # reverse proxy for Navidrome's Subsonic API (/rest/*)
│   ├── server/           # HTTP handlers and routing (go-chi/chi v5)
│   └── version/          # build metadata
└── test/                 # tests, mirroring internal/ (black-box, external packages)
```

## Usage

```sh
make run      # start the server on :8080 (curl localhost:8080/healthz)
make build    # build the server binary into ./bin
make test     # run tests
```

Configuration is read from environment variables; see `.env.example`.
On startup the server also loads `.env` from the working directory when present
(via [godotenv](https://github.com/joho/godotenv)); variables already set in the
environment take precedence.
`APOLLO_NAVIDROME_URL` is required: all `/rest/*` requests are proxied to it.

`LASTFM_API_KEY` enables Last.fm search. A `search3` query starting with `/s`
is answered from Last.fm's `track.search` instead of Navidrome; add `@ <artist>`
to narrow it, e.g. `/s Baby @ Justin Bieber`. The client's credentials are
checked against Navidrome first (via `ping`), so a search command needs the
same authentication as a library search and fails with Navidrome's error.
Last.fm failures are logged and the client gets an empty result.

Last.fm songs play from YouTube Music: `stream` and `download` for a `lastfm-`
song id run [yt-dlp](https://github.com/yt-dlp/yt-dlp) (`APOLLO_YTDLP_PATH`,
default `yt-dlp` from `PATH`) and pipe its ~128 kbps AAC audio to the client.
yt-dlp needs [deno](https://deno.com) on `PATH` for YouTube; the Docker image
ships both. At most `APOLLO_YTDLP_MAX_CONCURRENT` songs (default 4) stream at
once. A song YouTube Music doesn't have is Subsonic error 70; any other failure
is error 0 and is logged as an error.

The live tests in `test/navidrome/stream_live_test.go` play songs through a
real Navidrome and yt-dlp, and are skipped unless those are configured:

```sh
APOLLO_NAVIDROME_URL=http://localhost:4533 NAVIDROME_USER=admin \
NAVIDROME_PASSWORD=secret APOLLO_YTDLP_PATH=/path/to/yt-dlp go test ./test/...
```

The Navidrome proxy tests replay go-vcr cassettes from
`test/navidrome/testdata/cassettes`. To re-record them against a real server:

```sh
NAVIDROME_RECORD=1 APOLLO_NAVIDROME_URL=http://localhost:4533 \
NAVIDROME_USER=admin NAVIDROME_PASSWORD=secret go test ./test/navidrome
```

## Docker

Local stack, with apollo built from source in front of a Navidrome container:

```sh
docker compose up --build   # apollo on :8080, Navidrome UI on :4533
```

Navidrome scans `./music` (or `NAVIDROME_MUSIC_DIR`); create its admin user at
http://localhost:4533 on first run.

## Releasing

Pushing a semver tag runs `.github/workflows/release.yml`: it runs the tests,
pushes a `linux/amd64` + `linux/arm64` image to
`ghcr.io/tuananh131001/apollo`, and creates a GitHub release with generated
notes.

```sh
git tag v1.2.3 && git push origin v1.2.3
```

`v1.2.3` is tagged `1.2.3`, `1.2`, `1` and `latest`; pre-releases such as
`v1.3.0-rc.1` get only their exact version and a pre-release on GitHub.
