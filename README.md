# apollo

Go 1.27.3 application.

## Layout

```
.
├── go.mod, go.sum        # module definition and dependency checksums
├── Makefile, .env.example
├── cmd/                  # entry points, one folder per binary
│   └── server/main.go    # HTTP service
└── internal/             # private application code (not importable by other modules)
    ├── config/           # env-based configuration
    ├── server/           # HTTP handlers and routing (go-chi/chi v5)
    └── version/          # build metadata
```

## Usage

```sh
make run      # start the server on :8080 (curl localhost:8080/healthz)
make build    # build the server binary into ./bin
make test     # run tests
```

Configuration is read from environment variables; see `.env.example`.
