# ---- Stage 1: Builder ----
FROM golang:1.26-alpine AS builder

ENV GOPROXY=https://proxy.golang.org,direct

# Install CGO deps
RUN apk add --no-cache git build-base alsa-lib-dev

WORKDIR /usr/project

# Cache dependencies layer
COPY src/go.mod src/go.sum ./
COPY src/pkg ./pkg
RUN go mod download

# Copy rest of the source
COPY src .

# Build with embedded metadata, CGO enabled
RUN BUILD_DATE=$(date -u +"%Y-%m-%dT%H-%M-%SZ") && \
    GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none") && \
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
        -o app \
        -ldflags="-s -w \
            -X github.com/keshon/buildinfo.Version=dev \
            -X github.com/keshon/buildinfo.Commit=${GIT_COMMIT} \
            -X github.com/keshon/buildinfo.BuildTime=${BUILD_DATE} \
            -X github.com/keshon/buildinfo.Project=Melodix \
            -X 'github.com/keshon/buildinfo.Description=Discord music bot that allows you to play music from YouTube, SoundCloud and internet radio streams.'" \
        ./cmd/discord/

# ---- Stage 2: Runtime ----
FROM alpine:latest

# Runtime dependencies
# nodejs is here for yt-dlp: without a JavaScript runtime it cannot solve
# YouTube's challenges and falls back to a client googlevideo restricts, which
# breaks live streams. The bot passes the matching yt-dlp flags itself once it
# finds a runtime on PATH (see pkg/music/parsers/ytdlp/runtime.go), so there is
# no config file to keep in step with the code.
RUN apk add --no-cache \
        ffmpeg \
        nodejs \
        python3 \
        py3-pip \
        libstdc++ \
        alsa-lib \
    && pip3 install --no-cache-dir --break-system-packages 'yt-dlp[default]' \
    && rm -rf /var/cache/apk/*

WORKDIR /usr/project
COPY --from=builder /usr/project/app ./app

RUN mkdir -p data/store
RUN mkdir -p data/cache

ENTRYPOINT ["/usr/project/app"]
