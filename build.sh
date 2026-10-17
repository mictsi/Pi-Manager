#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 [OUTPUT_DIR]"
    echo "Build Windows, Linux and macOS binaries for amd64 and arm64."
    echo "Default output: bin/releases/<os>-<arch>/pi-manager[.exe]"
    echo "Set GO to use a specific Go executable."
    echo "Set VERSION to override the application version (for example, 0.2.0)."
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    usage
    exit 0
fi
if (( $# > 1 )); then
    usage >&2
    exit 1
fi

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
go_bin="${GO:-go}"
if ! command -v "$go_bin" >/dev/null 2>&1; then
    echo "Go not found. Install Go 1.23 or newer, or set GO to its executable path." >&2
    exit 1
fi
# Resolve the destination before changing directory so custom relative paths
# are relative to the caller. Builds always use this repository's sources.
output_dir="${1:-$project_dir/bin/releases}"
mkdir -p -- "$output_dir"
output_dir="$(cd -- "$output_dir" && pwd)"
cd -- "$project_dir"
app_version="${VERSION:-$(sed -n 's/^var version = "\([^"]*\)"/\1/p' main.go)}"
if [[ ! $app_version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "Version must have the format X.Y.Z (for example, 0.2.0)." >&2
    exit 1
fi

for target_os in windows linux darwin; do
    for target_arch in amd64 arm64; do
        filename=pi-manager
        if [[ $target_os == windows ]]; then
            filename+=.exe
        fi
        target_dir="$output_dir/$target_os-$target_arch"
        mkdir -p -- "$target_dir"
        echo "Building $target_os/$target_arch..."
        linker_flags="-X main.version=$app_version"
        if [[ $target_os == windows ]]; then
            linker_flags+=" -H=windowsgui"
        fi
        # Native webview libraries and dashboard assets are embedded. Windows
        # uses the GUI subsystem so double-clicking never creates a console.
        CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
            "$go_bin" build -buildvcs=false -trimpath -ldflags "$linker_flags" -o "$target_dir/$filename" .
        if [[ $target_os == darwin ]]; then
            bundle_dir="$target_dir/Pi-Manager.app/Contents"
            mkdir -p -- "$bundle_dir/MacOS" "$bundle_dir/Resources"
            cp -- "$target_dir/$filename" "$bundle_dir/MacOS/pi-manager"
            cp -- assets/icon.icns "$bundle_dir/Resources/icon.icns"
            sed "s/@VERSION@/$app_version/g" packaging/Info.plist > "$bundle_dir/Info.plist"
            printf 'APPL????' > "$bundle_dir/PkgInfo"
        elif [[ $target_os == linux ]]; then
            cp -- assets/icon-256.png "$target_dir/icon.png"
            cp -- packaging/install-desktop.sh "$target_dir/install-desktop.sh"
            chmod +x "$target_dir/install-desktop.sh"
        fi
    done
done

echo "Built all six binaries in $output_dir"
