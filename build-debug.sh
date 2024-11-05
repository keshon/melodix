#!/bin/bash

# BUILD

# Get Go version
GO_VERSION=$(go version | awk '{print $3}')

# Get the build date
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build command
go build -o melodix -ldflags "-X github.com/keshon/melodix/internal/version.BuildDate=$BUILD_DATE -X github.com/keshon/melodix/internal/version.GoVersion=$GO_VERSION" cmd/melodix/melodix.go