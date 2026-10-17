#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    echo "Usage: $0 [OUTPUT_DIR]"
    echo "Build six release archives and SHA256SUMS (default: dist/)."
    echo "Requires Go and Bash. Archives and checksums use Go's standard library."
    echo "Set GO to choose Go; set VERSION to override the source version."
    exit 0
fi
if (( $# > 1 )); then
    echo "Usage: $0 [OUTPUT_DIR]" >&2
    exit 1
fi
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
output_dir="${1:-$project_dir/dist}"
mkdir -p -- "$output_dir"
output_dir="$(cd -- "$output_dir" && pwd)"
app_version="${VERSION:-$(sed -n 's/^var version = "\([^"]*\)"/\1/p' "$project_dir/main.go")}"
# Fresh staging ensures an existing installation's config and logs never enter
# an archive. Only the six artifacts from this run enter SHA256SUMS.
staging_dir="$(mktemp -d "${TMPDIR:-/tmp}/pi-manager-release.XXXXXXXX")"
trap 'rm -rf -- "$staging_dir"' EXIT
VERSION="$app_version" "$project_dir/build.sh" "$staging_dir/build"
cd -- "$project_dir"
"${GO:-go}" run -buildvcs=false ./tools/release \
    -input "$staging_dir/build" -output "$output_dir" -version "$app_version"
echo "Release $app_version: six archives and SHA256SUMS in $output_dir"
