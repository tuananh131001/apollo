// Package version exposes build metadata, overridable at link time:
//
//	go build -ldflags "-X github.com/tuananh131001/apollo/internal/version.Version=v1.0.0"
package version

var Version = "dev"
