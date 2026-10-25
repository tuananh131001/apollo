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
