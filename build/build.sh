#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

if [[ $# -gt 1 ]]; then
	echo "Usage: build/build.sh [binary-output-path]" >&2
	exit 2
fi
binary="${1:-git-repo-tracker}"
mkdir -p dist
build_lock="$PROJECT_DIR/dist/.local-build-lock"
if ! mkdir "$build_lock"; then
	echo "build.sh: another local build is active, or dist is not writable" >&2
	exit 1
fi
trap 'rmdir "$build_lock"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$(dirname "$binary")"

if [[ "$(uname -s)" == Darwin ]]; then
	MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-11.0}" \
		go build -trimpath -ldflags '-s -w -X main.version=dev' -o "$binary" ./cmd/git-repo-tracker
	build/macos/package-app.sh --binary "$binary" --version dev --out dist \
		--install "${GIT_REPO_TRACKER_INSTALL_DIR:-/Applications}"
else
	go build -o "$binary" ./cmd/git-repo-tracker
fi
