.PHONY: build test run fmt vet

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

run:
	go run ./cmd/server

fmt:
	gofmt -w .

vet:
	go vet ./...
