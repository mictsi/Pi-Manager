# Pi-Manager

<img src="assets/icon.svg" width="96" height="96" alt="Pi-Manager: a white π on a rounded black square">

**Your Pi-hole fleet. One desktop.**

Pi-Manager brings up to **10 Pi-hole v6 servers** into one desktop app. See your
network at a glance, manage records across hosts, and choose exactly which
configuration changes to sync from your main server to its secondaries.

The dashboard, charts and icons travel inside the Go executable. Launch it like
any other app on Windows, Linux or macOS: there is no web server to deploy and no
separate frontend to install. Pi-Manager connects directly to your Pi-hole APIs.

**Starting version: 0.2.0** · Windows, Linux and macOS · Intel/AMD and ARM64

## One place to see and manage your network

- **A fleet-wide dashboard.** Combined query statistics, blocking trends and
  charts, with filters for one host, several hosts or the whole fleet.
- **A clear source of truth.** Choose one main server and up to nine secondaries.
  The blocklist domain total comes from the main; warnings highlight secondary
  totals that differ or cannot be checked.
- **Sync with control.** Preview drift in domains, local DNS, clients and lists.
  Select destinations, resource types and individual changes before applying.
- **Fewer repetitive clicks.** Select multiple records and target servers for
  management actions. Tables update in place without reloading the page.
- **Background monitoring.** Minimize to the tray and keep monitoring server
  status, blocking, CPU, memory, load and temperature where available.
- **A dashboard that fits.** Responsive views use the whole window, with English,
  Danish, Greek and Swedish interfaces.
- **Portable configuration.** Addresses, roles, saved passwords and preferences
  live in `config.json` beside the executable, alongside rotating logs.

## Download and launch

Open your GitHub repository's **Releases** page and download the archive matching
your operating system and processor. These are the artifact names for `0.2.0`:

| System | Intel / AMD 64-bit | ARM64 |
| --- | --- | --- |
| Windows | `pi-manager_0.2.0_windows_amd64.zip` | `pi-manager_0.2.0_windows_arm64.zip` |
| Linux | `pi-manager_0.2.0_linux_amd64.tar.gz` | `pi-manager_0.2.0_linux_arm64.tar.gz` |
| macOS | `pi-manager_0.2.0_darwin_amd64.tar.gz` | `pi-manager_0.2.0_darwin_arm64.tar.gz` |

Every archive includes documentation, an example configuration and license
notices. Each release also includes `SHA256SUMS` for checking the downloads.

1. Extract the archive into a folder you can write to.
2. On **Windows**, open `pi-manager.exe`. It includes the app icon and launches
   without a console window. Microsoft Edge WebView2 Runtime is required.
3. On **Linux**, run `./pi-manager`. Install GTK 3 and WebKitGTK 4.1 first.
   Optionally run `./install-desktop.sh` to add the app and icon to your desktop's
   application menu. Keep the extracted directory in its final location before
   installing that launcher.
4. On **macOS**, open `Pi-Manager.app`. The archive also includes a bare
   executable. WebKit and AppKit are supplied by macOS. Release bundles are
   currently unsigned and are not notarized.

On Debian/Ubuntu systems that provide these package names, install the Linux
runtimes with:

```sh
sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
```

Linux tray support additionally needs `libayatana-appindicator3-1` (or a
compatible AppIndicator library) and a desktop with a StatusNotifier host. Some
desktops need a tray extension. Without a usable tray, the app keeps normal
window minimization available.

## Connect your servers

1. Open **Settings** and enter a name and URL for each Pi-hole v6 server, such as
   `http://192.168.1.2/api` or `https://pi.hole/api`. The app adds `/api` when omitted.
2. Enter the Pi-hole password or app password accepted by that server's API.
   Passwords are saved in `config.json` so connections can be restored.
3. Mark exactly one server as **Main**. Set the others to **Secondary**. Use
   **Add server** to configure up to 10 servers in total.
4. Save. The app attempts to connect using the saved passwords. A server's login
   form can be used if it needs authentication.

Logout is available only in **Settings**. It ends the current connection and
suppresses background reconnection for that session. It does not erase the saved
password; clear the password field and save to remove it.

## Dashboard and queries

The top row shows server status. Dashboard cards and graphs combine the selected
hosts; use the host checkboxes to focus on a machine or group. The blocked
percentage reflects the combined query totals. **Domains on blocklist** always
shows the main server's total, even when the host filter changes. A different
secondary count raises a warning; use **Display drift** to inspect the actual
configuration differences.

**Details** starts expanded, while the top-domain panels start collapsed. Panel
choices are remembered. **Dashboard** and **Queries** default to a two-second
refresh. Each page has its own selector: realtime (a 500 ms poll), 1, 2, 5, 10, 15
or 30 seconds, one minute, or paused. Intervals run after a request finishes;
slower servers can make updates take longer. Use manual refresh whenever needed.

Queries can be filtered and inspected across hosts. Dashboard polling pauses
while the window is hidden; native health monitoring continues independently.

## Domains, local DNS, clients and lists

Choose target servers before applying changes in **Domains**, **Local DNS**,
**Clients** or **Lists**. Use row checkboxes for bulk actions and filters to
narrow the records. Add, enable, disable and delete controls appear where
supported by that resource. Only the affected table content updates after an
action, preserving the surrounding view.

The Local DNS editor manages host/IP records. Drift comparison and sync also
cover CNAME records exposed by the Pi-hole configuration API. **Gravity** starts
list database updates on the chosen servers and shows their output.

## Inspect drift and choose what to sync

1. In **Settings**, confirm the correct server is **Main**. Sync always flows
   from that server to the selected secondaries.
2. Open **Display drift**. Choose secondary servers and resource types: domains,
   local DNS, clients and/or lists.
3. Leave mirror mode off to add missing entries and update changed entries.
   Enable it to also offer removal of secondary-only entries.
4. Click **Preview sync**. Inspect the main and secondary values side by side.
   Filter by host, resource, text or differences only.
5. Check individual changes, or use **Select filtered changes**. Review the
   selection count, including selections hidden by the current filter.
6. Click **Apply selected changes** and confirm. Only selected changes apply.
   Even in mirror mode, deletions require selecting the secondary-only rows.

A preview does not modify servers. Group assignments are matched by group name
rather than numeric IDs. If configuration changes after a preview, create a new
preview before applying again. Errors are reported per operation. Sync covers
these resource types rather than every Pi-hole setting or query history.

## Tray and health monitoring

When a usable tray is available, minimizing hides the running window from the
Windows/Linux taskbar or macOS Dock. Use the tray menu to restore the dashboard
or quit. The app also offers an explicit tray-minimize control.

The native monitor keeps sampling while the dashboard is hidden, normally every
five seconds after the previous sample completes. Tray status includes
reachability, DNS blocking, CPU usage, load, RAM usage and temperature when the
server exposes those values. Missing measurements appear as `—`. Settings also
displays host health and tray availability.

## Configuration, logs and upgrades

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

See [config.example.json](config.example.json) for the format. Copy it to
`config.json` before first launch or configure through Settings. Quit before
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

## Build from source

Install **Go 1.23 or newer**; a current stable release is recommended. A C
compiler and Node.js are not required. Desktop runtimes are needed to run the
app, even though release compilation uses `CGO_ENABLED=0`.

Build for the current platform:

```sh
make build
./bin/pi-manager
```

Without Make, on Linux/macOS:

```sh
go build -buildvcs=false -trimpath -o bin/pi-manager .
```

On Windows, from PowerShell:

```powershell
go build -buildvcs=false -trimpath -ldflags="-H=windowsgui" -o bin/pi-manager.exe .
```

Build all six OS/architecture combinations from Linux, macOS or Windows with
Git Bash:

```sh
./build.sh
./build.sh /path/to/output
```

Outputs go to `bin/releases/<os>-<arch>/` by default. Windows builds embed the
icon and use the GUI subsystem. Linux builds include the desktop installer and
icon. macOS builds include `Pi-Manager.app` and a bare executable.

Build downloadable archives and checksums:

```sh
./release.sh
VERSION=0.2.1 ./release.sh /path/to/artifacts
```

`release.sh` requires only Go and Bash. Its Go packaging tool writes ZIP files
for Windows and `.tar.gz` files for Linux/macOS, plus `SHA256SUMS`. Fresh staging
keeps runtime `config.json` and logs out of release archives. Output defaults to
`dist/`. Set `GO=/path/to/go` to select a Go executable for either script.
`VERSION=X.Y.Z` overrides the version in every executable and the macOS bundle;
otherwise the default is **0.2.0** from `main.go`.

### Tests and source layout

```sh
go test ./...
go test -race ./...
go vet ./...
```

The race detector needs a C compiler. Optional native tests need the desktop
runtime and a display. On Linux, with Xvfb installed:

```sh
GDK_BACKEND=x11 PI_MANAGER_DESKTOP_TEST=1 xvfb-run -a go test -run TestNative -v .
```

- Root `.go` files: application logic, platform integration and tests.
- `assets/`: embedded interface, Chart.js, icons and dependency license notices.
- `tools/icon/`: Go icon generator; `go generate .` regenerates native resources.
- `tools/release/`: Go archive and checksum generator, used by `release.sh`.
- `packaging/`: macOS metadata and the Linux menu installer.
- `rsrc_windows_*.syso`: generated Windows icon resources used by Go builds.

## Publish a GitHub release

The [release workflow](.github/workflows/release.yml) runs when a tag matching
`v*` is pushed or a GitHub release is published. Tags must use **`vX.Y.Z`**. The
first version is **`v0.2.0`**.

After committing the sources and workflow to your GitHub repository:

```sh
git tag -a v0.2.0 -m "Pi-Manager 0.2.0"
git push origin v0.2.0
```

The workflow tests with the race detector, runs `go vet`, builds all six targets,
packages downloads, verifies checksums and checks the Linux executable's
version. It then creates a release with generated notes and uploads six archives
and `SHA256SUMS`. If a release already exists, it uploads assets to that release
without replacing its notes. Publishing through the GitHub UI is also supported;
choose a `vX.Y.Z` tag containing this workflow.

It uses the built-in `GITHUB_TOKEN` with `contents: write`; no personal token is
needed. Enable GitHub Actions and allow that permission in repository policy.
For the next release, use a new tag such as `v0.2.1`: the tag supplies the built
version automatically. Update `main.go` too when you want development builds to
show the new default version.

The tag-push route creates the release with its assets together and is preferred
when immutable releases are enabled. Existing immutable assets cannot be
replaced on a rerun. Workflow behavior follows GitHub's documentation for
[release events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#release)
and the [GitHub CLI release command](https://cli.github.com/manual/gh_release_create).

## Inspiration and homage

Pi-Manager takes its inspiration from
[Dobbelt-Pi-hole by tursoe / Christian Kragh Jessen](https://github.com/tursoe/Dobbelt-Pi-hole),
whose shared dashboard made managing two Pi-hole instances simpler. Pi-Manager
carries that idea forward as a local Go desktop app with up to ten servers,
selective sync and configuration drift inspection. Thank you to the original
project and its contributors for the foundation and inspiration.

Thanks also to the [Pi-hole project](https://pi-hole.net/) and its community for
the DNS blocking platform that makes Pi-Manager useful.

## License

Released under the [MIT license](LICENSE), retaining the original copyright
notice. Bundled dependency notices are in
[assets/THIRD_PARTY.txt](assets/THIRD_PARTY.txt) and available through `--licenses`.
