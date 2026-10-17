# Configuration, logs and upgrades

[Documentation](../README.md#documentation)

Files are created beside the **actual executable**, resolving symlinks:

| File | Purpose |
| --- | --- |
| `config.json` | Server IDs, names, URLs, roles, plaintext passwords and preferences |
| `pi-manager.log` | Structured JSON startup, action and error logs |
| `pi-manager.log.1` through `.5` | Rotated logs, approximately 5 MiB each |

For the macOS bundle, this directory is `Pi-Manager.app/Contents/MacOS/`. It must
be writable. Keep configuration and logs private: **saved passwords are
plaintext**. Files are created with owner-only permissions where supported.
Session tokens remain in memory. Passwords, session IDs and request/response
bodies are excluded from logs.

Use Settings to configure the app, or copy the example configuration included
with the release artifacts to `config.json` before first launch. Quit before
editing the file by hand. To upgrade, quit and preserve `config.json` while
replacing the executable or app bundle. On macOS, copy the configuration into
the new bundle's executable directory. Existing configurations from the earlier
app name can be reused unchanged.

Optional command-line flags:

```sh
./pi-manager --version   # print the embedded version and exit
./pi-manager --licenses  # print project and dependency licenses and exit
./pi-manager --debug     # include read requests in the log
```

The graphical app detaches when launched from a terminal. There is no terminal
interface. For startup failures, check the log, folder write permissions,
desktop runtimes and network access to each Pi-hole API.

## Single instance

Only one Pi-Manager desktop instance runs per user, including while minimized to
tray. Opening it again exits without starting another window or health monitor.
Use the existing window or the tray menu to restore it.

An OS-managed lock is kept in Pi-Manager's folder in your user configuration
directory. It is shared by all copies of the executable. The OS releases the
lock when the app exits or crashes; a leftover lock file does not prevent a
restart. Server configuration and logs still live beside the executable.

## Back up configuration

In Settings, use **Back up server** on a server's health card, or **Back up all
connected servers** to export their Pi-hole configurations as Teleporter ZIP
archives. Log in to any disconnected server before including it. Each server
shows its saved path or error, and one failure does not prevent other backups.

### Pi-hole permissions (including Docker)

If backup returns **HTTP 403: Unable to export configuration**, Pi-hole is
rejecting an app-password session without permission to export its configuration.
Exports include authentication settings, so Pi-hole protects them with
`webserver.api.app_sudo`. Enable it only for apps you trust: it also permits
app-password sessions to change server settings.

For Docker Compose, add this to the Pi-hole service's existing `environment`
section, then run `docker compose up -d` to recreate it with the changed setting:

```yaml
environment:
  FTLCONF_webserver_api_app_sudo: 'true'
```

For `docker run`, recreate the container with
`-e FTLCONF_webserver_api_app_sudo=true`, preserving the existing volumes, ports
and other options. A restart alone does not change container environment
variables. Environment-managed Pi-hole settings must be changed in the container
configuration rather than from Pi-Manager or the Pi-hole web UI.

For a native Pi-hole installation running directly on the host, run this on
that Pi-hole host:

```sh
sudo pihole-FTL --config webserver.api.app_sudo true
```

Alternatively, authenticate with the regular Pi-hole password accepted by the
API. Retry the backup after changing the server's permissions. These settings
apply to the Pi-hole server, whether you start Pi-Manager from its binary or
from a desktop launcher.

See Pi-hole's [app_sudo setting](https://docs.pi-hole.net/ftldns/configfile/#app_sudo)
for the supported configuration methods.

### Backup files

Use **Back up app settings** to save Pi-Manager's saved configuration separately,
including server passwords and preferences. Save any Settings edits first.

Backups are timestamped files in the `backups` folder beside the executable.
On macOS this is inside the app bundle's executable directory. Files are private
to the current user where supported. Keep app-settings backups private because
they contain saved passwords. Server ZIP contents are validated before a backup
is marked complete; failed or interrupted downloads are removed. Backups keep
running when you change views, and their results remain visible in Settings.

## Restore configuration

In Settings, open **Restore configuration**:

- **Restore Pi-hole server**: log in, choose the target server and a Teleporter
  ZIP, select the sections to replace, then confirm the file and destination.
  Domains, lists and clients include their group assignments. Include groups
  when restoring assignments from another server. Pi-hole settings include
  local DNS, network and API settings, so restoring these can change how you
  connect to the server. ZIP uploads are limited to 49 MiB.
- **Restore app settings**: choose a Pi-Manager settings JSON backup (up to
  2 MiB) and confirm. This replaces saved servers, passwords and preferences,
  discards unsaved Settings edits, and applies language and theme immediately.

Both actions save a timestamped backup of the current configuration first. If
that backup fails, the restore is cancelled. Settings shows the previous
configuration's backup path after success, or an error if the restore fails.
For a failed server import, the error includes that backup path; check the
server before retrying because an interrupted connection can leave the result
uncertain. Restores continue when you change views.

Pi-hole restarts after importing. Wait for it to return, update its address or
password if those changed, and log in again. App-password sessions require
Pi-hole's `webserver.api.app_sudo` setting to permit Teleporter imports. App
settings restores also clear current sessions; log in to each server again.
Only the selected target server is restored; use the sync controls separately
if you want to apply changes to secondaries.

### API details

Pi-hole supports exporting server configuration through its Teleporter API:
authenticated `GET /api/teleporter` returns a binary ZIP archive. With an API
base URL (ending in `/api`) and a valid session ID:

```sh
curl --fail --header "X-FTL-SID: $PIHOLE_SID" \
  --output pihole-backup.zip "${PIHOLE_API_URL%/}/teleporter"
```

The API also supports restoring a Teleporter archive using a multipart
`POST /api/teleporter`, with optional choices for which settings and database
tables to import. A restore replaces the selected server configuration and can
require additional permissions for app-password sessions.

These endpoints back up the Pi-hole server. Pi-Manager's local `config.json` is
saved and restored through the separate app-settings controls. You can also
restore app settings manually: quit Pi-Manager and copy the saved JSON over its
local `config.json`.

See Pi-hole's [Teleporter API specification](https://github.com/pi-hole/FTL/blob/master/src/api/docs/content/specs/teleporter.yaml)
and [API authentication documentation](https://docs.pi-hole.net/api/auth/).
