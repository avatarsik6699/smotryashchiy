#!/usr/bin/env bash
# Scans the full git history and every tracked/untracked non-ignored file for secrets.
set -Eeuo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
scan_dir=$(mktemp -d)
trap 'rm -rf -- "$scan_dir"' EXIT

gitleaks_version=v8.30.1
go install "github.com/zricethezav/gitleaks/v8@$gitleaks_version"
gitleaks_bin="$(go env GOPATH)/bin/gitleaks"

"$gitleaks_bin" git --config "$repo_dir/.gitleaks.toml" --redact --no-banner --verbose "$repo_dir"

git -C "$repo_dir" ls-files --cached --others --exclude-standard -z \
  | tar --directory "$repo_dir" --null --files-from=- --create \
  | tar --extract --directory "$scan_dir"

"$gitleaks_bin" dir --config "$repo_dir/.gitleaks.toml" --redact --no-banner --verbose "$scan_dir"
