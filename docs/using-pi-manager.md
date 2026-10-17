# Using Pi-Manager

[Documentation](../README.md#documentation)

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

Query rows use green for allowed results and red for blocked results, with SVG
status icons and an **Allowed / blocked** column. Pending and unknown queries
have separate neutral states; the detailed status remains available.

Use a row's **Allow** or **Deny** button to save an exact rule for that domain on
the server shown in the row. Existing exact entries keep their comments and
group assignments when enabled or moved between lists. Rules affect future
queries; the logged outcome keeps its original color. Filters, polling and the
surrounding page stay in place.

## Manage records

Choose target servers before applying changes in **Domains**, **Local DNS**,
**Clients** or **Lists**. Use row checkboxes for bulk actions and filters to
narrow the records. Add, enable, disable and delete controls appear where
supported by that resource. Only the affected table content updates after an
action, preserving the surrounding view.

In Domains, check **Add domain as wildcard** to include a domain and all its
subdomains. For example, `example.com` becomes the regex `(\.|^)example\.com$`.
A leading `*.` is optional.
This works for allow and deny entries and for each domain in a multiline batch.
Leave it unchecked for an exact entry, or choose Regular expression to enter
your own regex without conversion.

Each record view also offers **Sync secondaries from main**. Choose secondary
targets, then confirm the change count to add missing entries and update changed
entries for that resource. Secondary-only entries are kept. Use **Display drift**
when you want to select individual changes or remove secondary-only entries.

The Local DNS editor manages host/IP records. Drift comparison and sync also
cover CNAME records exposed by the Pi-hole configuration API. **Gravity** starts
list database updates on individual servers and shows their output. **Update
gravity on all servers** starts every connected server together, skipping any
already updating. The status line shows ready, running and logged-out servers;
each server keeps its own progress log.

## Tray and health monitoring

When a usable tray is available, minimizing hides the running window from the
Windows/Linux taskbar or macOS Dock. Use the tray menu to restore the dashboard
or quit. The app also offers an explicit tray-minimize control.

The native monitor keeps sampling while the dashboard is hidden, normally every
five seconds after the previous sample completes. Tray status includes
reachability, DNS blocking, CPU usage, load, RAM usage and temperature when the
server exposes those values. Missing measurements appear as `—`. Settings also
displays host health and tray availability.

For configuration comparison and synchronization, see [Drift and sync](sync.md).
