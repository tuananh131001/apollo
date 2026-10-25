# apollo

Go 1.27.3 application.

## Layout

```
.
├── go.mod, go.sum        # module definition (go.sum appears once dependencies are added)
├── Makefile, .env.example
├── cmd/                  # entry points, one folder per binary
│   ├── server/main.go    # HTTP service
│   └── cli/main.go       # command-line tool
└── internal/             # private application code (not importable by other modules)
    ├── config/           # env-based configuration
    ├── server/           # HTTP handlers and routing
    └── version/          # build metadata
```

## Usage

```sh
make run      # start the server on :8080 (curl localhost:8080/healthz)
make build    # build all binaries into ./bin
make test     # run tests
```

Configuration is read from environment variables; see `.env.example`.
