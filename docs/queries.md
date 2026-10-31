# Queries

[Documentation](../README.md#documentation) · [Using Pi-Manager](using-pi-manager.md)

Inspect queries across your servers. Choose a server on the left of the top
control row; the interval, **Pause / Resume**, **Refresh** and last-update text appear on
the right, matching the [Dashboard](dashboard.md).

![Queries with server and refresh controls, filters, colored allowed and blocked rows, and per-domain actions](images/queries.png)

## Filter and inspect

Use the time range, text, client, query type and status filters to narrow the
results. Choose the number of rows per page, sort using the column headings,
and use **Load older queries** when more history is available. Filters and
pagination stay in place during polling.

The **Allowed / blocked** column includes a status icon. Green rows indicate
allowed queries; red rows indicate blocked queries. Pending and unknown queries
have separate neutral states. The detailed **Status** column keeps the Pi-hole
result, such as Forwarded, Cache or GRAVITY.

## Pause and inspect a query

Click **Pause**, between the refresh interval and **Refresh**, to freeze the
current snapshot. A poll already in flight cannot change it. Click any row or
its domain to open query details: **Query received on**, **Client**, **Query
Status**, **Reply** and **Database ID**, along with the domain and host.

Reply information includes the API's reply type and response time when
available. Missing fields remain unavailable rather than being inferred.
**Resume** refreshes immediately and continues at the selected interval.
**Refresh** still requests a manual update while paused. The pause choice and
interval survive navigation; window visibility changes do not resume a pause.

![Paused query details with received time, client, status, reply and database ID](images/query-details.png)

Choose **Search lists** in the details to search that domain on its originating
server. If the record includes a CNAME target, **Search CNAME lists** searches
that target instead. See [Search lists](search-lists.md) for list membership and
multi-host searches.

## Allow or block a domain

Use a row's **Allow** or **Block** button to save an exact domain rule on the
server shown in that row. Existing exact entries keep their comments and group
assignments when enabled or moved between lists.

Rules affect future queries. The logged outcome keeps its original color;
filters, polling and the surrounding page stay in place. To include subdomains,
use the wildcard option in [Domains](records.md#domains-and-wildcards).

## Refresh

Queries defaults to a two-second interval. Choose realtime (a 500 ms poll), 1,
2, 5, 10, 15 or 30 seconds, one minute, or paused. The interval is saved separately
from Dashboard. Updates run after the previous request finishes; **Refresh**
requests an update immediately. Polling pauses while the window is hidden.
