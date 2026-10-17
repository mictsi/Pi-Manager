# Build from source

[Documentation](../README.md#documentation)

Install **Go 1.25 or newer**; a current stable release is recommended. A C
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
./release.sh /path/to/artifacts
```

`release.sh` requires only Go and Bash. Its Go packaging tool writes ZIP files
for Windows and `.tar.gz` files for Linux/macOS, plus `SHA256SUMS`. Fresh staging
keeps runtime `config.json` and logs out of release archives. Output defaults to
`dist/`. Set `GO=/path/to/go` to select a Go executable for either script.
`VERSION=X.Y.Z` overrides the version in every executable and the macOS bundle;
otherwise the source default is used.

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

For prebuilt downloads, find the latest versions under **Releases**, as release
artifacts. For automated publishing, see [Releasing](releasing.md).
