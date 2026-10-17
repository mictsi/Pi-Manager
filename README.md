# Pi-Manager

<img src="assets/icon.svg" width="96" height="96" alt="Pi-Manager: a white π on a rounded black square">

**Your Pi-hole fleet. One desktop.**

Pi-Manager brings up to **10 Pi-hole v6 servers** into one desktop app. See your
network at a glance, manage records across hosts, and choose exactly which
configuration changes to sync from your main server to its secondaries.

The dashboard, charts and icons travel inside the Go executable. Launch it on
Windows, Linux or macOS, with no web server to deploy or separate frontend to
install. Pi-Manager connects directly to your Pi-hole APIs.

Inspired by [Dobbelt-Pi-hole by tursoe / Christian Kragh Jessen](https://github.com/tursoe/Dobbelt-Pi-hole),
whose shared dashboard made managing two Pi-hole instances simpler. Thank you to
the original project and its contributors for the foundation and inspiration,
and to the [Pi-hole project](https://pi-hole.net/) and its community.

## Features

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

## Download

Find the latest versions under **Releases**, as release artifacts. Choose your
operating system and processor: Windows, Linux or macOS, on amd64 or ARM64.

## Documentation

- [Getting started](docs/getting-started.md): installation and server setup.
- [Using Pi-Manager](docs/using-pi-manager.md): dashboards, queries, records and tray.
- [Drift and sync](docs/sync.md): inspect differences and apply selected changes.
- [Configuration and logs](docs/configuration.md): saved settings, logging and upgrades.
- [Building](docs/building.md): local builds, cross-platform artifacts and tests.
- [Releasing](docs/releasing.md): publish artifacts through GitHub Actions.

## License

Pi-Manager is distributed under the [MIT License](LICENSE), retaining the
original copyright notice. Bundled dependency notices are included with the app
and release artifacts, and are available through `--licenses`.
