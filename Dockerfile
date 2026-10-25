# syntax=docker/dockerfile:1

# Build on the runner's native platform and cross-compile for the target, so
# multi-arch images don't need QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/ cmd/
COPY internal/ internal/

ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X github.com/tuananh131001/apollo/internal/version.Version=${VERSION}" \
      -o /out/apollo ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/apollo /apollo
EXPOSE 8080
ENTRYPOINT ["/apollo"]
