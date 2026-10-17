#!/usr/bin/env bash
# Run from a Linux release directory to register the application with your menu.
set -euo pipefail
release_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
data_dir="${XDG_DATA_HOME:-$HOME/.local/share}"
if [[ ! -x $release_dir/pi-manager || ! -f $release_dir/icon.png ]]; then
    echo "Run this script beside the release's pi-manager and icon.png." >&2
    exit 1
fi
mkdir -p -- "$data_dir/applications" "$data_dir/icons/hicolor/256x256/apps"
cp -- "$release_dir/icon.png" "$data_dir/icons/hicolor/256x256/apps/pi-manager.png"
# Desktop Entry Exec quoting differs from shell quoting. Escape its four
# reserved characters, and escape literal percent signs (field-code syntax).
desktop_argument() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//\$/\\\$}"
    value="${value//\`/\\\`}"
    # String-value escaping precedes Exec's quoted-argument parsing.
    value="${value//\\/\\\\}"
    value="${value//%/%%}"
    printf '"%s"' "$value"
}
exec_path="$(desktop_argument "$release_dir/pi-manager")"
exec_command="$(desktop_argument 'exec "$1"')"
cat > "$data_dir/applications/pi-manager.desktop" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=Pi-Manager
Comment=Manage Pi-hole DNS servers
Exec=/bin/sh -c $exec_command pi-manager $exec_path
Icon=pi-manager
Terminal=false
Categories=Network;System;
EOF
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$data_dir/applications"
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$data_dir/icons/hicolor" >/dev/null 2>&1 || true
fi
echo "Installed Pi-Manager launcher for $release_dir"
