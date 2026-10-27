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

# yt-dlp plays Last.fm songs. Its standalone build bundles Python; the pinned
# checksums come from the release's SHA2-256SUMS. To upgrade, bump the version
# and both checksums.
FROM --platform=$BUILDPLATFORM alpine:3.22 AS ytdlp
ARG TARGETARCH
ARG YTDLP_VERSION=2026.10.26
ARG YTDLP_SHA256_AMD64=767ba9dce7052aa196f8b12652f433d76de128ba168966fa84b6394d5e6c6795
ARG YTDLP_SHA256_ARM64=e87e9533e89fe5889abaa21f5114dfe7756d6aa6219b83551add04d48b18e483
RUN case "$TARGETARCH" in \
      amd64) file=yt-dlp_linux; sum=$YTDLP_SHA256_AMD64 ;; \
      arm64) file=yt-dlp_linux_aarch64; sum=$YTDLP_SHA256_ARM64 ;; \
      *) echo "unsupported arch: $TARGETARCH" >&2; exit 1 ;; \
    esac \
 && wget -qO /yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${file}" \
 && echo "${sum}  /yt-dlp" | sha256sum -c - \
 && chmod 0755 /yt-dlp

# yt-dlp and deno need glibc, so the runtime is distroless/cc, not static.
FROM gcr.io/distroless/cc-debian13:nonroot
COPY --from=ytdlp /yt-dlp /usr/local/bin/yt-dlp
# deno is the JavaScript runtime yt-dlp needs to download from YouTube.
COPY --from=denoland/deno:bin-2.9.8 /deno /usr/local/bin/deno
COPY --from=build /out/apollo /apollo
EXPOSE 8080
ENTRYPOINT ["/apollo"]
