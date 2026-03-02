# ---- Stage 1: Builder ----
FROM golang:1.24.10-alpine3.21 AS builder

ENV GOPROXY=https://proxy.golang.org,direct

RUN apk add --no-cache \
        git

WORKDIR /usr/project

# Cache dependencies as a separate layer
COPY src/go.mod src/go.sum ./
RUN go mod download

# Copy source code
COPY src .

# Build with embedded metadata
RUN BUILD_DATE=$(date -u +"%Y-%m-%dT%H-%M-%SZ") && \
    GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none") && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
        -o app \
        -ldflags="-s -w \
            -X github.com/keshon/buildinfo.Version=dev \
            -X github.com/keshon/buildinfo.Commit=${GIT_COMMIT} \
            -X github.com/keshon/buildinfo.BuildTime=${BUILD_DATE} \
            -X github.com/keshon/buildinfo.Project=Melodix \
            -X 'github.com/keshon/buildinfo.Description=Discord music bot that allows you to play music from YouTube, SoundCloud and internet radio streams.'" \
        ./cmd/discord/

# ---- Stage 2: Runtime ----
FROM alpine:3.21

# Runtime dependencies: media processing + yt-dlp for streaming
RUN apk add --no-cache \
        ffmpeg \
        python3 \
        py3-pip \
        libstdc++ \
    && pip3 install --no-cache-dir --break-system-packages yt-dlp \
    && rm -rf /var/cache/apk/*

WORKDIR /usr/project

COPY --from=builder /usr/project/app ./app

RUN mkdir -p data && touch data/datastore.json

ENTRYPOINT ["/usr/project/app"]