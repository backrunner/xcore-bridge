#!/usr/bin/env sh
set -eu

version="${1:-latest}"

go get "github.com/xtls/xray-core@${version}"
go mod tidy
go test ./...

go list -m github.com/xtls/xray-core
