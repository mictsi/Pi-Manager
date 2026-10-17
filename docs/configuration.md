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
