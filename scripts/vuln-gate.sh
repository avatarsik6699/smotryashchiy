#!/usr/bin/env bash
# Fails on known vulnerabilities in reachable code and in the Go standard library in use.
set -Eeuo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
govulncheck_version=v1.8.0
go install "golang.org/x/vuln/cmd/govulncheck@$govulncheck_version"
cd "$repo_dir"
"$(go env GOPATH)/bin/govulncheck" ./...
