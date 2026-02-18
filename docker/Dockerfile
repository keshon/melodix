# Stage 1 — Builder
FROM golang:1.24.0-alpine3.20 AS builder

RUN apk add --no-cache \
        build-base \
        git \
        opus-dev

WORKDIR /build

# Кеширование зависимостей
COPY src/go.mod src/go.sum ./
RUN go mod download

# Копируем исходники
COPY src .

# Сборка с CGO
RUN GO_VERSION=$(go version | awk '{print $3}') && \
    BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ") && \
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
        -o app \
        -ldflags="-s -w \
        -X 'server-domme/internal/version.BuildDate=${BUILD_DATE}' \
        -X 'server-domme/internal/version.GoVersion=${GO_VERSION}'" \
        cmd/discord/main.go


# Stage 2 — Runtime
FROM alpine:3.20

# Только runtime-зависимости
RUN apk add --no-cache \
        ffmpeg \
        python3 \
        py3-pip \
        opus \
        libstdc++ \
    && pip3 install --no-cache-dir --break-system-packages yt-dlp \
    && rm -rf /var/cache/apk/*

WORKDIR /usr/project

# Копируем только бинарник
COPY --from=builder /build/app ./app

# Данные
RUN mkdir -p data && touch data/datastore.json

ENTRYPOINT ["./app"]