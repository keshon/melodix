FROM golang:1.26-alpine3.23 AS builder
WORKDIR /src
RUN apk add --no-cache build-base alsa-lib-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/discord

FROM alpine:3.23
# yt-dlp needs a JavaScript runtime for YouTube challenges.
ARG YTDLP_VERSION=2026.10.26
RUN apk add --no-cache ca-certificates ffmpeg nodejs python3 py3-pip libstdc++ alsa-lib \
    && python3 -m venv /opt/yt-dlp \
    && /opt/yt-dlp/bin/pip install --no-cache-dir "yt-dlp[default]==${YTDLP_VERSION}"
ENV PATH="/opt/yt-dlp/bin:${PATH}"
WORKDIR /usr/project
COPY --from=builder /out/app ./app
COPY assets ./assets
RUN mkdir -p data/store data/cache
# Existing bind-mounted data needs no UID/GID migration.
USER 0:0
ENTRYPOINT ["/usr/project/app"]
