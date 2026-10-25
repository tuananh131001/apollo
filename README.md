# apollo

Go 1.27.3 application.

## Layout

```
.
├── go.mod, go.sum        # module definition and dependency checksums
├── Makefile, .env.example
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
