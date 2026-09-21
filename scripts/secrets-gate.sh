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

# Tracked files deleted from the working tree (not yet staged) are still listed by --cached but have
# nothing to scan, and tar would fail on them; --deleted lists exactly those, so drop them.
{
  git -C "$repo_dir" ls-files --cached --others --exclude-standard -z | sort -z -u
} > "$scan_dir.all"
git -C "$repo_dir" ls-files --deleted -z | sort -z -u > "$scan_dir.deleted"
comm -z -23 "$scan_dir.all" "$scan_dir.deleted" \
  | tar --directory "$repo_dir" --null --files-from=- --create \
  | tar --extract --directory "$scan_dir"
rm -f -- "$scan_dir.all" "$scan_dir.deleted"

"$gitleaks_bin" dir --config "$repo_dir/.gitleaks.toml" --redact --no-banner --verbose "$scan_dir"
