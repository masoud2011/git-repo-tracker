#!/usr/bin/env bash
#
# package-app.sh — assemble a macOS git-repo-tracker.app bundle around a
# prebuilt binary. GoReleaser OSS cannot build .app bundles (that is a Pro-only
# feature), so we wrap the binary ourselves. The bundle's Info.plist sets
# LSUIElement, so the app runs as a menu-bar agent: tray icon, no Dock icon,
# no main window.
#
# Usage:
#   build/macos/package-app.sh --binary <path> --version <ver> --out <dir>
#     [--zip <zipfile>] [--install <applications-dir>]
#
# Produces <dir>/git-repo-tracker.app, and (with --zip) a ditto zip of it that
# preserves symlinks/permissions so Homebrew's cask can install it verbatim.
# --install signs and verifies a local build, then replaces the installed app.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="git-repo-tracker"

binary=""
version=""
out=""
zipfile=""
install_dir=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--binary | --version | --out | --zip | --install)
		# Reject a missing value or one that looks like the next flag, so a typo
		# fails clearly instead of silently consuming the following option.
		if [[ $# -lt 2 || "${2:-}" == --* ]]; then
			echo "package-app.sh: $1 requires a value" >&2
			exit 2
		fi
		case "$1" in
		--binary) binary="$2" ;;
		--version) version="$2" ;;
		--out) out="$2" ;;
		--zip) zipfile="$2" ;;
		--install) install_dir="$2" ;;
		esac
		shift 2
		;;
	*) echo "package-app.sh: unknown argument: $1" >&2; exit 2 ;;
	esac
done

for req in binary version out; do
	if [[ -z "${!req}" ]]; then
		echo "package-app.sh: missing required --$req" >&2
		exit 2
	fi
done
if [[ ! -f "$binary" ]]; then
	echo "package-app.sh: binary not found: $binary" >&2
	exit 1
fi

mkdir -p "$out"
if [[ -n "$install_dir" ]]; then
	if [[ "$(uname -s)" != Darwin ]]; then
		echo "package-app.sh: --install requires macOS" >&2
		exit 1
	fi
	if [[ "$out" -ef "$install_dir" ]]; then
		echo "package-app.sh: output and install directories must be different" >&2
		exit 1
	fi
fi

package_lock="$out/.$APP_NAME.package-lock"
if ! mkdir "$package_lock"; then
	echo "package-app.sh: another package build is active, or $out is not writable" >&2
	exit 1
fi
lock=""
staging=""
previous=""
target=""
cleanup_package() {
	local status=$?
	local keep_backup=false
	trap - EXIT
	if [[ -n "$previous" && -d "$previous" && ! -e "$target" ]]; then
		if ! mv "$previous" "$target"; then
			echo "package-app.sh: restore failed; previous app retained at $previous" >&2
			keep_backup=true
			status=1
		fi
	fi
	if [[ -n "$staging" && "$keep_backup" == false ]]; then
		rm -rf "$staging" || status=1
	fi
	if [[ -n "$lock" ]]; then
		rmdir "$lock" || status=1
	fi
	rmdir "$package_lock" || status=1
	exit "$status"
}
trap cleanup_package EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

app="$out/$APP_NAME.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

install -m 0755 "$binary" "$app/Contents/MacOS/$APP_NAME"
install -m 0644 "$SCRIPT_DIR/icon.icns" "$app/Contents/Resources/icon.icns"

# Substitute the version into the Info.plist template.
sed "s/__VERSION__/${version}/g" "$SCRIPT_DIR/Info.plist" >"$app/Contents/Info.plist"

echo "package-app.sh: built $app (version $version)"

if [[ -n "$install_dir" ]]; then
	plutil -lint "$app/Contents/Info.plist"
	codesign --force --sign - "$app"
	codesign --verify --deep --strict "$app"
fi

if [[ -n "$zipfile" ]]; then
	# ditto produces a macOS-native zip that preserves the bundle structure.
	rm -f "$zipfile"
	ditto -c -k --sequesterRsrc --keepParent "$app" "$zipfile"
	echo "package-app.sh: zipped -> $zipfile"
fi

if [[ -n "$install_dir" ]]; then
	mkdir -p "$install_dir"
	install_dir="$(cd "$install_dir" && pwd -P)"
	target="$install_dir/$APP_NAME.app"
	if [[ -L "$target" || ( -e "$target" && ! -d "$target" ) ]]; then
		echo "package-app.sh: refusing to replace a symlink or non-directory: $target" >&2
		exit 1
	fi
	if ! mkdir "$install_dir/.$APP_NAME.install-lock"; then
		echo "package-app.sh: cannot lock $install_dir; check permissions or another installation" >&2
		exit 1
	fi
	lock="$install_dir/.$APP_NAME.install-lock"
	staging="$(mktemp -d "$install_dir/.$APP_NAME-install.XXXXXX")"
	previous="$staging/previous.app"
	next="$staging/$APP_NAME.app"
	ditto "$app" "$next"
	codesign --verify --deep --strict "$next"
	if [[ -d "$target" ]]; then
		mv "$target" "$previous"
	fi
	if ! mv "$next" "$target"; then
		echo "package-app.sh: could not install $target; restoring the previous app" >&2
		exit 1
	fi
	echo "package-app.sh: installed $target (reopen the app to use this build)"
fi
